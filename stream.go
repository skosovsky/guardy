package guardy

import (
	"context"
	"io"
	"sync"
	"time"
)

// DefaultStreamChunkSize is the default buffer size for GuardWriter.
const DefaultStreamChunkSize = 4096

// guardWriterConfig holds options for GuardWriter.
type guardWriterConfig struct {
	chunkSize int
	contextFn func() (context.Context, context.CancelFunc)
}

// StreamOption configures GuardWriter behavior.
type StreamOption func(*guardWriterConfig)

// WithChunkSize sets the buffer size in bytes before validation runs (default 4096).
func WithChunkSize(n int) StreamOption {
	return func(c *guardWriterConfig) {
		c.chunkSize = n
	}
}

// WithContext sets the context factory for chunk validation. The function is called
// for each chunk; the returned cancel must be called when done. If not set,
// context.Background() with a 5-second timeout is used.
func WithContext(ctxFn func() (context.Context, context.CancelFunc)) StreamOption {
	return func(c *guardWriterConfig) {
		c.contextFn = ctxFn
	}
}

// WithTimeout sets a timeout for each chunk validation. It is a convenience for
// WithContext using context.Background(). If both WithContext and WithTimeout
// are used, the last one wins.
func WithTimeout(d time.Duration) StreamOption {
	return func(c *guardWriterConfig) {
		c.contextFn = func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), d)
		}
	}
}

// GuardWriter wraps an io.Writer and runs the pipeline on buffered chunks.
// On Block it returns an error and ignores further writes; on Redact it writes CleanText.
// After any failure, subsequent Write/Close return the same error (ErrBlocked, ErrOverridden, or system error).
type GuardWriter struct {
	w       io.Writer
	p       *Pipeline
	config  guardWriterConfig
	mu      sync.Mutex
	buf     []byte
	failErr error
}

// NewGuardWriter creates a GuardWriter that validates data in chunks before writing.
// Panics if w or p is nil (programmer error; fail-fast).
func NewGuardWriter(w io.Writer, p *Pipeline, opts ...StreamOption) *GuardWriter {
	if w == nil {
		panic("guardy: NewGuardWriter: writer is nil")
	}
	if p == nil {
		panic("guardy: NewGuardWriter: pipeline is nil")
	}
	config := guardWriterConfig{
		chunkSize: DefaultStreamChunkSize,
	}
	for _, opt := range opts {
		opt(&config)
	}
	if config.chunkSize <= 0 {
		config.chunkSize = DefaultStreamChunkSize
	}
	return &GuardWriter{
		w:      w,
		p:      p,
		config: config,
		buf:    make([]byte, 0, config.chunkSize*2),
	}
}

// Write buffers data and runs the pipeline when the buffer reaches chunk size.
// On validation error (e.g. Block), it returns (n, err) where n is the number of bytes
// accepted from p; some data may already have been written to the underlying writer.
func (g *GuardWriter) Write(p []byte) (n int, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failErr != nil {
		return 0, g.failErr
	}
	g.buf = append(g.buf, p...)
	n = len(p)
	for len(g.buf) >= g.config.chunkSize {
		chunk := g.buf[:g.config.chunkSize]
		if err = g.validateAndWrite(chunk); err != nil {
			g.failErr = err
			return n, err
		}
		// Copy remaining bytes to front to reuse buffer capacity instead of re-slicing
		nCopied := copy(g.buf, g.buf[g.config.chunkSize:])
		g.buf = g.buf[:nCopied]
	}
	return n, nil
}

// Close flushes the remaining buffer and validates it.
func (g *GuardWriter) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failErr != nil {
		return g.failErr
	}
	if len(g.buf) > 0 {
		if err := g.validateAndWrite(g.buf); err != nil {
			g.failErr = err
			return err
		}
		g.buf = g.buf[:0]
	}
	return nil
}

func (g *GuardWriter) validateAndWrite(chunk []byte) error {
	var ctx context.Context
	var cancel context.CancelFunc
	if g.config.contextFn != nil {
		ctx, cancel = g.config.contextFn()
	} else {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	}
	defer cancel()
	input := Input{Text: string(chunk)}
	report, err := g.p.Run(ctx, input)
	if err != nil {
		return err
	}
	switch report.FinalAction {
	case Block:
		return ErrBlocked
	case Override:
		return ErrOverridden
	case Redact:
		_, err = g.w.Write([]byte(report.FinalText))
		return err
	case Pass, Retry:
		_, err = g.w.Write(chunk)
		return err
	default:
		_, err = g.w.Write(chunk)
		return err
	}
}
