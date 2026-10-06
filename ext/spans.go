package ext

import "strings"

type textSpan struct {
	start int
	end   int
}

// replaceSpans consumes ordered non-overlapping original byte ranges once.
func replaceSpans(input string, spans []textSpan, replace func(string) (string, error)) (string, error) {
	var output strings.Builder
	offset := 0
	for _, span := range spans {
		output.WriteString(input[offset:span.start])
		replacement, err := replace(input[span.start:span.end])
		if err != nil {
			return input, err
		}
		output.WriteString(replacement)
		offset = span.end
	}
	output.WriteString(input[offset:])
	return output.String(), nil
}
