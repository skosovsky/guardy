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

// defaultChunkValidationTimeout is used when WithContext/WithTimeout is not set.
const defaultChunkValidationTimeout = 5 * time.Second

type splitMode int

const (
	splitModeSemantic splitMode = iota
	splitModeJSONAware
)

// guardWriterConfig holds options for GuardWriter.
type guardWriterConfig struct {
	chunkSize    int
	maxChunkSize int
	contextFn    func() (context.Context, context.CancelFunc)
	splitMode    splitMode
	scope        ExecutionScope
}

type chunkSplitStrategy interface {
	nextChunk(data []byte, cfg guardWriterConfig) (split int, handled bool, err error)
	validateClose(data []byte, cfg guardWriterConfig) error
	overlapEnabled() bool
}

// GuardWriterOption configures GuardWriter behavior.
type GuardWriterOption func(*guardWriterConfig)

// WithChunkSize sets the buffer size in bytes before validation runs (default 4096).
func WithChunkSize(n int) GuardWriterOption {
	return func(c *guardWriterConfig) {
		c.chunkSize = n
	}
}

// WithMaxChunkSize sets the hard cap for delimiterless chunks (default 2048).
func WithMaxChunkSize(n int) GuardWriterOption {
	return func(c *guardWriterConfig) {
		c.maxChunkSize = n
	}
}

// WithContext sets the context factory for chunk validation.
func WithContext(ctxFn func() (context.Context, context.CancelFunc)) GuardWriterOption {
	return func(c *guardWriterConfig) {
		c.contextFn = ctxFn
	}
}

// WithTimeout sets a timeout for each chunk validation.
func WithTimeout(d time.Duration) GuardWriterOption {
	return func(c *guardWriterConfig) {
		c.contextFn = func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), d)
		}
	}
}

// WithJSONAwareSplitter switches GuardWriter to JSON-aware chunk splitting.
func WithJSONAwareSplitter() GuardWriterOption {
	return func(c *guardWriterConfig) {
		c.splitMode = splitModeJSONAware
	}
}

// WithExecutionScope sets the scope passed to [Pipeline.Run] for each chunk.
func WithExecutionScope(scope ExecutionScope) GuardWriterOption {
	return func(c *guardWriterConfig) {
		c.scope = scope
	}
}

// GuardWriter wraps an [io.Writer] and runs the pipeline on buffered chunks.
// On Block or Retry it returns [*StreamError] (unwraps to [ErrBlocked] or [ErrRetryRequested]).
// Use [errors.As] into [*PolicyFailure] to read the canonical decision without string parsing.
// On Redact it writes the mutated text.
// Uses index-based buffering to avoid O(N^2) copy; overlap prevents boundary bypass.
type GuardWriter struct {
	w        io.Writer
	p        *Pipeline[string]
	config   guardWriterConfig
	splitter chunkSplitStrategy
	mu       sync.Mutex
	buf      []byte
	start    int    // read position; compact when large
	overlap  []byte // tail of previous chunk for boundary detection
	failErr  error
}

// NewGuardWriter creates a GuardWriter that validates data in chunks before writing.
func NewGuardWriter(w io.Writer, p *Pipeline[string], opts ...GuardWriterOption) *GuardWriter {
	if w == nil {
		panic("guardy: NewGuardWriter: writer is nil")
	}
	if p == nil {
		panic("guardy: NewGuardWriter: pipeline is nil")
	}
	config := guardWriterConfig{
		chunkSize:    DefaultStreamChunkSize,
		maxChunkSize: DefaultStreamMaxChunkSize,
		splitMode:    splitModeSemantic,
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
	splitter := chunkSplitStrategy(semanticChunkSplitStrategy{})
	if config.splitMode == splitModeJSONAware {
		splitter = jsonAwareChunkSplitStrategy{}
	}
	return &GuardWriter{
		w:        w,
		p:        p,
		config:   config,
		splitter: splitter,
		buf:      make([]byte, 0, config.chunkSize*2),
	}
}

// Write buffers data and runs the pipeline when a splitter boundary is reached.
func (g *GuardWriter) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failErr != nil {
		return 0, g.failErr
	}
	g.buf = append(g.buf, p...)
	written := len(p)
	for {
		split, err := g.findChunkSplit()
		if err != nil {
			g.failErr = err
			return 0, err
		}
		if split == 0 {
			break
		}

		overlapLen := 0
		chunk := g.buf[g.start : g.start+split]
		if g.overlapEnabled() {
			overlapLen = len(g.overlap)
			withOverlap := make([]byte, overlapLen+split)
			copy(withOverlap, g.overlap)
			copy(withOverlap[overlapLen:], chunk)
			chunk = withOverlap
		} else {
			chunk = append([]byte(nil), chunk...)
		}

		overlap, err := g.validateAndWrite(chunk, overlapLen)
		if err != nil {
			g.failErr = err
			return 0, err
		}
		if g.overlapEnabled() {
			g.overlap = overlap
		} else {
			g.overlap = nil
		}
		g.start += split
		if g.start >= g.config.chunkSize {
			nCopied := copy(g.buf, g.buf[g.start:])
			g.buf = g.buf[:nCopied]
			g.start = 0
		}
	}
	return written, nil
}

func (g *GuardWriter) overlapEnabled() bool {
	if g.splitter == nil {
		return true
	}
	return g.splitter.overlapEnabled()
}

func (g *GuardWriter) findChunkSplit() (int, error) {
	data := g.buf[g.start:]
	split, handled, err := g.splitter.nextChunk(data, g.config)
	if err != nil {
		return 0, err
	}
	if handled {
		return split, nil
	}
	split, _, err = semanticChunkSplitStrategy{}.nextChunk(data, g.config)
	if err != nil {
		return 0, err
	}
	return split, nil
}

func jsonValueBoundary(data []byte) int {
	first := firstNonWhitespace(data)
	if first < 0 {
		return 0
	}

	var (
		inString bool
		escape   bool
		depthObj int
		depthArr int
	)

	switch data[first] {
	case '{':
		depthObj = 1
	case '[':
		depthArr = 1
	default:
		return 0
	}

	for i := first + 1; i < len(data); i++ {
		b := data[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if b == '\\' {
				escape = true
				continue
			}
			if b == '"' {
				inString = false
			}
			continue
		}

		switch b {
		case '"':
			inString = true
		case '{':
			depthObj++
		case '}':
			if depthObj == 0 {
				return 0
			}
			depthObj--
		case '[':
			depthArr++
		case ']':
			if depthArr == 0 {
				return 0
			}
			depthArr--
		}

		if depthObj == 0 && depthArr == 0 && !inString {
			end := i + 1
			for end < len(data) && isJSONSpace(data[end]) {
				end++
			}
			return end
		}
	}
	return 0
}

func firstNonWhitespace(data []byte) int {
	for i := range data {
		if !isJSONSpace(data[i]) {
			return i
		}
	}
	return -1
}

func isJSONSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
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
	if len(remain) == 0 && len(g.overlap) == 0 {
		return nil
	}
	if err := g.splitter.validateClose(remain, g.config); err != nil {
		g.failErr = err
		return err
	}

	overlapLen := 0
	chunk := append([]byte(nil), remain...)
	if g.overlapEnabled() && len(g.overlap) > 0 {
		overlapLen = len(g.overlap)
		withOverlap := make([]byte, overlapLen+len(remain))
		copy(withOverlap, g.overlap)
		copy(withOverlap[overlapLen:], remain)
		chunk = withOverlap
	}

	_, err := g.validateAndWrite(chunk, overlapLen)
	if err != nil {
		g.failErr = err
		return err
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
		ctx, cancel = context.WithTimeout(context.Background(), defaultChunkValidationTimeout)
	}
	defer cancel()

	result, err := g.p.Run(ctx, g.config.scope, string(chunk))
	if err != nil {
		return nil, err
	}
	rep := result.Decision()
	var output []byte
	if rep.IsRetryableCorrection() || rep.IsTerminalDeny() {
		return nil, streamErrorFromDecision(rep)
	}
	output = []byte(result.Output)

	if overlapLen < len(output) {
		if _, err = g.w.Write(output[overlapLen:]); err != nil {
			return nil, err
		}
	}

	overlap := output
	if len(overlap) > streamOverlapSize {
		overlap = overlap[len(overlap)-streamOverlapSize:]
	}
	return append([]byte(nil), overlap...), nil
}
