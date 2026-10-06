package guardy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type httpTrackedBody struct {
	reader            io.Reader
	readErr, closeErr error
	reads, closes     int
}

func (b *httpTrackedBody) Read(p []byte) (int, error) {
	b.reads++
	if b.readErr != nil {
		return 0, b.readErr
	}
	return b.reader.Read(p)
}
func (b *httpTrackedBody) Close() error { b.closes++; return b.closeErr }

func TestHTTPGuardCapAndConsumedWrapperOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		limit     int64
		status    int
	}{
		{"exact", "abc", 3, 200}, {"over", "abcd", 3, 413}, {"default", strings.Repeat("x", DefaultMaxBodyBytes+1), DefaultMaxBodyBytes, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange: a lying ContentLength must not bypass consumed-byte admission.
			original := &httpTrackedBody{reader: strings.NewReader(tc.raw)}
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.Body = original
			req.ContentLength = 1
			calls := 0
			var received string
			middleware, err := Guard(
				MustNewPipeline[string](),
				bodyExtractor,
				PlainTextInjector(),
				WithGuardMaxBodyBytes(tc.limit),
			)
			if err != nil {
				t.Fatal(err)
			}
			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				raw, _ := io.ReadAll(r.Body)
				received = string(raw)
				replay, replayErr := r.GetBody()
				if replayErr != nil {
					t.Fatal(replayErr)
				}
				again, _ := io.ReadAll(replay)
				_ = replay.Close()
				if string(again) != tc.raw || r.ContentLength != int64(len(tc.raw)) {
					t.Error("replay/length not synchronized")
				}
				w.WriteHeader(http.StatusOK)
			}))
			rec := httptest.NewRecorder()
			// Act.
			handler.ServeHTTP(rec, req)
			// Assert.
			if original.closes != 1 || rec.Code != tc.status {
				t.Fatalf("close=%d status=%d", original.closes, rec.Code)
			}
			if tc.status == 200 {
				if calls != 1 || received != tc.raw {
					t.Fatal("pass body not restored")
				}
			} else if calls != 0 {
				t.Fatal("oversize reached handler")
			}
		})
	}
}

//nolint:gocognit,gocyclo,cyclop // Matrix checks body ownership and handler suppression at every failure stage.
func TestHTTPGuardFailureMatrixClosesBodiesAndSuppressesHandler(t *testing.T) {
	for _, stage := range []string{"read", "close", "extract", "extract-cancel", "extract-deadline", "extract-close", "scope", "scope-cancel", "validator", "report-fault", "deny", "retry", "inject", "inject-cancel", "pre-cancel"} {
		t.Run(stage, func(t *testing.T) {
			// Arrange.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			private := errors.New("private provider detail")
			original := &httpTrackedBody{reader: strings.NewReader("abc")}
			replacement := &httpTrackedBody{reader: strings.NewReader("replacement")}
			if stage == "read" {
				original.readErr = private
			}
			if stage == "close" {
				original.closeErr = private
			}
			if stage == "extract-close" {
				replacement.closeErr = private
			}
			if stage == "pre-cancel" {
				cancel()
			}
			extracts, validations, injects, handlers := 0, 0, 0, 0
			extractor := func(r *http.Request) (string, error) {
				extracts++
				r.Body = replacement
				switch stage {
				case "extract":
					return "failed", private
				case "extract-cancel":
					cancel()
				case "extract-deadline":
					return "failed", fmt.Errorf("wrapped: %w", context.DeadlineExceeded)
				}
				return "abc", nil
			}
			rule := ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
				validations++
				switch stage {
				case "validator":
					return "failed", nil, private
				case "report-fault":
					return "failed", &Report{Action: Action(255)}, nil
				case "deny":
					return value, &Report{Action: ActionBlock}, nil
				case "retry":
					return value, FinishReport(&Report{Action: ActionRetry}, ControlSpec{Action: ActionRetry}), nil
				}
				return "clean", &Report{Action: ActionRedact, MutatedText: "fake mirror"}, nil
			})
			injected := &httpTrackedBody{reader: strings.NewReader("clean")}
			injector := func(r *http.Request, _ string) error {
				injects++
				r.Body = injected
				if stage == "inject" {
					return private
				}
				if stage == "inject-cancel" {
					cancel()
				}
				return nil
			}
			factory := func(context.Context) (ExecutionScope, error) {
				if stage == "scope" {
					return nil, private
				}
				if stage == "scope-cancel" {
					cancel()
				}
				return NewScope(), nil
			}
			handler := MustGuard(
				MustNewPipeline(WithSequential(rule)),
				extractor,
				injector,
				WithGuardScopeFactory(factory),
			)(
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) { handlers++ }),
			)
			req := httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx)
			req.Body = original
			rec := httptest.NewRecorder()
			// Act.
			handler.ServeHTTP(rec, req)
			// Assert.
			status := 500
			if stage == "read" || stage == "extract" {
				status = 400
			}
			if stage == "deny" || stage == "retry" {
				status = 422
			}
			if handlers != 0 || rec.Code != status || original.closes != 1 ||
				strings.Contains(rec.Body.String(), private.Error()) {
				t.Fatalf(
					"handler=%d status=%d close=%d response=%s",
					handlers,
					rec.Code,
					original.closes,
					rec.Body.String(),
				)
			}
			if extracts > 0 && replacement.closes != 1 {
				t.Fatal("extractor replacement not closed")
			}
			if injects > 0 && injected.closes != 1 {
				t.Fatal("failed injected replacement not closed")
			}
			if (stage == "extract" || stage == "extract-cancel" || stage == "extract-deadline" || stage == "extract-close") &&
				validations != 0 {
				t.Fatal("failed extraction reached validator")
			}
			if stage == "pre-cancel" && (original.reads != 0 || extracts != 0) {
				t.Fatal("pre-cancel read or extracted body")
			}
		})
	}
}

func TestHTTPInjectedBodyHandoffOwnershipAndAuthoritativeValue(t *testing.T) {
	// Arrange: a close failure after handoff cannot rewrite an already sent response.
	original := &httpTrackedBody{reader: strings.NewReader("secret")}
	injected := &httpTrackedBody{closeErr: errors.New("late private close error")}
	handlerReplacement := &httpTrackedBody{reader: strings.NewReader("handler-owned")}
	rule := ValidatorFunc[string](func(context.Context, string) (string, *Report, error) {
		return "clean", &Report{Action: ActionRedact, MutatedText: "wrong"}, nil
	})
	var received string
	injector := func(r *http.Request, value string) error {
		injected.reader = strings.NewReader(value)
		r.Body = injected
		return nil
	}
	handler := MustGuard(
		MustNewPipeline(WithSequential(rule)),
		bodyExtractor,
		injector,
	)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			received = string(raw)
			r.Body = handlerReplacement
			w.WriteHeader(http.StatusCreated)
		}),
	)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Body = original
	rec := httptest.NewRecorder()
	// Act.
	handler.ServeHTTP(rec, req)
	// Assert.
	if received != "clean" || rec.Code != 201 || original.closes != 1 || injected.closes != 1 ||
		handlerReplacement.closes != 0 {
		t.Fatal("handoff/body replacement ownership or authoritative output changed")
	}
	_ = handlerReplacement.Close() // Explicit handler-owner cleanup of its replacement.
}

func TestHTTPGuardConstructionRejectsInvalidConfiguration(t *testing.T) {
	// Arrange.
	p := MustNewPipeline[string]()
	for _, options := range [][]GuardOption{{nil}, {WithGuardMaxBodyBytes(0)}, {WithGuardMaxBodyBytes(-1)}} {
		// Act.
		middleware, err := Guard(p, bodyExtractor, PlainTextInjector(), options...)
		// Assert.
		if middleware != nil || !errors.Is(err, ErrConfiguration) {
			t.Fatal("invalid HTTP config admitted")
		}
	}
	if middleware, err := Guard[string](
		nil,
		bodyExtractor,
		PlainTextInjector(),
	); middleware != nil ||
		!errors.Is(err, ErrConfiguration) {
		t.Fatal("nil pipeline admitted")
	}
	if middleware, err := Guard[string](
		p,
		nil,
		PlainTextInjector(),
	); middleware != nil ||
		!errors.Is(err, ErrConfiguration) {
		t.Fatal("nil extractor admitted")
	}
	if middleware, err := Guard[string](p, bodyExtractor, nil); middleware != nil || !errors.Is(err, ErrConfiguration) {
		t.Fatal("nil injector admitted")
	}
}
