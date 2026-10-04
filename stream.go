package guardy

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
