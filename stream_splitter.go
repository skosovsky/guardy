package guardy

import "errors"

// nextJSONUnit reuses the object/array framing scanner. Syntactic JSON validity
// is independently checked before and after validation by StreamProcessor.
func nextJSONUnit(data []byte, maxBytes int) (int, error) {
	first := firstNonWhitespace(data)
	if first < 0 {
		if len(data) >= maxBytes {
			return 0, errors.New("guardy: JSON whitespace exceeds unit limit")
		}
		return 0, nil
	}
	if data[first] != '{' && data[first] != '[' {
		return 0, errors.New("guardy: JSON unit must be object or array")
	}
	n := jsonValueBoundary(data)
	if n > 0 {
		if n > maxBytes {
			return 0, errors.New("guardy: JSON unit exceeds limit")
		}
		return n, nil
	}
	if len(data) >= maxBytes {
		return 0, errors.New("guardy: incomplete JSON exceeds unit limit")
	}
	return 0, nil
}
