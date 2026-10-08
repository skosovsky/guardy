package ext

import (
	"cmp"
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/skosovsky/guardy"
)

type piiValidator struct {
	cfg RuleConfig
}

// Ensure PII validator implements guardy.Validator[string] at compile time.
var _ guardy.Validator[string] = (*piiValidator)(nil)

// Shape matchers for the documented finite formats, not identity validators.
var (
	phonePattern        = regexp.MustCompile(`(?:\+?1[ .-]?)?(?:\([0-9]{3}\)|[0-9]{3})[ .-]?[0-9]{3}[ .-]?[0-9]{4}`)
	russianPhonePattern = regexp.MustCompile(
		`\+7[ .-]?(?:\([0-9]{3}\)|[0-9]{3})[ .-]?[0-9]{3}[ .-]?[0-9]{2}[ .-]?[0-9]{2}`,
	)
	cardPattern = regexp.MustCompile(
		`[45][0-9]{3}(?: [0-9]{4}){3}|[45][0-9]{3}(?:-[0-9]{4}){3}|[45][0-9]{12,18}`,
	)
)

const defaultPIIValidatorName = "pii_validator"

// NewPIIValidator matches a finite set of ASCII email, phone and card patterns.
// Phones support 10-digit NANP numbers with optional +1/1, and +7 numbers with
// 3-3-2-2 grouping. Separators may be space, dot or hyphen, with optional area parentheses.
// Cards support Visa 13/16/19 contiguous digits and MasterCard 51-55 with 16 digits;
// 16-digit cards also support four groups separated consistently by space or hyphen.
// Card matching checks shape/prefix, not Luhn or issuance. Matches must be bounded
// by non-word characters. Spans are found on original text and overlapping spans
// merge before literal replacement/token storage; generated text is never rescanned.
// International email, other country codes/card families and arbitrary formatting
// are unsupported. This matcher neither establishes trust nor provides global DLP.
func NewPIIValidator(opts ...Option) (guardy.Validator[string], error) {
	cfg, err := applyOptions("pii", RuleConfig{
		Action:               guardy.ActionRedact,
		Severity:             guardy.SeverityHigh,
		Name:                 defaultPIIValidatorName,
		RedactionReplacement: defaultRedactionReplacement,
	}, opts...)
	if err != nil {
		return nil, err
	}
	if err := validateRuleOptions("pii", cfg); err != nil {
		return nil, err
	}

	return &piiValidator{
		cfg: cfg,
	}, nil
}

// MustPIIValidator panics when NewPIIValidator rejects configuration.
func MustPIIValidator(opts ...Option) guardy.Validator[string] {
	v, err := NewPIIValidator(opts...)
	if err != nil {
		panic(err)
	}
	return v
}

func (p *piiValidator) Validate(ctx context.Context, input string) (string, *guardy.Report, error) {
	if err := ctx.Err(); err != nil {
		return input, nil, err
	}
	spans := piiSpans(input)
	if len(spans) == 0 {
		return input, passReport(p.cfg), nil
	}
	if p.cfg.Action == guardy.ActionBlock {
		return input, violationReport(p.cfg, guardy.ActionBlock, "PII detected"), nil
	}
	output, err := replaceSpans(input, spans, func(original string) (string, error) {
		return storeRedaction(p.cfg.TokenVault, TokenNamespacePII, original, p.cfg.RedactionReplacement)
	})
	if fault := errors.Join(err, ctx.Err()); fault != nil {
		return input, nil, fault
	}
	report := violationReport(p.cfg, guardy.ActionRedact, "PII detected")
	report.MutatedText = output
	return output, report, nil
}

func piiSpans(input string) []textSpan {
	spans := emailSpans(input)
	for _, pattern := range []*regexp.Regexp{phonePattern, russianPhonePattern, cardPattern} {
		spans = appendPatternSpans(spans, input, pattern)
	}
	slices.SortFunc(spans, func(a, b textSpan) int {
		if a.start != b.start {
			return cmp.Compare(a.start, b.start)
		}
		return cmp.Compare(b.end, a.end)
	})
	merged := make([]textSpan, 0, len(spans))
	for _, span := range spans {
		if len(merged) > 0 && span.start < merged[len(merged)-1].end {
			merged[len(merged)-1].end = max(merged[len(merged)-1].end, span.end)
		} else {
			merged = append(merged, span)
		}
	}
	return merged
}

func piiBoundary(input string, start, end int) bool {
	if start > 0 {
		before, _ := utf8.DecodeLastRuneInString(input[:start])
		if isWordRune(before) {
			return false
		}
	}
	if end < len(input) {
		after, _ := utf8.DecodeRuneInString(input[end:])
		if isWordRune(after) {
			return false
		}
	}
	return true
}

func isWordRune(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsNumber(value) || unicode.IsMark(value) || value == '_'
}

func supportedCard(raw string) bool {
	digits := strings.ReplaceAll(strings.ReplaceAll(raw, " ", ""), "-", "")
	if digits[0] == '4' {
		return len(digits) == 13 || len(digits) == 16 || len(digits) == 19
	}
	return len(digits) == 16 && digits[0] == '5' && digits[1] >= '1' && digits[1] <= '5'
}

// nextPIIStart revisits overlapping candidates at possible non-word boundaries,
// without retrying each character of a long word/email local part.
func nextPIIStart(input string, start int) int {
	offset := start + 1 // All patterns start on an ASCII byte.
	for offset < len(input) {
		before, _ := utf8.DecodeLastRuneInString(input[:offset])
		if !isWordRune(before) {
			break
		}
		_, size := utf8.DecodeRuneInString(input[offset:])
		offset += size
	}
	return offset
}

// emailSpans visits each @ once. Adjacent email shapes may overlap in domain/local
// text; scanning by separators finds their union without repeatedly scanning suffixes.
func emailSpans(input string) []textSpan {
	var spans []textSpan
	for at := range len(input) {
		if input[at] != '@' {
			continue
		}
		start := at
		for start > 0 && emailLocalByte(input[start-1]) {
			start--
		}
		end := emailDomainEnd(input, at+1)
		if end == 0 {
			continue
		}
		for start < at && !piiBoundary(input, start, end) {
			start = nextPIIStart(input, start)
		}
		if start < at && piiBoundary(input, start, end) {
			spans = append(spans, textSpan{start: start, end: end})
		}
	}
	return spans
}

func emailLocalByte(value byte) bool {
	return asciiLetter(value) || asciiDigit(value) || strings.IndexByte("._%+-", value) >= 0
}
func emailDomainByte(value byte) bool {
	return asciiLetter(value) || asciiDigit(value) || value == '.' || value == '-'
}
func asciiLetter(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}
func asciiDigit(value byte) bool { return value >= '0' && value <= '9' }

// emailDomainEnd selects the longest qualifying alphabetic-TLD prefix, even when
// unsupported suffixes follow at a non-word boundary (for example .42 or -next).
func emailDomainEnd(input string, start int) int {
	const minTLDLength = 2
	dot, tldLetters, candidate := -1, -1, 0
	for end := start; end < len(input) && emailDomainByte(input[end]); {
		value := input[end]
		switch {
		case value == '.':
			dot, tldLetters = end, 0
		case asciiLetter(value) && tldLetters >= 0:
			tldLetters++
		default:
			tldLetters = -1
		}
		end++
		if dot <= start || tldLetters < minTLDLength {
			continue
		}
		if end == len(input) {
			candidate = end
			continue
		}
		next, _ := utf8.DecodeRuneInString(input[end:])
		if !isWordRune(next) {
			candidate = end
		}
	}
	return candidate
}

func appendPatternSpans(spans []textSpan, input string, pattern *regexp.Regexp) []textSpan {
	offset := 0
	for offset < len(input) {
		match := pattern.FindStringIndex(input[offset:])
		if match == nil {
			break
		}
		start, end := offset+match[0], offset+match[1]
		valid := piiBoundary(input, start, end)
		if pattern == phonePattern && start > 0 && input[start-1] == '+' && input[start] != '+' {
			valid = false
		}
		if pattern == cardPattern && !supportedCard(input[start:end]) {
			valid = false
		}
		if valid {
			spans = append(spans, textSpan{start: start, end: end})
		}
		offset = nextPIIStart(input, start)
	}
	return spans
}
