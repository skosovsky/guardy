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
		validate: func(_ context.Context, in *Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
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
		validate: func(_ context.Context, in *Input) (Result, error) {
			if in != nil && strings.Contains(in.Data, "x") {
				return Result{Passed: false, Action: Block}, nil
			}
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
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
		validate: func(_ context.Context, in *Input) (Result, error) {
			return Result{
				Passed:    false,
				Action:    Redact,
				CleanText: "[CLEAN]",
			}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	var out bytes.Buffer
	gw := NewGuardWriter(&out, p, WithChunkSize(10))
	_, _ = gw.Write([]byte("dirty"))
	_ = gw.Close()
	if out.String() != "[CLEAN]" {
		t.Errorf("out = %q", out.String())
	}
}

func TestGuardWriter_FlushOnClose(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(_ context.Context, in *Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
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

func TestGuardWriter_Override(t *testing.T) {
	v := &fakeValidator{
		name: "override",
		validate: func(_ context.Context, in *Input) (Result, error) {
			return Result{
				Passed:       false,
				Action:       Override,
				OverrideText: "replaced",
			}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	var out bytes.Buffer
	gw := NewGuardWriter(&out, p, WithChunkSize(10))
	_, _ = gw.Write([]byte("trigger")) // 7 bytes, below chunk size
	err := gw.Close()                  // flush triggers validation
	if err == nil {
		t.Fatal("expected ErrOverridden")
	}
	if !errors.Is(err, ErrOverridden) {
		t.Errorf("err = %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("Override should not write chunk to output")
	}
}

func TestGuardWriter_WriteReturnsNAcceptedOnError(t *testing.T) {
	v := &fakeValidator{
		name: "block",
		validate: func(_ context.Context, in *Input) (Result, error) {
			if in != nil && in.Data == "bb" {
				return Result{Passed: false, Action: Block}, nil
			}
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	var out bytes.Buffer
	gw := NewGuardWriter(&out, p, WithChunkSize(2))
	_, _ = gw.Write([]byte("aa")) // ok
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
		validate: func(_ context.Context, _ *Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
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
		validate: func(_ context.Context, _ *Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
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

func TestGuardWriter_WithMetadata(t *testing.T) {
	meta := map[string]any{"k": "v"}
	var metadataSeen atomic.Bool
	v := &fakeValidator{
		name: "meta-check",
		validate: func(_ context.Context, in *Input) (Result, error) {
			if in != nil && in.Metadata != nil && in.Metadata["k"] == "v" {
				metadataSeen.Store(true)
			}
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	var out bytes.Buffer
	gw := NewGuardWriter(&out, p, WithChunkSize(10), WithMetadata(meta))
	_, err := gw.Write([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	_ = gw.Close()
	if out.String() != "hello" {
		t.Errorf("out = %q", out.String())
	}
	if !metadataSeen.Load() {
		t.Error("WithMetadata: validator did not receive Input.Metadata")
	}
}

func TestGuardWriter_ChunkSizeZeroOrNegative_UsesDefault(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(_ context.Context, _ *Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
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

// TestGuardWriter_UTF8Safe_Cyrillic ensures multi-byte runes (e.g. Cyrillic) are not split across chunks.
func TestGuardWriter_UTF8Safe_Cyrillic(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(_ context.Context, in *Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	var out bytes.Buffer
	// Chunk size 3: "привет" is 12 bytes (6 runes x 2). With size 3 we must not cut in the middle of a rune.
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

// TestGuardWriter_UTF8Safe_Emoji ensures emoji (4-byte runes) are not split.
func TestGuardWriter_UTF8Safe_Emoji(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(_ context.Context, _ *Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	var out bytes.Buffer
	emoji := "x\U0001f600y" // 1 + 4 + 1 bytes
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

// TestGuardWriter_SemanticBoundary_ForbiddenWord ensures a forbidden word is not split across chunks
// when a semantic boundary (space) keeps it in one chunk, so the validator can detect it.
func TestGuardWriter_SemanticBoundary_ForbiddenWord(t *testing.T) {
	v := &fakeValidator{
		name: "block",
		validate: func(_ context.Context, in *Input) (Result, error) {
			if in != nil && strings.Contains(in.Data, "bad") {
				return Result{Passed: false, Action: Block}, nil
			}
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	var out bytes.Buffer
	// Chunk size 7: "ok bad x" -> first chunk up to last boundary in 7 bytes: "ok bad " (space at 6), so chunk "ok bad "
	// Validator sees "ok bad " and blocks.
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
