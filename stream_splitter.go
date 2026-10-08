package guardy

// frameScanner stores only fixed-size framing state. JSON syntax is checked
// separately before release; depth counts need no attacker-sized nesting stack.
type frameScanner struct {
	offset, objects, arrays                      int
	started, inString, escape, complete, invalid bool
	visited                                      uint64
}

func (f *frameScanner) reset() {
	visited := f.visited
	*f = frameScanner{
		offset:   0,
		objects:  0,
		arrays:   0,
		started:  false,
		inString: false,
		escape:   false,
		complete: false,
		invalid:  false,
		visited:  visited,
	}
}

func (f *frameScanner) newline(b *streamBuffer, final bool) int {
	for f.offset < b.size {
		c := b.at(f.offset)
		f.visited++
		f.offset++
		if c == '\n' {
			return f.offset
		}
	}
	if final {
		return b.size
	}
	return 0
}

func (f *frameScanner) json(b *streamBuffer, maxBytes int, final bool) (int, error) {
	for f.offset < b.size {
		c := b.at(f.offset)
		f.visited++
		if f.complete && !isJSONSpace(c) {
			return f.offset, nil
		}
		f.offset++
		if f.offset > maxBytes {
			return 0, ErrStreamUnitLimit
		}
		if f.complete {
			continue
		}
		skip, err := f.startJSON(c, maxBytes)
		if err != nil {
			return 0, err
		}
		if skip {
			continue
		}

		f.scanJSONByte(c)
		if f.invalid {
			return 0, ErrInvalidStreamUnit
		}
		if !f.complete && f.offset >= maxBytes {
			return 0, ErrStreamUnitLimit
		}
	}
	return f.finishJSON(b.size, maxBytes, final)
}

func (f *frameScanner) finishJSON(size, maxBytes int, final bool) (int, error) {
	if f.complete {
		if f.offset > maxBytes {
			return 0, ErrStreamUnitLimit
		}
		if final {
			return f.offset, nil
		}
		return 0, nil
	}
	if size >= maxBytes {
		return 0, ErrStreamUnitLimit
	}
	return 0, nil
}

func (f *frameScanner) scanJSONByte(c byte) {
	if f.invalid {
		return
	}
	if f.inString {
		if f.escape {
			f.escape = false
			return
		}
		switch c {
		case '\\':
			f.escape = true
		case '"':
			f.inString = false
		}
		return
	}
	switch c {
	case '"':
		f.inString = true
	case '{':
		f.objects++
	case '[':
		f.arrays++
	case '}':
		if f.objects == 0 {
			f.invalid = true
			return
		}
		f.objects--
	case ']':
		if f.arrays == 0 {
			f.invalid = true
			return
		}
		f.arrays--
	}
	f.complete = f.objects == 0 && f.arrays == 0
}

func (f *frameScanner) startJSON(c byte, maxBytes int) (bool, error) {
	if !f.started {
		if isJSONSpace(c) {
			if f.offset >= maxBytes {
				return false, ErrStreamUnitLimit
			}
			return true, nil
		}
		if c != '{' && c != '[' {
			return false, ErrInvalidStreamUnit
		}
		f.started = true
	}
	return false, nil
}
