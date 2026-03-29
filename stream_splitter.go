package guardy

import "fmt"

type semanticChunkSplitStrategy struct{}

func (semanticChunkSplitStrategy) overlapEnabled() bool {
	return true
}

func (semanticChunkSplitStrategy) validateClose(_ []byte, _ guardWriterConfig) error {
	return nil
}

func (semanticChunkSplitStrategy) nextChunk(
	data []byte,
	cfg guardWriterConfig,
) (int, bool, error) {
	chunkSize, maxChunkSize := normalizeChunkConfig(cfg)
	dataLen := len(data)
	if dataLen == 0 {
		return 0, true, nil
	}

	if dataLen >= chunkSize {
		window := data[:chunkSize]
		for i := len(window) - 1; i >= 0; i-- {
			if isBoundaryByte(window[i]) {
				return i + 1, true, nil
			}
		}

		fallback := maxChunkSize
		if fallback <= 0 || fallback > chunkSize {
			fallback = chunkSize
		}
		return utf8SafePrefixLen(data[:fallback]), true, nil
	}

	if maxChunkSize > 0 && dataLen >= maxChunkSize {
		for i := len(data) - 1; i >= 0; i-- {
			if isBoundaryByte(data[i]) {
				return 0, true, nil
			}
		}
		return utf8SafePrefixLen(data[:maxChunkSize]), true, nil
	}

	return 0, true, nil
}

type jsonAwareChunkSplitStrategy struct{}

func (jsonAwareChunkSplitStrategy) overlapEnabled() bool {
	return false
}

func (jsonAwareChunkSplitStrategy) validateClose(data []byte, _ guardWriterConfig) error {
	rest := data
	for {
		first := firstNonWhitespace(rest)
		if first < 0 {
			return nil
		}
		rest = rest[first:]
		if len(rest) == 0 {
			return nil
		}
		if rest[0] != '{' && rest[0] != '[' {
			// Non-JSON tail: semantic fallback is valid for this mode.
			return nil
		}
		split := jsonValueBoundary(rest)
		if split == 0 {
			return fmt.Errorf("%w: json stream ended with incomplete value", ErrValidatorFailed)
		}
		rest = rest[split:]
	}
}

func (jsonAwareChunkSplitStrategy) nextChunk(
	data []byte,
	cfg guardWriterConfig,
) (int, bool, error) {
	if len(data) == 0 {
		return 0, true, nil
	}

	first := firstNonWhitespace(data)
	if first < 0 {
		return 0, true, nil
	}
	if data[first] != '{' && data[first] != '[' {
		// Non-JSON prefix: fallback to semantic splitter behavior.
		return 0, false, nil
	}

	splitAt := jsonValueBoundary(data)
	if splitAt > 0 {
		return splitAt, true, nil
	}

	_, maxChunkSize := normalizeChunkConfig(cfg)
	if len(data) >= maxChunkSize {
		return 0, true, fmt.Errorf("%w: json fragment exceeds max chunk size before completion", ErrValidatorFailed)
	}
	return 0, true, nil
}

func normalizeChunkConfig(cfg guardWriterConfig) (int, int) {
	chunkSize := cfg.chunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultStreamChunkSize
	}
	maxChunkSize := cfg.maxChunkSize
	if maxChunkSize <= 0 {
		maxChunkSize = DefaultStreamMaxChunkSize
	}
	return chunkSize, maxChunkSize
}
