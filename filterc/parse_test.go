package filterc

import (
	"fmt"
	"strings"
	"testing"

	"github.com/expr-lang/expr/file"
)

func TestParseLowers(t *testing.T) {
	cases := []struct{ src, want string }{
		{"iv >= 90", "iv{[90,100]}"},
		{"90 <= iv", "iv{[90,100]}"},
		{"cp > 1500", "cp{[1501,32767]}"},
		{"iv != 50", "iv{[-1,49],[51,100]}"},
		{"iv > 100", "iv{}"},
		{"iv in -1..100", "iv{[-1,100]}"},
		{"level not in 1..29", "level{[-1,0],[30,127]}"},
		{"gender in [3, 1]", "gender{[1,1],[3,3]}"},
		{"gender in []", "gender{}"},
		{"pokemon in [4, 1, 4]", "pokemon{[1,1],[4,4]}"},
		{"pokemon != 710", "pokemon{[1,709],[711,32767]}"},
		{"pokemon not in [1, 4]", "pokemon{[2,3],[5,32767]}"},
		{"pokemon == 1 and form == 0", "and(pokemon{[1,1]},form{[0,0]})"},
		{"!(iv >= 90) || great <= 100 && cp == 1500", "or(not(iv{[90,100]}),and(great{[1,100]},cp{[1500,1500]}))"},
		{"not (great <= 100)", "not(great{[1,100]})"},
		// `x not in y` lowers to one negative literal; `not (x in y)` keeps the not
		{"not (level in 1..29)", "not(level{[1,29]})"},
		{"great == 4096", "great{[4096,4096]}"},
		// pokemon and form are interval sets over their domains like any
		// other field; nothing is enumerated while lowering
		{"pokemon < 5", "pokemon{[1,4]}"},
		{"pokemon in 1..5", "pokemon{[1,5]}"},
		{"pokemon > 5", "pokemon{[6,32767]}"},
		{"pokemon == 1 && form > 0", "and(pokemon{[1,1]},form{[1,32767]})"},
		{"5 > pokemon", "pokemon{[1,4]}"},
		{"pokemon in 0..3", "pokemon{[1,3]}"},
		{"pokemon not in 1..3", "pokemon{[4,32767]}"},
		{"pokemon >= 1", "pokemon{[1,32767]}"},
		{"pokemon > 32767", "pokemon{}"},
		{"pokemon in 5..1", "pokemon{}"},
		{"form <= 2", "form{[0,2]}"},
		{"pokemon in 1..16383", "pokemon{[1,16383]}"},
		{"pokemon > 20000 && pokemon < 20010", "and(pokemon{[20001,32767]},pokemon{[1,20009]})"},
		{"pokemon in [3, 1, 2, 7]", "pokemon{[1,3],[7,7]}"},
	}
	for _, c := range cases {
		n, err := parse(c.src)
		if err != nil {
			t.Errorf("%q: %v", c.src, err)
			continue
		}
		if got := format(n); got != c.want {
			t.Errorf("%q: lowered to %s, want %s", c.src, got, c.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{"iv >= 90 &&", "1:11: unexpected token EOF"},
		{"x == 1", `1:1: unknown field "x"`},
		{"iv == 1.5", "1:7: expected an integer, got 1.5"},
		{"iv.foo == 1", "1:4: expected a field on one side of =="},
		{"iv >= level", "1:7: expected an integer, got level"},
		{"5 == 6", "1:1: expected a field on one side of =="},
		{"iv", "1:1: expected a comparison"},
		{"not great <= 100", "1:1: not binds tighter than <=; write not (…)"},
		{"pokemon == 0", `1:12: pokemon 0 is not a species id; leave pokemon unconstrained for "everything else"`},
		{"form == -1", "1:9: form -1 is out of range 0..32767"},
		{"iv == 99999999", "1:7: integer 99999999 is out of range"},
		{"iv ?? 1", `1:4: unsupported operator "??"`},
		{"pokemon in [1, 0, 5]", `1:16: pokemon 0 is not a species id; leave pokemon unconstrained for "everything else"`},
		{"form in [3, 40000, 7]", "1:13: form 40000 is out of range 0..32767"},
	}
	for _, c := range cases {
		_, err := parse(c.src)
		if err == nil {
			t.Errorf("%q: expected an error", c.src)
			continue
		}
		if err.Error() != c.want {
			t.Errorf("%q: error %q, want %q", c.src, err.Error(), c.want)
		}
	}
}

func TestParseRejectsLongExpression(t *testing.T) {
	src := make([]byte, maxExpressionBytes+1)
	for i := range src {
		src[i] = ' '
	}
	if _, err := parse(string(src)); err == nil {
		t.Error("expected an error for an over-long expression")
	}
}

// Offsets are bytes while columns stay rune-based: the é before each error
// is two bytes but one column.
func TestParseErrorPositionsNonASCII(t *testing.T) {
	cases := []struct {
		src  string
		want Position
	}{
		{"/* é */ x == 1", Position{Line: 1, Column: 9, Offset: 9}},
		{"/* é */ iv == 1.5", Position{Line: 1, Column: 15, Offset: 15}},
		{"/* é */ iv == 1 )", Position{Line: 1, Column: 17, Offset: 17}},
	}
	for _, c := range cases {
		_, err := parse(c.src)
		e, ok := err.(*Error)
		if !ok {
			t.Errorf("%q: error %v, want *Error", c.src, err)
			continue
		}
		if e.Pos != c.want {
			t.Errorf("%q: position %+v, want %+v (%s)", c.src, e.Pos, c.want, e.Msg)
		}
	}
}

// Positions come from per-parse tables, so a long expression's positions
// cost no re-scan of the source per node: a 64 KB list of 8,000 values
// lowers in well under 16 MiB.
func TestParseLongListAllocations(t *testing.T) {
	var b strings.Builder
	b.WriteString("cp in [")
	for i := 0; i < 8000; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprint(&b, 4*i+1)
	}
	b.WriteString("]")
	src := b.String()
	if len(src) > maxExpressionBytes || len(src) < maxExpressionBytes*3/4 {
		t.Fatalf("source is %d bytes; want close to %d", len(src), maxExpressionBytes)
	}
	var n node
	var err error
	bytes := allocDuring(func() { n, err = parse(src) })
	if err != nil {
		t.Fatal(err)
	}
	if lit, ok := n.(*rangeLit); !ok || len(lit.set) != 8000 {
		t.Fatalf("lowered to %T, want one cp literal of 8000 intervals", n)
	}
	if bytes > 16<<20 {
		t.Errorf("parse allocated %d MiB", bytes>>20)
	}
}

// Every literal records its position: a 64 KB line of 2,000 comparisons
// (Expr's node limit bounds the count; spaces pad it to the byte limit)
// must not re-scan the source per literal.
func TestParseLongChainAllocations(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 2000; i++ {
		if i > 0 {
			b.WriteString(" || ")
		}
		fmt.Fprintf(&b, "cp == %-20d", i)
	}
	src := b.String()
	if len(src) > maxExpressionBytes || len(src) < maxExpressionBytes*3/4 {
		t.Fatalf("source is %d bytes; want close to %d", len(src), maxExpressionBytes)
	}
	var err error
	bytes := allocDuring(func() { _, err = parse(src) })
	if err != nil {
		t.Fatal(err)
	}
	if bytes > 16<<20 {
		t.Errorf("parse allocated %d MiB", bytes>>20)
	}
}

// End to end, including the parse: 1,500 half-domain species ranges reach
// the key cap in well under 16 MiB.
func TestCompileSpeciesRangesAllocations(t *testing.T) {
	src := strings.Repeat("pokemon in 1..16383 && ", 1499) + "pokemon in 1..16383"
	var err error
	bytes := allocDuring(func() { _, err = Compile(src) })
	if err == nil || err.Error() != fmt.Sprintf("1:1: expression names more than %d species/form keys; simplify it", DefaultMaxKeys) {
		t.Errorf("err = %v", err)
	}
	if bytes > 16<<20 {
		t.Errorf("Compile allocated %d MiB before refusing", bytes>>20)
	}
}

// Positions on later lines and after multi-byte runes, from the tables.
func TestParseErrorPositionsMultiline(t *testing.T) {
	cases := []struct {
		src  string
		want Position
	}{
		{"iv == 1 &&\n  x == 2", Position{Line: 2, Column: 3, Offset: 13}},
		{"iv == 1 &&\n\n/* é */ level == 1.5", Position{Line: 3, Column: 18, Offset: 30}},
		{"iv == 1\n&& pokemon == 0", Position{Line: 2, Column: 15, Offset: 22}},
		{"iv == 1 &&\n", Position{Line: 1, Column: 11, Offset: 10}}, // Expr places EOF at the last token
	}
	for _, c := range cases {
		_, err := parse(c.src)
		e, ok := err.(*Error)
		if !ok {
			t.Errorf("%q: error %v, want *Error", c.src, err)
			continue
		}
		if e.Pos != c.want {
			t.Errorf("%q: position %+v, want %+v (%s)", c.src, e.Pos, c.want, e.Msg)
		}
	}
}

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
