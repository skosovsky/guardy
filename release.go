package guardy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"
	"unicode/utf8"
)

// ReleaseProfile is an explicit promise about when bytes become irreversible.
type ReleaseProfile string

const (
	ReleaseWholeResponse  ReleaseProfile = "whole_response"
	ReleaseValidatedUnits ReleaseProfile = "validated_units"
	ReleaseBestEffort     ReleaseProfile = "best_effort"
)

// StreamStage is supplied by trusted processor control, never by input content.
type StreamStage string

const (
	StreamPartial StreamStage = "partial"
	StreamUnit    StreamStage = "unit"
	StreamFinal   StreamStage = "final"
)

type streamStageKey struct{}

// StreamValidationStage returns the current trusted validation stage.
func StreamValidationStage(ctx context.Context) StreamStage {
	stage, _ := ctx.Value(streamStageKey{}).(StreamStage)
	return stage
}

// StreamCapabilities declares rule requirements, not a proof of detector quality.
// The initial unit splitter supports unit-local rules (zero lookbehind/lookahead).
type StreamCapabilities struct {
	Partial, Unit, Final  bool
	Lookbehind, Lookahead int
}

type streamCapable interface{ StreamCapabilities() StreamCapabilities }

type streamingRule struct {
	Validator[string]

	capability StreamCapabilities
}

func (v streamingRule) StreamCapabilities() StreamCapabilities { return v.capability }

type streamingPolicy struct {
	PolicyValidator[string]

	capability StreamCapabilities
}

func (v streamingPolicy) StreamCapabilities() StreamCapabilities { return v.capability }

// WithStreamingPolicyCapabilities declares stage support for a scoped policy.
func WithStreamingPolicyCapabilities(v PolicyValidator[string], capability StreamCapabilities) PolicyValidator[string] {
	return streamingPolicy{PolicyValidator: v, capability: capability}
}

// WithStreamingCapabilities associates a caller-declared capability with a rule.
// The declaration must reflect the validator's actual contextual requirements.
func WithStreamingCapabilities(v Validator[string], capability StreamCapabilities) Validator[string] {
	return streamingRule{Validator: v, capability: capability}
}

// StreamCategory is independent of the downstream transport's error text.
type StreamCategory string

const (
	StreamSuccess     StreamCategory = "success"
	StreamIncomplete  StreamCategory = "incomplete_unit"
	StreamLimit       StreamCategory = "buffer_limit"
	StreamTimeout     StreamCategory = "validation_timeout"
	StreamFault       StreamCategory = "validator_fault"
	StreamUnsupported StreamCategory = "unsupported_release_profile"
	StreamBlocked     StreamCategory = "blocked_delivery"
	StreamTransport   StreamCategory = "delivery_failure"
)

// StreamOutcome is a snapshot. ReleasedBytes measures actual transport writes;
// Sequence numbers approved attempts, not a claim that failed writes were complete.
// PeakPendingBytes counts the maximum retained ring allocation, including unused capacity.
type StreamOutcome struct {
	Identity         string
	Category         StreamCategory
	Decision         Decision
	Sequence         uint64
	ReleasedBytes    int64
	ReceivedBytes    int64
	PeakPendingBytes int
	Terminal         bool
	Fallback         bool
}

// ReleaseError preserves the terminal outcome and canonical guard failure.
type ReleaseError struct {
	Outcome StreamOutcome
	Cause   error
	Failure PolicyFailure
}

func (e *ReleaseError) Error() string      { return "guardy stream: " + string(e.Outcome.Category) }
func (e *ReleaseError) Unwrap() error      { return e.Cause }
func (e *ReleaseError) As(target any) bool { return asPolicyFailure(target, &e.Failure) }

// StreamEvent intentionally has no raw payload or reports. Observers must not reenter.
type StreamEvent struct {
	Outcome StreamOutcome
	Stage   StreamStage
}

// StreamConfig owns bounded release configuration; all limits count bytes and
// must be positive. Policy and pipeline remain immutable after compilation.
type StreamConfig struct {
	Identity          string
	Profile           ReleaseProfile
	JSONValues        bool // Frame and validate complete JSON objects/arrays, not transport chunks.
	Pipeline          *Pipeline[string]
	ScopeFactory      ScopeFactory
	Delivery          DeliveryPolicy
	MaxInputBytes     int64
	MaxPendingBytes   int
	MaxUnitBytes      int
	MaxOutputBytes    int64
	ValidationTimeout time.Duration
	Observer          func(StreamEvent)
}

// StreamProcessor serializes all operations. A trusted Complete is required;
// Close aborts unfinished input. Processor owns pending bytes; caller owns transport.
type StreamProcessor struct {
	mu                sync.Mutex
	writer            io.Writer
	cfg               StreamConfig
	pending           streamBuffer
	framer            frameScanner
	outcome           StreamOutcome
	terminalErr       error
	fallbackAttempted bool
}

// CompileStream fails before delivery for unsupported profiles/capabilities.
// Whole-response accepts ordinary final-value validators. Unit and incremental
// rules must explicitly declare their stage. No downgrade is performed.
func CompileStream(writer io.Writer, cfg StreamConfig) (*StreamProcessor, error) {
	if writer == nil || cfg.Pipeline == nil || cfg.Identity == "" || cfg.MaxInputBytes <= 0 ||
		cfg.MaxPendingBytes <= 0 ||
		cfg.MaxUnitBytes <= 0 ||
		cfg.MaxOutputBytes <= 0 ||
		cfg.ValidationTimeout <= 0 {
		return nil, streamConfigurationError(errors.New("guardy: invalid stream configuration"))
	}
	if cfg.Profile != ReleaseWholeResponse && cfg.Profile != ReleaseValidatedUnits && cfg.Profile != ReleaseBestEffort {
		return nil, streamConfigurationError(errors.New("guardy: explicit release profile required"))
	}
	if cfg.Profile == ReleaseBestEffort && (cfg.JSONValues || cfg.MaxUnitBytes < utf8.UTFMax) {
		return nil, streamConfigurationError(errors.New("guardy: incompatible incremental framing or unit limit"))
	}
	if cfg.Profile == ReleaseValidatedUnits && cfg.JSONValues && cfg.MaxPendingBytes <= cfg.MaxUnitBytes {
		return nil, streamConfigurationError(
			errors.New("guardy: JSON framing requires pending capacity greater than unit limit"),
		)
	}
	if err := checkStreamCapabilities(cfg.Pipeline, cfg.Profile); err != nil {
		return nil, streamConfigurationError(err)
	}
	cfg.Delivery = normalizeDeliveryPolicy(cfg.Delivery)
	var outcome StreamOutcome
	outcome.Identity = cfg.Identity
	outcome.Decision = DecisionFromReport(nil)
	return &StreamProcessor{
		mu:      sync.Mutex{},
		writer:  writer,
		cfg:     cfg,
		pending: streamBuffer{data: nil, head: 0, size: 0, copied: 0},
		framer: frameScanner{
			offset:   0,
			objects:  0,
			arrays:   0,
			started:  false,
			inString: false,
			escape:   false,
			complete: false,
			invalid:  false,
			visited:  0,
		},
		terminalErr:       nil,
		fallbackAttempted: false,
		outcome:           outcome,
	}, nil
}

func checkStreamCapabilities(p *Pipeline[string], profile ReleaseProfile) error {
	rules := make([]any, 0, len(p.fastPath)+len(p.slowPath)+len(p.policyValidators))
	for _, rule := range p.fastPath {
		rules = append(rules, rule)
	}
	for _, rule := range p.slowPath {
		rules = append(rules, rule)
	}
	for _, rule := range p.policyValidators {
		rules = append(rules, rule)
	}
	// Check both the underlying rules and the actual middleware chains. A wrapper
	// cannot authorize a stage unsupported by its delegate or silently hide a
	// missing declaration. Middleware construction must be deterministic.
	for _, rule := range p.fastPathLayers {
		rules = append(rules, rule)
	}
	for _, rule := range p.slowPathLayers {
		rules = append(rules, rule)
	}
	_, policyLayers := p.buildPolicyChain(NewScope(), true)
	for _, rule := range policyLayers {
		rules = append(rules, rule)
	}
	for _, rule := range rules {
		if profile != ReleaseWholeResponse {
			if !supportsStreamRule(rule, profile) {
				return errors.New("guardy: unsupported rule capability")
			}
			continue
		}
		if c, ok := rule.(streamCapable); ok && !c.StreamCapabilities().Final {
			return errors.New("guardy: final stage unsupported")
		}
	}
	return nil
}

func supportsStreamRule(rule any, profile ReleaseProfile) bool {
	v, ok := rule.(streamCapable)
	if !ok {
		return false
	}
	c := v.StreamCapabilities()
	if c.Lookbehind != 0 || c.Lookahead != 0 {
		return false
	}
	if profile == ReleaseValidatedUnits {
		return c.Unit
	}
	return c.Partial
}

func streamConfigurationError(cause error) error {
	d := DecisionFromReport(nil)
	d.Disposition = DispositionSystemFault
	var o StreamOutcome
	o.Category, o.Decision, o.Terminal = StreamUnsupported, d, true
	return &ReleaseError{Outcome: o, Cause: cause, Failure: PolicyFailure{Decision: d, Cause: cause}}
}

// Write buffers input without exposing a whole-response prefix. Large caller
// chunks are processed in bounded slices; input size is checked before copying.
func (s *StreamProcessor) Write(p []byte) (int, error) {
	return s.WriteContext(context.Background(), p)
}

// WriteContext supplies cancellation to all validation triggered by this input.
func (s *StreamProcessor) WriteContext(ctx context.Context, p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.outcome.Terminal {
		if s.terminalErr != nil {
			return 0, s.terminalErr
		}
		return 0, io.ErrClosedPipe
	}
	if int64(len(p)) > s.cfg.MaxInputBytes-s.outcome.ReceivedBytes {
		return 0, s.fail(StreamLimit, errors.New("guardy: input limit"), DecisionFromReport(nil))
	}
	if err := ctx.Err(); err != nil {
		return 0, s.fail(StreamTimeout, err, DecisionFromReport(nil))
	}
	accepted := 0
	for len(p) > 0 {
		room := s.cfg.MaxPendingBytes - s.pending.size
		if room <= 0 {
			return accepted, s.fail(StreamLimit, errors.New("guardy: pending limit"), DecisionFromReport(nil))
		}
		n := min(room, len(p))
		s.pending.append(p[:n], s.cfg.MaxPendingBytes)
		p = p[n:]
		accepted += n
		s.outcome.ReceivedBytes += int64(n)
		s.outcome.PeakPendingBytes = max(s.outcome.PeakPendingBytes, len(s.pending.data))
		if s.cfg.Profile == ReleaseWholeResponse {
			continue
		}
		if err := s.releasePending(ctx, false); err != nil {
			return accepted, err
		}
	}
	return accepted, nil
}

func (s *StreamProcessor) releasePending(ctx context.Context, final bool) error {
	for s.pending.size > 0 {
		n, stage, err := s.nextUnit(final)
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		if n > s.cfg.MaxUnitBytes {
			return s.fail(StreamLimit, errors.New("guardy: unit limit"), DecisionFromReport(nil))
		}
		if err := s.validateAndRelease(ctx, s.pending.prefix(n), stage); err != nil {
			return err
		}
		s.pending.consume(n)
		s.framer.reset()
	}
	return nil
}

func (s *StreamProcessor) nextUnit(final bool) (int, StreamStage, error) {
	if s.cfg.Profile == ReleaseValidatedUnits && s.cfg.JSONValues {
		return s.nextJSONUnit(final)
	}
	if s.cfg.Profile == ReleaseValidatedUnits {
		n := s.framer.newline(&s.pending, final)
		if n == 0 && s.pending.size >= s.cfg.MaxUnitBytes {
			return 0, StreamUnit, s.fail(
				StreamLimit,
				errors.New("guardy: incomplete unit exceeds limit"),
				DecisionFromReport(nil),
			)
		}
		return n, StreamUnit, nil
	}
	if s.pending.size < s.cfg.MaxUnitBytes && !final {
		return 0, StreamPartial, nil
	}
	n := min(s.pending.size, s.cfg.MaxUnitBytes)
	for offset := 0; offset < n; {
		var runeBytes [utf8.UTFMax]byte
		available := min(utf8.UTFMax, n-offset)
		for i := range available {
			runeBytes[i] = s.pending.at(offset + i)
		}
		if !utf8.FullRune(runeBytes[:available]) {
			n = offset
			break
		}
		_, size := utf8.DecodeRune(runeBytes[:available])
		if size == 1 && runeBytes[0] >= utf8.RuneSelf {
			return 0, StreamPartial, s.fail(
				StreamIncomplete,
				errors.New("guardy: invalid UTF-8 unit"),
				DecisionFromReport(nil),
			)
		}
		offset += size
	}
	if n == 0 {
		if !final {
			return 0, StreamPartial, nil
		}
		return 0, StreamPartial, s.fail(
			StreamIncomplete,
			errors.New("guardy: invalid UTF-8 unit"),
			DecisionFromReport(nil),
		)
	}
	return n, StreamPartial, nil
}

func (s *StreamProcessor) nextJSONUnit(final bool) (int, StreamStage, error) {
	n, err := s.framer.json(&s.pending, s.cfg.MaxUnitBytes, final)
	if err != nil {
		return 0, StreamUnit, s.fail(StreamLimit, err, DecisionFromReport(nil))
	}
	if n == 0 && final {
		return 0, StreamUnit, s.fail(StreamIncomplete, io.ErrUnexpectedEOF, DecisionFromReport(nil))
	}
	return n, StreamUnit, nil
}

func newlineUnitBoundary(p []byte, final bool) int {
	for i, b := range p {
		if b == '\n' {
			return i + 1
		}
	}
	if final {
		return len(p)
	}
	return 0
}

// Complete is a trusted producer success signal. It never derives completion
// from payload content. Repeated terminal calls return the original outcome.
func (s *StreamProcessor) Complete(ctx context.Context) (StreamOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.outcome.Terminal {
		return s.outcome, s.terminalErr
	}
	if err := ctx.Err(); err != nil {
		return s.outcome, s.fail(StreamTimeout, err, DecisionFromReport(nil))
	}
	if s.cfg.Profile == ReleaseWholeResponse {
		if s.pending.size > s.cfg.MaxUnitBytes {
			return s.outcome, s.fail(StreamLimit, errors.New("guardy: final unit limit"), DecisionFromReport(nil))
		}
		if err := s.validateAndRelease(ctx, s.pending.prefix(s.pending.size), StreamFinal); err != nil {
			return s.outcome, err
		}
	} else if err := s.releasePending(ctx, true); err != nil {
		return s.outcome, err
	}
	s.pending.clear()
	s.framer.reset()
	s.outcome.Terminal = true
	s.outcome.Category = StreamSuccess
	s.observe(StreamFinal)
	return s.outcome, nil
}

// Abort discards pending data; it never flushes an incomplete whole response.
func (s *StreamProcessor) Abort(cause error) (StreamOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.outcome.Terminal {
		return s.outcome, s.terminalErr
	}
	if cause == nil {
		cause = io.ErrUnexpectedEOF
	}
	err := s.fail(StreamIncomplete, cause, DecisionFromReport(nil))
	return s.outcome, err
}

// Close marks an unfinished producer as incomplete; use Complete for success.
func (s *StreamProcessor) Close() error { _, err := s.Abort(nil); return err }

// Outcome returns a safe snapshot of counters and current/terminal state.
func (s *StreamProcessor) Outcome() StreamOutcome { s.mu.Lock(); defer s.mu.Unlock(); return s.outcome }

// DeliverFallback is a separate, at-most-once checked delivery after policy deny.
// It cannot turn a validator fault into success. The original outcome remains
// terminal; the returned outcome records only this additional delivery.
//
//nolint:funlen // Single checked fallback transaction keeps validation before irreversible delivery explicit.
func (s *StreamProcessor) DeliverFallback(ctx context.Context) (StreamOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.outcome.Category != StreamBlocked || s.fallbackAttempted {
		return s.outcome, errors.New("guardy: fallback unavailable")
	}
	s.fallbackAttempted = true
	candidate, ok := s.cfg.Delivery.Fallback.(string)
	if !ok {
		return s.outcome, errors.New("guardy: fallback not configured")
	}
	var outcome StreamOutcome
	outcome.Identity = s.cfg.Identity
	outcome.Fallback = true
	outcome.Terminal = true
	outcome.Decision = DecisionFromReport(nil)
	if len(candidate) > s.cfg.MaxUnitBytes || int64(len(candidate)) > s.cfg.MaxInputBytes {
		return s.fallbackFailure(outcome, StreamLimit, errors.New("guardy: fallback input limit"))
	}
	if !utf8.ValidString(candidate) || (s.cfg.JSONValues && !json.Valid([]byte(candidate))) {
		return s.fallbackFailure(outcome, StreamIncomplete, errors.New("guardy: invalid fallback input"))
	}
	if s.cfg.Profile == ReleaseValidatedUnits && !s.cfg.JSONValues &&
		newlineUnitBoundary([]byte(candidate), true) != len(candidate) {
		return s.fallbackFailure(
			outcome,
			StreamUnsupported,
			errors.New("guardy: fallback must be one self-contained release unit"),
		)
	}
	outcome.ReceivedBytes = int64(len(candidate))
	stage := StreamFinal
	switch s.cfg.Profile {
	case ReleaseWholeResponse:
		stage = StreamFinal
	case ReleaseValidatedUnits:
		stage = StreamUnit
	case ReleaseBestEffort:
		stage = StreamPartial
	}
	guardCtx, cancel := context.WithTimeout(
		context.WithValue(ctx, streamStageKey{}, stage),
		s.cfg.ValidationTimeout,
	)
	defer cancel()
	scope, err := s.cfg.ScopeFactory.scope(guardCtx)
	if err != nil {
		return s.fallbackFailure(outcome, StreamFault, err)
	}
	policy := s.cfg.Delivery
	policy.Fallback = nil
	checked, err := s.cfg.Pipeline.GuardDelivery(guardCtx, scope, policy, candidate)
	outcome.Decision = checked.Decision
	if guardCtx.Err() != nil {
		return s.fallbackFailure(outcome, StreamTimeout, guardCtx.Err())
	}
	if err != nil {
		category := StreamBlocked
		if checked.Decision.IsSystemFault() {
			category = StreamFault
		}
		return s.fallbackFailure(outcome, category, err)
	}
	value, ok := checked.DeliverableValue()
	if !ok {
		return s.fallbackFailure(outcome, StreamBlocked, ErrBlocked)
	}
	if !utf8.ValidString(value) || (s.cfg.JSONValues && !json.Valid([]byte(value))) {
		return s.fallbackFailure(outcome, StreamFault, errors.New("guardy: invalid sanitized fallback"))
	}
	if int64(len(value)) > s.cfg.MaxOutputBytes-s.outcome.ReleasedBytes {
		return s.fallbackFailure(outcome, StreamLimit, errors.New("guardy: fallback output limit"))
	}
	n, err := io.WriteString(s.writer, value)
	if n < 0 || n > len(value) {
		return s.fallbackFailure(outcome, StreamTransport, errors.New("guardy: invalid fallback byte count"))
	}
	outcome.Sequence = 1
	outcome.ReleasedBytes = int64(n)
	if err == nil && n != len(value) {
		err = io.ErrShortWrite
	}
	outcome.Category = StreamSuccess
	if err != nil {
		return s.fallbackFailure(outcome, StreamTransport, err)
	}
	s.observeOutcome(outcome, stage)
	return outcome, err
}

func (s *StreamProcessor) fallbackFailure(
	outcome StreamOutcome,
	category StreamCategory,
	cause error,
) (StreamOutcome, error) {
	if failure, ok := errors.AsType[*PolicyFailure](cause); ok {
		outcome.Decision = failure.Decision
	}
	if outcome.Decision.Disposition == DispositionNone && category != StreamTransport {
		outcome.Decision.Disposition = DispositionSystemFault
	}
	outcome.Category = category
	s.observeOutcome(outcome, StreamFinal)
	return outcome, &ReleaseError{
		Outcome: outcome,
		Cause:   cause,
		Failure: PolicyFailure{Decision: outcome.Decision, Cause: cause},
	}
}

func (s *StreamProcessor) validateAndRelease(parent context.Context, value string, stage StreamStage) error {
	if !utf8.ValidString(value) {
		return s.fail(StreamIncomplete, errors.New("guardy: invalid UTF-8"), DecisionFromReport(nil))
	}
	if s.cfg.JSONValues && !json.Valid([]byte(value)) {
		return s.fail(StreamIncomplete, errors.New("guardy: invalid JSON value"), DecisionFromReport(nil))
	}
	ctx, cancel := context.WithTimeout(context.WithValue(parent, streamStageKey{}, stage), s.cfg.ValidationTimeout)
	defer cancel()
	scope, err := s.cfg.ScopeFactory.scope(ctx)
	if err != nil {
		return s.fail(StreamFault, err, DecisionFromReport(nil))
	}
	// Stream fallback is handled by the processor as a distinct checked delivery.
	policy := s.cfg.Delivery
	policy.Fallback = nil
	delivery, err := s.cfg.Pipeline.GuardDelivery(ctx, scope, policy, value)
	if ctx.Err() != nil {
		return s.fail(StreamTimeout, ctx.Err(), DecisionFromReport(nil))
	}
	if err != nil {
		category := StreamBlocked
		if delivery.Decision.IsSystemFault() {
			category = StreamFault
		}
		return s.fail(category, err, delivery.Decision)
	}
	approved, ok := delivery.DeliverableValue()
	if !ok {
		return s.fail(StreamBlocked, ErrBlocked, delivery.Decision)
	}
	if !utf8.ValidString(approved) {
		return s.fail(StreamFault, errors.New("guardy: redaction corrupted UTF-8"), DecisionFromReport(nil))
	}
	if s.cfg.JSONValues && !json.Valid([]byte(approved)) {
		return s.fail(StreamFault, errors.New("guardy: redaction corrupted JSON"), DecisionFromReport(nil))
	}
	if int64(len(approved)) > s.cfg.MaxOutputBytes-s.outcome.ReleasedBytes {
		return s.fail(StreamLimit, errors.New("guardy: output limit"), DecisionFromReport(nil))
	}
	s.outcome.Decision = delivery.Decision
	s.outcome.Sequence++
	n, writeErr := io.WriteString(s.writer, approved)
	if n < 0 || n > len(approved) {
		return s.fail(StreamTransport, errors.New("guardy: invalid transport byte count"), delivery.Decision)
	}
	s.outcome.ReleasedBytes += int64(n)
	if writeErr == nil && n != len(approved) {
		writeErr = io.ErrShortWrite
	}
	if writeErr != nil {
		return s.fail(StreamTransport, writeErr, delivery.Decision)
	}
	s.observe(stage)
	return nil
}

func (s *StreamProcessor) fail(category StreamCategory, cause error, decision Decision) error {
	if pf, ok := errors.AsType[*PolicyFailure](cause); ok {
		decision = pf.Decision
	}
	if decision.Disposition == DispositionNone {
		decision.Disposition = DispositionSystemFault
	}
	s.pending.clear()
	s.framer.reset()
	s.outcome.Category = category
	s.outcome.Decision = decision
	s.outcome.Terminal = true
	s.terminalErr = &ReleaseError{
		Outcome: s.outcome,
		Cause:   cause,
		Failure: PolicyFailure{Decision: decision, Cause: cause},
	}
	s.observe(StreamFinal)
	return s.terminalErr
}

func (s *StreamProcessor) observe(stage StreamStage) {
	s.observeOutcome(s.outcome, stage)
}

func (s *StreamProcessor) observeOutcome(outcome StreamOutcome, stage StreamStage) {
	if s.cfg.Observer == nil {
		return
	}
	// Instrumentation cannot change enforcement even if an observer panics.
	func() {
		defer func() { _ = recover() }()
		outcome.Decision.RetryFeedback = ""
		s.cfg.Observer(StreamEvent{Outcome: outcome, Stage: stage})
	}()
}
