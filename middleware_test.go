package guardy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func bodyExtractor(r *http.Request) (*Input, error) {
	body, _ := io.ReadAll(r.Body)
	return &Input{Data: string(body)}, nil
}

func TestGuard_Block(t *testing.T) {
	v := &fakeValidator{
		name: "block",
		validate: func(context.Context, *Input) (Result, error) {
			return Result{Passed: false, Action: Block, Code: "BAD", Reason: "blocked"}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	handler := Guard(p, bodyExtractor)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called on Block")
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "BAD") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestGuard_Pass(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, *Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
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

func TestGuard_Override(t *testing.T) {
	v := &fakeValidator{
		name: "override",
		validate: func(context.Context, *Input) (Result, error) {
			return Result{
				Passed:       false,
				Action:       Override,
				OverrideText: "I am a bot.",
			}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	handler := Guard(p, bodyExtractor)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called on Override")
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("who are you?"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "I am a bot." {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestGuard_Redact(t *testing.T) {
	v := &fakeValidator{
		name: "redact",
		validate: func(_ context.Context, in *Input) (Result, error) {
			return Result{
				Passed:    false,
				Action:    Redact,
				CleanText: "clean",
			}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	handler := Guard(p, bodyExtractor)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != "clean" {
			t.Errorf("body = %q, want clean", string(body))
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("dirty"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestGuard_ReportFromContext(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, *Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	handler := Guard(p, bodyExtractor)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		report, ok := ReportFromContext(r.Context())
		if !ok {
			t.Fatal("ReportFromContext: report not in context")
		}
		if report.FinalAction != Pass {
			t.Errorf("FinalAction = %s, want Pass", report.FinalAction)
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestGuard_Retry(t *testing.T) {
	v := &fakeValidator{
		name: "retry",
		validate: func(context.Context, *Input) (Result, error) {
			return Result{
				Passed: false,
				Action: Retry,
				Code:   "HALLUCINATION",
				Reason: "not grounded",
			}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	handler := Guard(p, bodyExtractor)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called on Retry")
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "HALLUCINATION") {
		t.Errorf("body should contain code: %s", rec.Body.String())
	}
}

func TestGuard_PanicOnNilPipeline(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Guard(nil, extractor) should panic")
		} else if !strings.Contains(r.(string), "Pipeline") {
			t.Errorf("panic message should mention Pipeline, got %q", r)
		}
	}()
	_ = Guard(nil, bodyExtractor)
}

func TestGuard_PanicOnNilExtractor(t *testing.T) {
	p := NewPipeline()
	defer func() {
		if r := recover(); r == nil {
			t.Error("Guard(p, nil) should panic")
		} else if !strings.Contains(r.(string), "extractor") {
			t.Errorf("panic message should mention extractor, got %q", r)
		}
	}()
	_ = Guard(p, nil)
}
