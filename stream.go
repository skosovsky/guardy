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

// DefaultStreamMaxChunkSize is the hard cap for delimiterless chunks.
const DefaultStreamMaxChunkSize = 2048

// streamOverlapSize is bytes kept at chunk boundaries to detect patterns spanning splits.
const streamOverlapSize = 64

// guardWriterConfig holds options for GuardWriter.
type guardWriterConfig struct {
	chunkSize    int
	maxChunkSize int
	contextFn    func() (context.Context, context.CancelFunc)
}

// StreamOption configures GuardWriter behavior.
type StreamOption func(*guardWriterConfig)

// WithChunkSize sets the buffer size in bytes before validation runs (default 4096).
func WithChunkSize(n int) StreamOption {
	return func(c *guardWriterConfig) {
		c.chunkSize = n
	}
}

// WithMaxChunkSize sets the hard cap for delimiterless chunks (default 2048).
func WithMaxChunkSize(n int) StreamOption {
	return func(c *guardWriterConfig) {
		c.maxChunkSize = n
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
// On Block it returns ErrBlocked; on Redact it writes the mutated text.
// Uses index-based buffering to avoid O(N^2) copy; overlap prevents boundary bypass.
type GuardWriter struct {
	w       io.Writer
	p       *Pipeline[string]
	config  guardWriterConfig
	mu      sync.Mutex
	buf     []byte
	start   int    // read position; compact when large
	overlap []byte // tail of previous chunk for boundary detection
	failErr error
}

// NewGuardWriter creates a GuardWriter that validates data in chunks before writing.
func NewGuardWriter(w io.Writer, p *Pipeline[string], opts ...StreamOption) *GuardWriter {
	if w == nil {
		panic("guardy: NewGuardWriter: writer is nil")
	}
	if p == nil {
		panic("guardy: NewGuardWriter: pipeline is nil")
	}
	config := guardWriterConfig{
		chunkSize:    DefaultStreamChunkSize,
		maxChunkSize: DefaultStreamMaxChunkSize,
	}
	for _, opt := range opts {
		opt(&config)
	}
	if config.chunkSize <= 0 {
		config.chunkSize = DefaultStreamChunkSize
	}
	if config.maxChunkSize <= 0 {
		config.maxChunkSize = DefaultStreamMaxChunkSize
	}
	return &GuardWriter{
		w:      w,
		p:      p,
		config: config,
		buf:    make([]byte, 0, config.chunkSize*2),
	}
}

// Write buffers data and runs the pipeline when a natural boundary or hard cap is reached.
func (g *GuardWriter) Write(p []byte) (n int, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failErr != nil {
		return 0, g.failErr
	}
	g.buf = append(g.buf, p...)
	n = len(p)
	for {
		split := g.findChunkSplit()
		if split == 0 {
			break
		}
		chunk := make([]byte, len(g.overlap)+split)
		copy(chunk, g.overlap)
		copy(chunk[len(g.overlap):], g.buf[g.start:g.start+split])
		g.overlap, err = g.validateAndWrite(chunk, len(g.overlap))
		if err != nil {
			g.failErr = err
			// io.Writer: on error, n must be < len(p) (bytes actually written)
			return 0, err
		}
		g.start += split
		if g.start >= g.config.chunkSize {
			nCopied := copy(g.buf, g.buf[g.start:])
			g.buf = g.buf[:nCopied]
			g.start = 0
		}
	}
	return n, nil
}

func (g *GuardWriter) findChunkSplit() int {
	dataLen := len(g.buf) - g.start
	if dataLen == 0 {
		return 0
	}

	if dataLen >= g.config.chunkSize {
		window := g.buf[g.start : g.start+g.config.chunkSize]
		for i := len(window) - 1; i >= 0; i-- {
			if isBoundaryByte(window[i]) {
				return i + 1
			}
		}

		fallback := g.config.maxChunkSize
		if fallback <= 0 || fallback > g.config.chunkSize {
			fallback = g.config.chunkSize
		}
		return utf8SafePrefixLen(g.buf[g.start : g.start+fallback])
	}

	if g.config.maxChunkSize > 0 && dataLen >= g.config.maxChunkSize {
		window := g.buf[g.start : g.start+dataLen]
		for i := len(window) - 1; i >= 0; i-- {
			if isBoundaryByte(window[i]) {
				return 0
			}
		}

		return utf8SafePrefixLen(g.buf[g.start : g.start+g.config.maxChunkSize])
	}

	return 0
}

func utf8SafePrefixLen(window []byte) int {
	if len(window) == 0 || utf8.Valid(window) {
		return len(window)
	}

	minIndex := max(1, len(window)-utf8.UTFMax+1)
	for i := len(window) - 1; i >= minIndex; i-- {
		if utf8.Valid(window[:i]) {
			return i
		}
	}

	return len(window)
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
	remain := g.buf[g.start:]
	if len(g.overlap) > 0 || len(remain) > 0 {
		chunk := make([]byte, len(g.overlap)+len(remain))
		copy(chunk, g.overlap)
		copy(chunk[len(g.overlap):], remain)
		_, err := g.validateAndWrite(chunk, len(g.overlap))
		if err != nil {
			g.failErr = err
			return err
		}
	}
	g.buf = g.buf[:0]
	g.start = 0
	g.overlap = nil
	return nil
}

// validateAndWrite runs pipeline on chunk, writes output (skipping overlapLen bytes), returns new overlap.
func (g *GuardWriter) validateAndWrite(chunk []byte, overlapLen int) ([]byte, error) {
	var ctx context.Context
	var cancel context.CancelFunc
	if g.config.contextFn != nil {
		ctx, cancel = g.config.contextFn()
	} else {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	}
	defer cancel()
	result, err := g.p.Run(ctx, string(chunk))
	if err != nil {
		return nil, err
	}
	rep := result.Decision()
	var output []byte
	switch rep.Action {
	case ActionBlock:
		return nil, ErrBlocked
	case ActionRetry:
		return nil, ErrRetryRequested
	case ActionRedact:
		output = []byte(result.Output)
	default:
		output = chunk
	}
	if overlapLen < len(output) {
		if _, err = g.w.Write(output[overlapLen:]); err != nil {
			return nil, err
		}
	}
	// New overlap: last streamOverlapSize bytes of output (copy to avoid retaining large buffer)
	overlap := output
	if len(overlap) > streamOverlapSize {
		overlap = overlap[len(overlap)-streamOverlapSize:]
	}
	return append([]byte(nil), overlap...), nil
}
