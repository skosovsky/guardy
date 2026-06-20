package guardy_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

func ExamplePipeline_Run() {
	wordlistV := ext.NewWordlistValidator([]string{"bad"}, ext.Blocklist, ext.WithCode("FORBIDDEN"))
	pipeline := guardy.NewPipeline(guardy.WithFastPath(wordlistV))
	ctx := context.Background()
	result, err := pipeline.Run(ctx, nil, "this is bad")
	if err != nil {
		panic(err)
	}
	decision := result.PolicyDecision()
	if decision.IsTerminal() {
		fmt.Println("blocked:", decision.Code)
	}
	// Output:
	// blocked: FORBIDDEN
}

func ExampleNewPipeline() {
	regexV, _ := ext.NewRegexValidator(`(?i)(ignore previous|system prompt)`, ext.WithCode("PROMPT_INJECTION"))
	lengthV := ext.NewLengthValidator(0, 10000, ext.WithCode("TOO_LONG"))

	pipeline := guardy.NewPipeline(
		guardy.WithFastPath(regexV, lengthV),
	)

	ctx := context.Background()
	result, err := pipeline.Run(ctx, nil, "Hello, what is the weather?")
	if err != nil {
		panic(err)
	}
	decision := result.PolicyDecision()
	switch {
	case decision.IsTerminal():
		// handle block
	case decision.IsRetryable():
		_ = decision.RetryFeedback
	default:
		_ = result.Output
	}
	fmt.Println("configured")
	// Output:
	// configured
}

func ExamplePipeline_Run_withScope() {
	roleKey := guardy.NewScopeKey[string]("principal.role")
	pipeline := guardy.NewPipeline(
		guardy.WithPolicyValidators(guardy.NewTypedAttributeEquals[string, string](
			roleKey,
			"admin",
			guardy.WithPolicyCode(guardy.CodeAttributeMismatch),
		)),
	)
	scope := guardy.NewScope(guardy.ScopeValue(roleKey, "viewer"))
	result, err := pipeline.Run(context.Background(), scope, "hello")
	if err != nil {
		panic(err)
	}
	if result.PolicyDecision().IsTerminal() {
		fmt.Println("denied:", result.PolicyDecision().Disposition)
	}
	// Output:
	// denied: terminal_deny
}

func ExampleGuard() {
	regexV, _ := ext.NewRegexValidator(`(?i)ignore`, ext.WithCode("INJECT"))
	pipeline := guardy.NewPipeline(guardy.WithFastPath(regexV))

	extractor := func(r *http.Request) (string, error) {
		body, _ := io.ReadAll(r.Body)
		return string(body), nil
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler := guardy.Guard(pipeline, extractor, guardy.PlainTextInjector())(next)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	fmt.Println(rec.Code)
	// Output:
	// 200
}

func ExampleNewGuardWriter() {
	v := &examplePassValidator{}
	pipeline := guardy.NewPipeline(guardy.WithFastPath(v))

	var out strings.Builder
	gw := guardy.NewGuardWriter(&out, pipeline, guardy.WithChunkSize(64))
	_, _ = gw.Write([]byte("streaming text "))
	_ = gw.Close()
	fmt.Println(out.String())
	// Output:
	// streaming text
}

type examplePassValidator struct{}

func (examplePassValidator) Validate(_ context.Context, input string) (string, *guardy.Report, error) {
	return input, &guardy.Report{Action: guardy.ActionPass}, nil
}
