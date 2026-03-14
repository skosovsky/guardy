package guardy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func bodyExtractor(r *http.Request) (string, error) {
	body, _ := io.ReadAll(r.Body)
	return string(body), nil
}

func TestGuard_Block(t *testing.T) {
	v := &fakeValidator{
		name: "block",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionBlock, Validator: "block", Reason: "blocked"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	handler := Guard(p, bodyExtractor)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called on Block")
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "block") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestGuard_Pass(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	called := false
	handler := Guard(p, bodyExtractor)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !called {
		t.Error("next handler should be called on Pass")
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Errorf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGuard_PassBodyRestored(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var nextBody string
	handler := Guard(p, bodyExtractor)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		nextBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if nextBody != "hello" {
		t.Errorf("next body = %q, want hello (body must be restored on Pass)", nextBody)
	}
}

// transformingExtractor returns normalized text for validation but downstream must receive original body on Pass.
func transformingExtractor(r *http.Request) (string, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", err
	}
	return strings.ToUpper(string(body)), nil
}

func TestGuard_PassRestoresOriginalBodyNotExtractorText(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var nextBody string
	handler := Guard(p, transformingExtractor)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		nextBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	// Extractor returned "HELLO" for pipeline; downstream must get original "hello".
	if nextBody != "hello" {
		t.Errorf("next body = %q, want hello (original bytes, not extractor-transformed text)", nextBody)
	}
}

func TestGuard_Redact(t *testing.T) {
	v := &fakeValidator{
		name: "redact",
		validate: func(_ context.Context, text string) (Report, error) {
			if text == "dirty" {
				return Report{Action: ActionRedact, Validator: "redact", MutatedText: "clean"}, nil
			}
			return Report{Action: ActionPass, Validator: "redact"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var nextBody string
	handler := Guard(p, bodyExtractor)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		nextBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("dirty"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if nextBody != "clean" {
		t.Errorf("next body = %q, want clean", nextBody)
	}
}

func TestGuard_RedactToEmptyBody(t *testing.T) {
	v := &fakeValidator{
		name: "wiper",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionRedact, Validator: "wiper", MutatedText: ""}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var nextBody string
	handler := Guard(p, bodyExtractor)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		nextBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("sensitive"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if nextBody != "" {
		t.Errorf("next body = %q, want empty (redact to empty must not leak original)", nextBody)
	}
}

func TestGuard_ReportFromContext(t *testing.T) {
	v := &fakeValidator{
		name: "v",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionPass, Validator: "v"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	handler := Guard(p, bodyExtractor)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rep, ok := ReportFromContext(r.Context())
		if !ok {
			t.Error("ReportFromContext: no report")
			return
		}
		if rep.Validator != "v" {
			t.Errorf("report.Validator = %q", rep.Validator)
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestGuard_ExtractorError(t *testing.T) {
	badExtractor := func(*http.Request) (string, error) {
		return "", io.EOF
	}
	p := NewPipeline()
	handler := Guard(p, badExtractor)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
