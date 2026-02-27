package guardy_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

func ExampleNewPipeline() {
	regexV, _ := ext.NewRegex(`(?i)(ignore previous|system prompt)`, guardy.Block, "PROMPT_INJECTION")
	lengthV := ext.NewLength(0, 10000, guardy.Block, "TOO_LONG")

	pipeline := guardy.NewPipeline(
		guardy.WithFailFast(true),
		guardy.WithTier1(regexV, lengthV),
	)

	ctx := context.Background()
	input := guardy.Input{Text: "Hello, what is the weather?"}
	report, err := pipeline.Run(ctx, input)
	if err != nil {
		panic(err)
	}
	switch report.FinalAction {
	case guardy.Block:
		// handle block
	case guardy.Pass, guardy.Redact:
		_ = report.FinalText
	}
	_ = report
}

func ExampleGuard() {
	regexV, _ := ext.NewRegex(`(?i)ignore`, guardy.Block, "INJECT")
	pipeline := guardy.NewPipeline(guardy.WithTier1(regexV))

	extractor := func(r *http.Request) (guardy.Input, error) {
		body, _ := io.ReadAll(r.Body)
		return guardy.Input{Text: string(body)}, nil
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler := guardy.Guard(pipeline, extractor)(next)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	// rec.Code is 200, rec.Body is "ok"
}

func ExampleNewGuardWriter() {
	v := &examplePassValidator{}
	pipeline := guardy.NewPipeline(guardy.WithTier1(v))

	var out strings.Builder
	gw := guardy.NewGuardWriter(&out, pipeline, guardy.WithChunkSize(64))
	_, _ = gw.Write([]byte("streaming text "))
	_ = gw.Close()
	// out contains "streaming text "
}

type examplePassValidator struct{}

func (examplePassValidator) Validate(context.Context, guardy.Input) (guardy.Result, error) {
	return guardy.Result{Passed: true, Action: guardy.Pass}, nil
}

func (examplePassValidator) Name() string { return "example" }
