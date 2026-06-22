package guardy

import (
	"context"
	"encoding/json"
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
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{Action: ActionBlock, Validator: "block", Reason: "blocked"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	handler := Guard(p, bodyExtractor, PlainTextInjector())(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Error("next handler should not be called on Block")
		}),
	)
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

func TestGuard_Retry_PublicMessageDoesNotLeakFeedback(t *testing.T) {
	v := &fakeValidator{
		name: "retry",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{
				Action: ActionRetry, Validator: ActionRetry.String(),
				Feedback: "field age must be >= 18",
			}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	handler := Guard(p, bodyExtractor, PlainTextInjector())(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Error("next must not run on retry")
		}),
	)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "age must be") {
		t.Fatalf("feedback leaked in body: %s", rec.Body.String())
	}
}

func TestGuard_Block_UsesSafeUserMessage(t *testing.T) {
	v := &fakeValidator{
		name: "block",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{
				Action: ActionBlock, Validator: "block", Code: "DENIED",
				Reason: "internal", SafeUserMessage: "Access denied",
			}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	handler := Guard(p, bodyExtractor, PlainTextInjector())(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}),
	)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Access denied") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestGuard_Block_UsesReportCode(t *testing.T) {
	v := &fakeValidator{
		name: "block",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{
				Action:    ActionBlock,
				Validator: "validator-name",
				Code:      "RULE_BLOCKED",
				Reason:    "blocked",
			}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	handler := Guard(p, bodyExtractor, PlainTextInjector())(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}),
	)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "RULE_BLOCKED") {
		t.Fatalf("expected code from report, body=%s", rec.Body.String())
	}
}

func TestGuard_Pass(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	called := false
	handler := Guard(p, bodyExtractor, PlainTextInjector())(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}),
	)
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
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var nextBody string
	handler := Guard(p, bodyExtractor, PlainTextInjector())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			nextBody = string(b)
			w.WriteHeader(http.StatusOK)
		}),
	)
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
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var nextBody string
	handler := Guard(p, transformingExtractor, PlainTextInjector())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			nextBody = string(b)
			w.WriteHeader(http.StatusOK)
		}),
	)
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
		validate: func(_ context.Context, text string) (string, *Report, error) {
			if text == "dirty" {
				return "clean", &Report{Action: ActionRedact, Validator: "redact", MutatedText: "clean"}, nil
			}
			return text, &Report{Action: ActionPass, Validator: "redact"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var nextBody string
	handler := Guard(p, bodyExtractor, PlainTextInjector())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			nextBody = string(b)
			w.WriteHeader(http.StatusOK)
		}),
	)
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
		validate: func(_ context.Context, _ string) (string, *Report, error) {
			return "", &Report{Action: ActionRedact, Validator: "wiper", MutatedText: ""}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var nextBody string
	handler := Guard(p, bodyExtractor, PlainTextInjector())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			nextBody = string(b)
			w.WriteHeader(http.StatusOK)
		}),
	)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("sensitive"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if nextBody != "" {
		t.Errorf("next body = %q, want empty (redact to empty must not leak original)", nextBody)
	}
}

func TestGuard_ExtractorError(t *testing.T) {
	badExtractor := func(*http.Request) (string, error) {
		return "", io.EOF
	}
	p := NewPipeline[string]()
	handler := Guard(p, badExtractor, PlainTextInjector())(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}),
	)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestGuard_ScopeIncompleteBeforeBody(t *testing.T) {
	t.Parallel()
	resourceKey := NewScopeKey[string]("resource.id")
	p := NewPipeline(WithPolicyValidators(NewTypedAttributePresent[string, string](resourceKey)))
	var nextCalled bool
	handler := Guard(
		p,
		bodyExtractor,
		PlainTextInjector(),
		WithGuardScope(MapScope{}),
	)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		nextCalled = true
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"secret":"data"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if nextCalled {
		t.Fatal("next handler must not run on incomplete scope")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var response struct {
		Code         string   `json:"code"`
		Message      string   `json:"message"`
		Missing      []string `json:"missing"`
		Requirements []struct {
			Key  string `json:"key"`
			Type string `json:"type"`
		} `json:"requirements"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if response.Code != CodeAttributeMissing {
		t.Fatalf("code = %q, want %q", response.Code, CodeAttributeMissing)
	}
	if len(response.Missing) != 1 || response.Missing[0] != resourceKey.Name() {
		t.Fatalf("missing = %#v", response.Missing)
	}
	if len(response.Requirements) != 1 {
		t.Fatalf("requirements = %#v", response.Requirements)
	}
	if response.Requirements[0].Key != resourceKey.Name() || response.Requirements[0].Type != "string" {
		t.Fatalf("requirements = %#v", response.Requirements)
	}
}

func TestGuard_TerminalRetry_Returns422WithTerminalDenyDisposition(t *testing.T) {
	v := &fakeValidator{
		name: "terminal-retry",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, FinishReport(&Report{
				Action:    ActionRetry,
				Validator: ActionRetry.String(),
				Retryable: false,
				Reason:    "terminal",
				Code:      "TERMINAL_RETRY",
			}, ControlSpec{Action: ActionRetry, Retryable: new(false)}), nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	handler := Guard(p, bodyExtractor, PlainTextInjector())(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Error("next must not run on terminal retry")
		}),
	)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("payload"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "TERMINAL_RETRY") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
