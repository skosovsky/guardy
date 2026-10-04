package guardy

import "strings"

// streamBuffer is a bounded ring. Consuming a unit never copies its tail.
// Allocation growth and immutable unit copies are counted for contract tests.
type streamBuffer struct {
	data       []byte
	head, size int
	copied     uint64
}

func (b *streamBuffer) at(i int) byte { return b.data[(b.head+i)%len(b.data)] }

func (b *streamBuffer) append(p []byte, maxBytes int) {
	required := b.size + len(p)
	if required > len(b.data) {
		capacity := maxBytes
		if len(b.data) <= maxBytes-len(b.data) {
			capacity = min(maxBytes, max(required, len(b.data)*2))
		}
		data := make([]byte, capacity)
		for i := range b.size {
			data[i] = b.at(i)
		}
		b.copied += uint64(uint(b.size))
		b.data, b.head = data, 0
	}
	if len(p) == 0 {
		return
	}
	tail := (b.head + b.size) % len(b.data)
	first := min(len(p), len(b.data)-tail)
	copy(b.data[tail:], p[:first])
	copy(b.data, p[first:])
	b.size += len(p)
	b.copied += uint64(len(p))
}

func (b *streamBuffer) prefix(n int) string {
	if n == 0 {
		return ""
	}
	var out strings.Builder
	out.Grow(n)
	first := min(n, len(b.data)-b.head)
	_, _ = out.Write(b.data[b.head : b.head+first])
	_, _ = out.Write(b.data[:n-first])
	b.copied += uint64(uint(n))
	return out.String()
}

func (b *streamBuffer) consume(n int) {
	b.size -= n
	b.head = (b.head + n) % len(b.data)
	if b.size == 0 {
		b.head = 0
	}
}

func (b *streamBuffer) clear() { b.data = nil; b.head, b.size = 0, 0 }

func isJSONSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}
