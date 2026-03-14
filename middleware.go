package guardy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Guard returns an HTTP middleware that runs the pipeline on the request body.
// The extractor reads the request and returns the text to validate; the middleware runs the pipeline
// and handles Block (422), Redact (substitutes body with MutatedText and calls next), or Pass (calls next).
func Guard(p *Pipeline, extractor func(*http.Request) (string, error)) func(http.Handler) http.Handler {
	if p == nil {
		panic("guardy: Guard requires non-nil *Pipeline")
	}
	if extractor == nil {
		panic("guardy: Guard requires non-nil extractor")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			// Read and keep original body so we can restore it on Pass (extractor may transform text).
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_input", err.Error())
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			text, err := extractor(r)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_input", err.Error())
				return
			}
			report, err := p.Run(ctx, text)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "pipeline_error", err.Error())
				return
			}
			switch report.Action {
			case ActionBlock:
				code := report.Validator
				if code == "" {
					code = "blocked"
				}
				reason := report.Reason
				if reason == "" {
					reason = "validation failed"
				}
				writeJSONError(w, http.StatusUnprocessableEntity, code, reason)
				return
			case ActionRedact:
				ctx = withReport(r.Context(), &report)
				r2 := r.WithContext(ctx)
				// Always substitute body with MutatedText (including empty string) to avoid leaking original content.
				r2.Body = io.NopCloser(strings.NewReader(report.MutatedText))
				r2.ContentLength = int64(len(report.MutatedText))
				next.ServeHTTP(w, r2)
				return
			default:
				ctx = withReport(r.Context(), &report)
				r2 := r.WithContext(ctx)
				// Restore original body for downstream (not extractor's text, which may be transformed).
				r2.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				r2.ContentLength = int64(len(bodyBytes))
				next.ServeHTTP(w, r2)
			}
		})
	}
}

type reportKey struct{}

func withReport(ctx context.Context, report *Report) context.Context {
	return context.WithValue(ctx, reportKey{}, report)
}

// ReportFromContext returns the Report attached by Guard middleware, if any.
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
