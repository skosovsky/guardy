package guardy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGuardWriter_Pass(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var out bytes.Buffer
	gw := NewGuardWriter(&out, p, WithChunkSize(4))
	_, err := gw.Write([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	_ = gw.Close()
	if out.String() != "hello" {
		t.Errorf("out = %q", out.String())
	}
}

func TestGuardWriter_Block(t *testing.T) {
	v := &fakeValidator{
		name: "block",
		validate: func(_ context.Context, text string) (Report, error) {
			if strings.Contains(text, "x") {
				return Report{Action: ActionBlock, Validator: "block"}, nil
			}
			return Report{Action: ActionPass, Validator: "block"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var out bytes.Buffer
	gw := NewGuardWriter(&out, p, WithChunkSize(2))
	n, err := gw.Write([]byte("ab"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("n = %d", n)
	}
	_, err = gw.Write([]byte("xy"))
	if err == nil {
		t.Error("expected error on block")
	}
	if !errors.Is(err, ErrBlocked) {
		t.Errorf("err = %v", err)
	}
	_, _ = gw.Write([]byte("z"))
	if out.String() != "ab" {
		t.Errorf("out = %q (blocked content should not appear)", out.String())
	}
}

func TestGuardWriter_Redact(t *testing.T) {
	v := &fakeValidator{
		name: "redact",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionRedact, Validator: "redact", MutatedText: "[CLEAN]"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var out bytes.Buffer
	gw := NewGuardWriter(&out, p, WithChunkSize(10))
	_, _ = gw.Write([]byte("dirty"))
	_ = gw.Close()
	if out.String() != "[CLEAN]" {
		t.Errorf("out = %q", out.String())
	}
}

func TestGuardWriter_RedactToEmptyChunk(t *testing.T) {
	v := &fakeValidator{
		name: "wiper",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionRedact, Validator: "wiper", MutatedText: ""}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var out bytes.Buffer
	gw := NewGuardWriter(&out, p, WithChunkSize(10))
	_, _ = gw.Write([]byte("secret"))
	_ = gw.Close()
	if out.String() != "" {
		t.Errorf("out = %q, want empty (redact to empty must not leak chunk)", out.String())
	}
}

func TestGuardWriter_FlushOnClose(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var out bytes.Buffer
	gw := NewGuardWriter(&out, p, WithChunkSize(100))
	_, _ = gw.Write([]byte("small"))
	if out.Len() != 0 {
		t.Error("buffer should not be written until chunk size or Close")
	}
	_ = gw.Close()
	if out.String() != "small" {
		t.Errorf("out = %q", out.String())
	}
}

func TestGuardWriter_WriteReturnsNAcceptedOnError(t *testing.T) {
	v := &fakeValidator{
		name: "block",
		validate: func(_ context.Context, text string) (Report, error) {
			if text == "bb" {
				return Report{Action: ActionBlock, Validator: "block"}, nil
			}
			return Report{Action: ActionPass, Validator: "block"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var out bytes.Buffer
	gw := NewGuardWriter(&out, p, WithChunkSize(2))
	_, _ = gw.Write([]byte("aa"))
	n, err := gw.Write([]byte("bb"))
	if err == nil {
		t.Fatal("expected error")
	}
	if n != 2 {
		t.Errorf("Write must return bytes accepted on error: n = %d", n)
	}
	if out.String() != "aa" {
		t.Errorf("out = %q", out.String())
	}
}

func TestGuardWriter_ImplementsWriter(t *testing.T) {
	var _ io.WriteCloser = (*GuardWriter)(nil)
}

func TestGuardWriter_WithContext(t *testing.T) {
	var contextCalled atomic.Bool
	ctxFn := func() (context.Context, context.CancelFunc) {
		contextCalled.Store(true)
		return context.Background(), func() {}
	}
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var out bytes.Buffer
	gw := NewGuardWriter(&out, p, WithChunkSize(10), WithContext(ctxFn))
	_, _ = gw.Write([]byte("trigger"))
	_ = gw.Close()
	if !contextCalled.Load() {
		t.Error("WithContext: context factory was not called")
	}
}

func TestGuardWriter_WithTimeout(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var out bytes.Buffer
	gw := NewGuardWriter(&out, p, WithChunkSize(5), WithTimeout(2*time.Second))
	_, err := gw.Write([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	_ = gw.Close()
	if out.String() != "hello" {
		t.Errorf("out = %q", out.String())
	}
}

func TestGuardWriter_ChunkSizeZeroOrNegative_UsesDefault(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var out bytes.Buffer
	gw := NewGuardWriter(&out, p, WithChunkSize(0))
	_, _ = gw.Write([]byte("a"))
	_ = gw.Close()
	if out.String() != "a" {
		t.Errorf("out = %q", out.String())
	}
	out.Reset()
	gw2 := NewGuardWriter(&out, p, WithChunkSize(-1))
	_, _ = gw2.Write([]byte("b"))
	_ = gw2.Close()
	if out.String() != "b" {
		t.Errorf("out = %q", out.String())
	}
}

func TestGuardWriter_UTF8Safe_Cyrillic(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var out bytes.Buffer
	gw := NewGuardWriter(&out, p, WithChunkSize(3))
	_, err := gw.Write([]byte("привет"))
	if err != nil {
		t.Fatal(err)
	}
	_ = gw.Close()
	if out.String() != "привет" {
		t.Errorf("out = %q (Cyrillic must not be corrupted)", out.String())
	}
}

func TestGuardWriter_UTF8Safe_Emoji(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var out bytes.Buffer
	emoji := "x\U0001f600y"
	gw := NewGuardWriter(&out, p, WithChunkSize(2))
	_, err := gw.Write([]byte(emoji))
	if err != nil {
		t.Fatal(err)
	}
	_ = gw.Close()
	if out.String() != emoji {
		t.Errorf("out = %q (emoji must not be corrupted)", out.String())
	}
}

func TestGuardWriter_SemanticBoundary_ForbiddenWord(t *testing.T) {
	v := &fakeValidator{
		name: "block",
		validate: func(_ context.Context, text string) (Report, error) {
			if strings.Contains(text, "bad") {
				return Report{Action: ActionBlock, Validator: "block"}, nil
			}
			return Report{Action: ActionPass, Validator: "block"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	var out bytes.Buffer
	gw := NewGuardWriter(&out, p, WithChunkSize(7))
	_, err := gw.Write([]byte("ok bad x"))
	if err == nil {
		t.Fatal("expected block when forbidden word 'bad' is in stream")
	}
	if !errors.Is(err, ErrBlocked) {
		t.Errorf("err = %v", err)
	}
	outStr := out.String()
	if outStr != "" && outStr != "ok " {
		t.Errorf("out = %q (blocked content)", outStr)
	}
}
