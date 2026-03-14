package guardy

import (
	"context"
	"io"
	"sync"
	"time"
	"unicode/utf8"
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

// WithContext sets the context factory for chunk validation.
func WithContext(ctxFn func() (context.Context, context.CancelFunc)) StreamOption {
	return func(c *guardWriterConfig) {
		c.contextFn = ctxFn
	}
}

// WithTimeout sets a timeout for each chunk validation.
func WithTimeout(d time.Duration) StreamOption {
	return func(c *guardWriterConfig) {
		c.contextFn = func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), d)
		}
	}
}

// GuardWriter wraps an io.Writer and runs the pipeline on buffered chunks.
// On Block it returns ErrBlocked; on Redact it writes MutatedText.
type GuardWriter struct {
	w       io.Writer
	p       *Pipeline
	config  guardWriterConfig
	mu      sync.Mutex
	buf     []byte
	failErr error
}

// NewGuardWriter creates a GuardWriter that validates data in chunks before writing.
func NewGuardWriter(w io.Writer, p *Pipeline, opts ...StreamOption) *GuardWriter {
	if w == nil {
		panic("guardy: NewGuardWriter: writer is nil")
	}
	if p == nil {
		panic("guardy: NewGuardWriter: pipeline is nil")
	}
	config := guardWriterConfig{chunkSize: DefaultStreamChunkSize}
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

// Write buffers data and runs the pipeline when the buffer reaches chunk size or a natural boundary.
func (g *GuardWriter) Write(p []byte) (n int, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failErr != nil {
		return 0, g.failErr
	}
	g.buf = append(g.buf, p...)
	n = len(p)
	for len(g.buf) >= g.config.chunkSize {
		split := g.findChunkSplit()
		chunk := g.buf[:split]
		if err = g.validateAndWrite(chunk); err != nil {
			g.failErr = err
			return n, err
		}
		nCopied := copy(g.buf, g.buf[split:])
		g.buf = g.buf[:nCopied]
	}
	return n, nil
}

func (g *GuardWriter) findChunkSplit() int {
	limit := min(g.config.chunkSize, len(g.buf))
	window := g.buf[:limit]
	for i := len(window) - 1; i >= 0; i-- {
		if isBoundaryByte(window[i]) {
			return i + 1
		}
	}
	for i := limit; i > 0; i-- {
		if utf8.FullRune(window[:i]) {
			return i
		}
	}
	return 1
}

func isBoundaryByte(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	case '.', ',', '!', '?', ';', ':', '(', ')', '[', ']', '{', '}':
		return true
	default:
		return false
	}
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
	report, err := g.p.Run(ctx, string(chunk))
	if err != nil {
		return err
	}
	switch report.Action {
	case ActionBlock:
		return ErrBlocked
	case ActionRedact:
		// Always write MutatedText (including empty) to avoid leaking original chunk.
		_, err = g.w.Write([]byte(report.MutatedText))
		return err
	default:
		_, err = g.w.Write(chunk)
		return err
	}
}
