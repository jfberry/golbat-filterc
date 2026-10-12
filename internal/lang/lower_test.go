package lang

import (
	"testing"

	"github.com/expr-lang/expr/file"
)

// The table agrees with Expr's own Bind (line, column) and a rune walk
// (byte offset) at every offset, past the end included.
func TestPosTableMatchesBind(t *testing.T) {
	for _, src := range []string{
		"", "iv == 1", "a\nb", "\n\n", "é\nüx\n", "/* é */ iv == 1\n&& cp > 2\n", "\xff\xfe\n\xff", "a\r\nb\tc",
	} {
		tab := newPosTable(src)
		runes := len([]rune(src))
		for from := 0; from <= runes+2; from++ {
			e := &file.Error{Location: file.Location{From: from}}
			e.Bind(file.NewSource(src))
			off, n := len(src), 0
			for i := range src {
				if n == from {
					off = i
					break
				}
				n++
			}
			want := Position{Line: e.Line, Column: e.Column + 1, Offset: off}
			if got := tab.at(from); got != want {
				t.Errorf("%q at %d: %+v, want %+v", src, from, got, want)
			}
		}
	}
}
