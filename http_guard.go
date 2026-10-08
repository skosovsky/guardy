package guardy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// DefaultMaxBodyBytes is the default request body size limit for Guard (1 MiB of consumed input).
const DefaultMaxBodyBytes = 1 << 20

const defaultBlockedCode = "blocked"

// PlainTextInjector returns an injector that replaces the request body with the mutated string.
// Syncs Body, ContentLength, Header[Content-Length], and GetBody for proxy/retry compatibility.
// Guard closes the replaced borrowed body. Standalone callers must close the old
// body themselves and own the new body and each independently opened GetBody.
func PlainTextInjector() func(*http.Request, string) error {
	return func(r *http.Request, mutated string) error {
		body := io.NopCloser(strings.NewReader(mutated))
		r.Body = body
		r.ContentLength = int64(len(mutated))
		if r.Header == nil {
			r.Header = make(http.Header)
		}
		r.Header.Set("Content-Length", strconv.FormatInt(int64(len(mutated)), 10))
		r.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(mutated)), nil
		}
		return nil
	}
}

// GuardOption configures HTTP Guard middleware.
type GuardOption func(*guardConfig)

type guardConfig struct {
	scopeFactory ScopeFactory
	maxBodyBytes int64
}

// WithGuardScopeFactory constructs current caller-owned facts for every request,
// after extraction and immediately before validation. A nil factory means empty scope.
func WithGuardScopeFactory(scopeFactory ScopeFactory) GuardOption {
	return func(c *guardConfig) {
		c.scopeFactory = scopeFactory
	}
}

// WithGuardMaxBodyBytes sets a positive consumed-input cap; default is 1 MiB.
func WithGuardMaxBodyBytes(limit int64) GuardOption {
	return func(c *guardConfig) { c.maxBodyBytes = limit }
}

// Guard builds HTTP request middleware, returning deterministic config errors.
// Extractor/injector and handler borrow bodies; Guard closes consumed/installed
// bodies as specified in CONTRACTS.md. Redaction uses returned T, not MutatedText.
// Oversize is 413; read/extract error is 400; deny/retry is 422; faults are 500.
// Extractor/injector/next and options must not panic; they are trusted host callbacks.
func Guard[T any](
	p *Pipeline[T],
	extractor func(*http.Request) (T, error),
	injector func(*http.Request, T) error,
	opts ...GuardOption,
) (func(http.Handler) http.Handler, error) {
	if p == nil {
		return nil, configurationError("http_guard", "pipeline", "nil")
	}
	if extractor == nil {
		return nil, configurationError("http_guard", "extractor", "nil")
	}
	if injector == nil {
		return nil, configurationError("http_guard", "injector", "nil")
	}
	cfg := guardConfig{scopeFactory: nil, maxBodyBytes: DefaultMaxBodyBytes}
	for _, opt := range opts {
		if opt == nil {
			return nil, configurationError("http_guard", "options", "nil")
		}
		opt(&cfg)
	}
	if cfg.maxBodyBytes <= 0 {
		return nil, configurationError("http_guard", "max_body_bytes", "nonpositive")
	}
	return func(next http.Handler) http.Handler {
		if next == nil {
			panic(configurationError("http_guard", "handler", "nil"))
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serveGuard(w, r, p, extractor, injector, cfg, next)
		})
	}, nil
}

// MustGuard builds HTTP middleware or panics on Guard's configuration error.
func MustGuard[T any](
	p *Pipeline[T],
	extractor func(*http.Request) (T, error),
	injector func(*http.Request, T) error,
	opts ...GuardOption,
) func(http.Handler) http.Handler {
	middleware, err := Guard(p, extractor, injector, opts...)
	if err != nil {
		panic(err)
	}
	return middleware
}

// ownedHTTPBody makes framework Close idempotent without assuming concrete body types.
type ownedHTTPBody struct {
	io.ReadCloser

	once     sync.Once
	closeErr error
}

func (b *ownedHTTPBody) Close() error {
	b.once.Do(func() { b.closeErr = b.ReadCloser.Close() })
	return b.closeErr
}

func adoptHTTPBody(r *http.Request) *ownedHTTPBody {
	if body, ok := r.Body.(*ownedHTTPBody); ok {
		return body
	}
	body := r.Body
	if body == nil {
		body = http.NoBody
	}
	owned := &ownedHTTPBody{ReadCloser: body, once: sync.Once{}, closeErr: nil}
	r.Body = owned
	return owned
}

func finishHTTPCallback(r *http.Request, borrowed *ownedHTTPBody, keep bool) error {
	current := adoptHTTPBody(r)
	var err error
	if current != borrowed {
		err = borrowed.Close()
	}
	if !keep {
		err = errors.Join(err, current.Close())
		r.Body = http.NoBody
	}
	return err
}

func restoreHTTPBody(r *http.Request, data []byte) *ownedHTTPBody {
	r.Body = io.NopCloser(bytes.NewReader(data))
	r.ContentLength = int64(len(data))
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	r.Header.Set("Content-Length", strconv.FormatInt(r.ContentLength, 10))
	r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	return adoptHTTPBody(r)
}

func serveGuard[T any](
	w http.ResponseWriter,
	r *http.Request,
	p *Pipeline[T],
	extractor func(*http.Request) (T, error),
	injector func(*http.Request, T) error,
	cfg guardConfig,
	next http.Handler,
) {
	ctx := r.Context()
	original := adoptHTTPBody(r)
	if ctx.Err() != nil {
		_ = original.Close()
		r.Body = http.NoBody
		writeJSONError(w, http.StatusInternalServerError, CodeValidatorFailed, "validation failed")
		return
	}
	limited := http.MaxBytesReader(w, original, cfg.maxBodyBytes)
	data, readErr := io.ReadAll(limited)
	closeErr := limited.Close()
	r.Body = http.NoBody
	if callbackCancellation(ctx, readErr) != nil || closeErr != nil {
		writeJSONError(w, http.StatusInternalServerError, CodeValidatorFailed, "validation failed")
		return
	}
	if readErr != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](readErr); ok {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "input_too_large", "request body too large")
		} else {
			writeJSONError(w, http.StatusBadRequest, "invalid_input", "request body invalid")
		}
		return
	}
	borrowed := restoreHTTPBody(r, data)
	text, extractErr := extractor(r)
	closeErr = finishHTTPCallback(r, borrowed, false)
	if callbackCancellation(ctx, extractErr) != nil || closeErr != nil {
		writeJSONError(w, http.StatusInternalServerError, CodeValidatorFailed, "validation failed")
		return
	}
	if extractErr != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_input", "extraction failed")
		return
	}
	scope, err := cfg.scopeFactory.scope(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, CodeValidatorFailed, "validation failed")
		return
	}
	result, err := p.Run(ctx, scope, text)
	if err != nil {
		if errors.Is(err, ErrScopeIncomplete) {
			writeJSONScopeError(w, http.StatusBadRequest, err)
		} else {
			writeJSONError(w, http.StatusInternalServerError, CodeValidatorFailed, "validation failed")
		}
		return
	}
	decision := result.PolicyDecision()
	if decision.IsSystemFault() {
		writeJSONError(w, http.StatusInternalServerError, CodeValidatorFailed, "validation failed")
		return
	}
	if decision.IsTerminal() || decision.IsRetryable() {
		writeJSONDecisionError(w, http.StatusUnprocessableEntity, decision)
		return
	}
	deliverHTTPRequest(w, r, data, result, injector, next)
}

func deliverHTTPRequest[T any](
	w http.ResponseWriter,
	r *http.Request,
	data []byte,
	result RunResult[T],
	injector func(*http.Request, T) error,
	next http.Handler,
) {
	ctx := r.Context()
	borrowed := restoreHTTPBody(r, data)
	if result.Decision().Action == ActionRedact {
		injectErr := injector(r, result.Output)
		keep := callbackCancellation(ctx, injectErr) == nil && injectErr == nil
		closeErr := finishHTTPCallback(r, borrowed, keep)
		if !keep || closeErr != nil {
			if keep {
				_ = finishHTTPCallback(r, adoptHTTPBody(r), false)
			}
			writeJSONError(w, http.StatusInternalServerError, "inject_failed", "injection failed")
			return
		}
	}
	handedOff := adoptHTTPBody(r)
	defer func() { _ = handedOff.Close() }()
	if ctx.Err() != nil {
		writeJSONError(w, http.StatusInternalServerError, CodeValidatorFailed, "validation failed")
		return
	}
	next.ServeHTTP(w, r)
}

func scopeIncompleteMessage(err error) string {
	missing := MissingScopeKeys(err)
	if len(missing) == 0 {
		return "execution scope incomplete"
	}
	return "execution scope incomplete: " + strings.Join(missing, ", ")
}

type jsonErrorResponse struct {
	Code         string                     `json:"code"`
	Message      string                     `json:"message"`
	Missing      []string                   `json:"missing,omitempty"`
	Requirements []scopeRequirementResponse `json:"requirements,omitempty"`
}

type scopeRequirementResponse struct {
	Key  string `json:"key"`
	Type string `json:"type,omitempty"`
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSONErrorResponse(w, status, jsonErrorResponse{
		Code:         code,
		Message:      message,
		Missing:      nil,
		Requirements: nil,
	})
}

func writeJSONScopeError(w http.ResponseWriter, status int, err error) {
	writeJSONErrorResponse(w, status, jsonErrorResponse{
		Code:         CodeAttributeMissing,
		Message:      scopeIncompleteMessage(err),
		Missing:      MissingScopeKeys(err),
		Requirements: scopeRequirementResponses(err),
	})
}

func writeJSONErrorResponse(w http.ResponseWriter, status int, response jsonErrorResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func writeJSONDecisionError(w http.ResponseWriter, status int, decision Decision) {
	code := decision.Code
	if code == "" {
		code = decision.Validator
	}
	if code == "" {
		code = defaultBlockedCode
	}
	msg := decision.SafeMessage
	if msg == "" {
		msg = decision.RetryFeedback
	}
	writeJSONError(w, status, code, msg)
}

func scopeRequirementResponses(err error) []scopeRequirementResponse {
	requirements := MissingScopeRequirements(err)
	if len(requirements) == 0 {
		return nil
	}
	out := make([]scopeRequirementResponse, 0, len(requirements))
	for _, req := range requirements {
		out = append(out, scopeRequirementResponse{Key: req.Key, Type: req.Type})
	}
	return out
}
