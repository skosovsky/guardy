package guardy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// DefaultMaxBodyBytes is the default request body size limit for Guard (1MB, DoS protection).
const DefaultMaxBodyBytes = 1 << 20

const defaultBlockedCode = "blocked"

// PlainTextInjector returns an injector that replaces the request body with the mutated string.
// Syncs Body, ContentLength, Header[Content-Length], and GetBody for proxy/retry compatibility.
func PlainTextInjector() func(*http.Request, string) error {
	return func(r *http.Request, mutated string) error {
		body := io.NopCloser(strings.NewReader(mutated))
		r.Body = body
		r.ContentLength = int64(len(mutated))
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
	scope ExecutionScope
}

// WithGuardScope sets the execution scope for pipeline runs.
func WithGuardScope(scope ExecutionScope) GuardOption {
	return func(c *guardConfig) {
		c.scope = scope
	}
}

// Guard returns net/http middleware that runs the pipeline on the request body.
// It is HTTP-specific; for generic func(context.Context, Req) (Res, error) wrapping use WrapInput / WrapOutput.
// Extractor reads the request and returns value of type T to validate.
// Injector applies mutated T to the request body on ActionRedact (required; format-aware).
// On Block/Retry returns 422. On Pass restores original body.
// Use Guard[string] with PlainTextInjector for string-based pipelines.
func Guard[T any](
	p *Pipeline[T],
	extractor func(*http.Request) (T, error),
	injector func(*http.Request, T) error,
	opts ...GuardOption,
) func(http.Handler) http.Handler {
	if p == nil {
		panic("guardy: Guard requires non-nil Pipeline")
	}
	if extractor == nil {
		panic("guardy: Guard requires non-nil extractor")
	}
	if injector == nil {
		panic("guardy: Guard requires non-nil injector")
	}
	cfg := guardConfig{scope: nil}
	for _, opt := range opts {
		opt(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if err := checkScopeRequirements(cfg.scope, p.RequiredScope()); err != nil {
				writeJSONScopeError(w, http.StatusBadRequest, err)
				return
			}
			limitedBody := http.MaxBytesReader(w, r.Body, DefaultMaxBodyBytes)
			bodyBytes, err := io.ReadAll(limitedBody)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_input", "request body too large or invalid")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			text, err := extractor(r)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_input", "extraction failed")
				return
			}
			result, err := p.Run(ctx, cfg.scope, text)
			if err != nil {
				if errors.Is(err, ErrScopeIncomplete) {
					writeJSONScopeError(w, http.StatusBadRequest, err)
					return
				}
				writeJSONError(w, http.StatusInternalServerError, CodeValidatorFailed, "validation failed")
				return
			}
			rep := result.Decision()
			decision := result.PolicyDecision()
			switch {
			case decision.IsTerminal(), decision.IsRetryable():
				writeJSONDecisionError(w, http.StatusUnprocessableEntity, decision)
				return
			case rep.Action == ActionRedact:
				if err := injector(r, result.Output); err != nil {
					writeJSONError(w, http.StatusInternalServerError, "inject_failed", "injection failed")
					return
				}
				next.ServeHTTP(w, r)
				return
			default:
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				r.ContentLength = int64(len(bodyBytes))
				r.Header.Set("Content-Length", strconv.FormatInt(int64(len(bodyBytes)), 10))
				r.GetBody = func() (io.ReadCloser, error) {
					return io.NopCloser(bytes.NewReader(bodyBytes)), nil
				}
				next.ServeHTTP(w, r)
			}
		})
	}
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
		out = append(out, scopeRequirementResponse(req))
	}
	return out
}
