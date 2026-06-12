package guardy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// DefaultMaxBodyBytes is the default request body size limit for Guard (1MB, DoS protection).
const DefaultMaxBodyBytes = 1 << 20

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
			if err := checkScopeComplete(cfg.scope, p.RequiredScopeKeys()); err != nil {
				writeJSONError(w, http.StatusBadRequest, CodeAttributeMissing, "execution scope incomplete")
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
					writeJSONError(w, http.StatusBadRequest, CodeAttributeMissing, "execution scope incomplete")
					return
				}
				writeJSONError(w, http.StatusInternalServerError, CodeValidatorFailed, "validation failed")
				return
			}
			rep := result.Decision()
			switch {
			case rep.IsTerminalDeny(), rep.IsRetryableCorrection():
				code := rep.Code
				if code == "" {
					code = rep.Validator
				}
				if code == "" {
					code = "blocked"
				}
				msg := rep.PublicMessage()
				writeJSONError(w, http.StatusUnprocessableEntity, code, msg)
				return
			case rep.Action == ActionRedact:
				r2 := r.WithContext(withReport(r.Context(), rep))
				if err := injector(r2, result.Output); err != nil {
					writeJSONError(w, http.StatusInternalServerError, "inject_failed", "injection failed")
					return
				}
				next.ServeHTTP(w, r2)
				return
			default:
				ctx = withReport(r.Context(), rep)
				r2 := r.WithContext(ctx)
				r2.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				r2.ContentLength = int64(len(bodyBytes))
				r2.Header.Set("Content-Length", strconv.FormatInt(int64(len(bodyBytes)), 10))
				r2.GetBody = func() (io.ReadCloser, error) {
					return io.NopCloser(bytes.NewReader(bodyBytes)), nil
				}
				next.ServeHTTP(w, r2)
			}
		})
	}
}

type reportKey struct{}

func withReport(ctx context.Context, report *Report) context.Context {
	return context.WithValue(ctx, reportKey{}, report)
}

// ReportFromContext returns the Report attached by HTTP Guard, if any.
func ReportFromContext(ctx context.Context) (Report, bool) {
	r, ok := ctx.Value(reportKey{}).(*Report)
	if !ok || r == nil {
		return Report{}, false
	}
	return *r, true
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}
