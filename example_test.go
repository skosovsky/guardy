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
	wordlistV := ext.NewWordlist([]string{"bad"}, ext.Blocklist, guardy.ActionBlock, "FORBIDDEN")
	pipeline := guardy.NewPipeline(guardy.WithFastPath(wordlistV))
	ctx := context.Background()
	report, err := pipeline.Run(ctx, "this is bad")
	if err != nil {
		panic(err)
	}
	if report.Action == guardy.ActionBlock {
		fmt.Println("blocked:", report.Reason)
	}
	// Output:
	// blocked: blocklisted word found
}

func ExampleNewPipeline() {
	regexV, _ := ext.NewRegex(`(?i)(ignore previous|system prompt)`, guardy.ActionBlock, "PROMPT_INJECTION")
	lengthV := ext.NewLength(0, 10000, guardy.ActionBlock, "TOO_LONG")

	pipeline := guardy.NewPipeline(
		guardy.WithFastPath(regexV, lengthV),
	)

	ctx := context.Background()
	report, err := pipeline.Run(ctx, "Hello, what is the weather?")
	if err != nil {
		panic(err)
	}
	switch report.Action {
	case guardy.ActionBlock:
		// handle block
	case guardy.ActionPass, guardy.ActionRedact:
		_ = report.MutatedText
	}
	_ = report
}

func ExampleGuard() {
	regexV, _ := ext.NewRegex(`(?i)ignore`, guardy.ActionBlock, "INJECT")
	pipeline := guardy.NewPipeline(guardy.WithFastPath(regexV))

	extractor := func(r *http.Request) (string, error) {
		body, _ := io.ReadAll(r.Body)
		return string(body), nil
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
	pipeline := guardy.NewPipeline(guardy.WithFastPath(v))

	var out strings.Builder
	gw := guardy.NewGuardWriter(&out, pipeline, guardy.WithChunkSize(64))
	_, _ = gw.Write([]byte("streaming text "))
	_ = gw.Close()
	// out contains "streaming text "
}

type examplePassValidator struct{}

func (examplePassValidator) Validate(context.Context, string) (guardy.Report, error) {
	return guardy.Report{Action: guardy.ActionPass}, nil
}

func (examplePassValidator) Name() string { return "example" }
