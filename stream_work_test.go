package guardy

import (
	"fmt"
	"strings"
	"testing"
)

func TestIncrementalFramingAndBufferWorkBound(t *testing.T) {
	for _, size := range []int{128, 1024, 8192} {
		for _, tc := range []struct {
			name, input string
			json        bool
		}{
			{"newline", strings.Repeat("x", size) + "\n", false},
			{"json", `{"text":"` + strings.Repeat(`a\"`, size) + `","nested":[{}]}`, true},
			{"unfinished", `{"text":"` + strings.Repeat("x", size), true},
			{"trailing", `{}` + strings.Repeat(" ", size), true},
			{"many", strings.Repeat("a\n", size), false},
			{"many-json", strings.Repeat("{} ", size), true},
		} {
			for _, split := range []int{1, 17, len(tc.input)} {
				t.Run(fmt.Sprintf("%s/size=%d/split=%d", tc.name, size, split), func(t *testing.T) {
					verifyFramingWork(t, tc.name, tc.input, tc.json, split)
				})
			}
		}
	}
}

func TestRingBufferWraparoundHasNoTailCopies(t *testing.T) {
	// Arrange: leave a live tail, repeatedly consume and append at the capacity bound.
	var pending streamBuffer
	const budget = 31
	pending.append([]byte(strings.Repeat("a", budget)), budget)
	baseline := pending.copied
	// Act.
	for range 10000 {
		pending.consume(1)
		pending.append([]byte("b"), budget)
	}
	value := pending.prefix(pending.size)
	// Assert: no compaction copies the large retained tail per operation.
	if value != strings.Repeat("b", budget) || pending.copied-baseline != 10000+budget || len(pending.data) > budget {
		t.Fatalf("value=%q copies=%d capacity=%d", value, pending.copied-baseline, len(pending.data))
	}
	pending.clear()
	if pending.data != nil || pending.size != 0 || pending.head != 0 {
		t.Fatal("terminal buffer retains bytes")
	}
}

func workBoundary(t *testing.T, scanner *frameScanner, pending *streamBuffer, maxUnit int, jsonValues, final bool) int {
	t.Helper()
	if !jsonValues {
		return scanner.newline(pending, final)
	}
	n, err := scanner.json(pending, maxUnit, final)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func verifyFramingWork(t *testing.T, name, input string, jsonValues bool, split int) {
	t.Helper()
	// Arrange: work counters exclude validator and transport callbacks.
	var pending streamBuffer
	var scanner frameScanner
	units := 0
	budget := len(input) + 1
	maxUnit := len(input) + 1
	// Act: transport partitions do not restart framing or move consumed tails.
	for offset := 0; offset < len(input); {
		end := min(offset+split, len(input))
		pending.append([]byte(input[offset:end]), budget)
		offset = end
		for pending.size > 0 {
			n := workBoundary(t, &scanner, &pending, maxUnit, jsonValues, false)
			if n == 0 {
				break
			}
			_ = pending.prefix(n)
			pending.consume(n)
			scanner.reset()
			units++
		}
		if len(pending.data) > budget {
			t.Fatal("retained buffer budget exceeded")
		}
	}
	if pending.size > 0 {
		n := workBoundary(t, &scanner, &pending, maxUnit, jsonValues, true)
		if n > 0 {
			_ = pending.prefix(n)
			pending.consume(n)
			scanner.reset()
			units++
		}
	}
	// Assert: each byte is scanned once, plus one next-value lookahead per unit.
	if scanner.visited > uint64(len(input)+units) {
		t.Fatalf("visits=%d bytes=%d units=%d split=%d", scanner.visited, len(input), units, split)
	}
	if pending.copied > uint64(4*len(input)) {
		t.Fatalf("copied=%d bytes=%d split=%d", pending.copied, len(input), split)
	}
	if name == "unfinished" && pending.size != len(input) {
		t.Fatal("unfinished value consumed")
	}
}
