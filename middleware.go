package guardy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Guard returns an HTTP middleware that runs the pipeline on the request.
// The extractor converts *http.Request into *Input; the middleware runs the pipeline
// and handles Block (422), Redact (substitutes body and calls next), Override (200 + OverrideText), or Pass (calls next).
// Passing nil for p or extractor is a programmer error and causes a fail-fast panic.
func Guard(p *Pipeline, extractor func(*http.Request) (*Input, error)) func(http.Handler) http.Handler {
	if p == nil {
		panic("guardy: Guard requires non-nil *Pipeline")
	}
	if extractor == nil {
		panic("guardy: Guard requires non-nil extractor")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			input, err := extractor(r)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_input", err.Error())
				return
			}
			report, err := p.Run(ctx, input)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "pipeline_error", err.Error())
				return
			}
			switch report.FinalAction {
			case Block:
				writeJSONError(w, http.StatusUnprocessableEntity, report.errorCode(), report.errorReason())
				return
			case Override:
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				// Empty OverrideText yields empty body (200); validator should set OverrideText when using Override.
				_, _ = w.Write([]byte(report.OverrideText)) //nolint:gosec // G705: Content-Type is text/plain, not HTML
				return
			case Redact:
				ctx := withReport(r.Context(), report)
				r2 := r.WithContext(ctx)
				if report.FinalText != "" {
					r2.Body = io.NopCloser(strings.NewReader(report.FinalText))
					r2.ContentLength = int64(len(report.FinalText))
				}
				next.ServeHTTP(w, r2)
				return
			case Pass:
				ctx := withReport(r.Context(), report)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			case Retry:
				writeJSONError(w, http.StatusUnprocessableEntity, report.errorCode(), report.errorReason())
				return
			default:
				ctx := withReport(r.Context(), report)
				next.ServeHTTP(w, r.WithContext(ctx))
			}
		})
	}
}

func (r Report) errorCode() string {
	if idx := r.worstResultIndex(); idx >= 0 {
		return r.Results[idx].Code
	}
	return "blocked"
}

func (r Report) errorReason() string {
	if idx := r.worstResultIndex(); idx >= 0 {
		return r.Results[idx].Reason
	}
	return "validation failed"
}

// worstResultIndex returns the index of the result with highest-priority (worst) action.
func (r Report) worstResultIndex() int {
	if len(r.Results) == 0 {
		return -1
	}
	best := 0
	bestPri := PriorityForAction(r.Results[0].Action)
	for i := 1; i < len(r.Results); i++ {
		pri := PriorityForAction(r.Results[i].Action)
		if pri > bestPri {
			bestPri = pri
			best = i
		}
	}
	return best
}

type reportKey struct{}

func withReport(ctx context.Context, report Report) context.Context {
	return context.WithValue(ctx, reportKey{}, report)
}

// ReportFromContext returns the Report attached by Guard middleware, if any.
func ReportFromContext(ctx context.Context) (Report, bool) {
	r, ok := ctx.Value(reportKey{}).(Report)
	return r, ok
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}
