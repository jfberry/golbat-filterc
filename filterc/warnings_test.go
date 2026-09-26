package filterc

import (
	"encoding/json"
	"slices"
	"testing"
)

const nothing = "the expression can never hold; the request matches nothing"

func TestWarnings(t *testing.T) {
	const hasPvp = "so this condition holds for every pokemon with PvP data and excludes pokemon without PvP data"
	const pvp = " never matches a pokemon without PvP data; a pokemon with no PvP data fails every PvP comparison, negated or not"
	cases := []struct {
		src  string
		want []string
	}{
		// no warnings
		{`iv >= 90`, nil},
		{`great <= 100`, nil},
		{`pokemon > 5 && iv == 100`, nil},
		{`iv in 50..90`, nil},
		{`!(great != 4096)`, nil}, // written negative, then negated: positive

		// W1: a complemented PvP literal, by an enclosing not
		{`!(great <= 100)`, []string{"1:3: negating great <= 100" + pvp}},
		{`!(great <= 100 && iv == 100)`, []string{"1:3: negating great <= 100" + pvp}},
		{`not (little in [1, 2])`, []string{"1:6: negating little in [1, 2]" + pvp}},
		{`!(100 >= ultra)`, []string{"1:3: negating 100 >= ultra" + pvp}},
		// W1: written negative
		{`great != 4096`, []string{"1:1: great != 4096" + pvp}},
		{`great not in 1..10`, []string{"1:1: great not in 1..10" + pvp}},
		{`iv == 100 && ultra not in [1, 2]`, []string{"1:14: ultra not in [1, 2]" + pvp}},
		// a PvP literal over the whole domain means "has PvP data": one
		// warning saying so, and no W1 on top of it; W4 still notes the negation
		{`!(great >= 1)`, []string{
			"1:3: great >= 1: great's range is 1..4096, " + hasPvp,
			nothing + "; note that negated PvP conditions never match pokemon without PvP data",
		}},
		{`great <= 4096`, []string{"1:1: great <= 4096: great's range is 1..4096, " + hasPvp}},
		{`great in 1..4096`, []string{"1:1: great in 1..4096: great's range is 1..4096, " + hasPvp}},
		{`great != 0`, []string{"1:1: great != 0: 0 is outside great's range 1..4096, " + hasPvp}},
		{`great < 5000`, []string{"1:1: great < 5000: 5000 is outside great's range 1..4096, " + hasPvp}},
		{`little not in []`, []string{"1:1: little not in []: the list is empty, " + hasPvp}},

		// W2: comparison value outside the domain
		{`iv < 200`, []string{"1:1: iv < 200: 200 is outside iv's range -1..100, so this condition always holds"}},
		{`iv != 200`, []string{"1:1: iv != 200: 200 is outside iv's range -1..100, so this condition always holds"}},
		{`iv == 200`, []string{"1:1: iv == 200: 200 is outside iv's range -1..100, so this condition can never hold", nothing}},
		{`200 > iv`, []string{"1:1: 200 > iv: 200 is outside iv's range -1..100, so this condition always holds"}},
		{`pokemon >= 0`, []string{"1:1: pokemon >= 0: 0 is outside pokemon's range 1..32767, so this condition always holds"}},
		// W2: value in range, but the comparison is empty or whole
		{`iv > 100`, []string{"1:1: iv > 100: iv's range is -1..100, so this condition can never hold", nothing}},
		{`iv >= -1`, []string{"1:1: iv >= -1: iv's range is -1..100, so this condition always holds"}},
		{`great < 1`, []string{"1:1: great < 1: great's range is 1..4096, so this condition can never hold", nothing}},
		// PvP ranks top out at 4096
		{`great > 4096`, []string{"1:1: great > 4096: great's range is 1..4096, so this condition can never hold", nothing}},
		{`great >= 5000`, []string{"1:1: great >= 5000: 5000 is outside great's range 1..4096, so this condition can never hold", nothing}},
		// W2: ranges
		{`iv in 50..200`, []string{"1:1: iv in 50..200: 200 is outside iv's range -1..100; the range is clipped to 50..100"}},
		{`iv not in 50..200`, []string{"1:1: iv not in 50..200: 200 is outside iv's range -1..100; the range is clipped to 50..100"}},
		{`iv in -5..200`, []string{"1:1: iv in -5..200: -5 is outside iv's range -1..100, so this condition always holds"}},
		{`iv in 101..200`, []string{"1:1: iv in 101..200: 101 is outside iv's range -1..100, so this condition can never hold", nothing}},
		{`iv in -1..100`, []string{"1:1: iv in -1..100: iv's range is -1..100, so this condition always holds"}},
		{`iv not in -1..100`, []string{"1:1: iv not in -1..100: iv's range is -1..100, so this condition can never hold", nothing}},
		// reversed ranges, on any field, even with a bound outside the domain
		{`iv in 5..1`, []string{"1:1: iv in 5..1: the range 5..1 is empty (5 > 1), so this condition can never hold", nothing}},
		{`iv >= 90 || pokemon in 5..1`, []string{"1:13: pokemon in 5..1: the range 5..1 is empty (5 > 1), so this condition can never hold"}},
		{`iv in 200..5`, []string{"1:1: iv in 200..5: the range 200..5 is empty (200 > 5), so this condition can never hold", nothing}},
		{`iv not in 5..1`, []string{"1:1: iv not in 5..1: the range 5..1 is empty (5 > 1), so this condition always holds"}},
		{`pokemon in 0..40000`, []string{"1:1: pokemon in 0..40000: 0 is outside pokemon's range 1..32767, so this condition always holds"}},
		{`pokemon in 5..40000 && iv == 1`, []string{"1:1: pokemon in 5..40000: 40000 is outside pokemon's range 1..32767; the range is clipped to 5..32767"}},

		// W3: list members outside the domain, each at its own position
		{`gender in [1, 7, 9]`, []string{
			"1:15: gender in [1, 7, 9]: 7 is outside gender's range -1..3 and is ignored",
			"1:18: gender in [1, 7, 9]: 9 is outside gender's range -1..3 and is ignored",
		}},
		// list verdicts, at the literal (so before its members' W3)
		{`iv not in [200]`, []string{
			"1:1: iv not in [200]: no listed value is in iv's range -1..100, so this condition always holds",
			"1:12: iv not in [200]: 200 is outside iv's range -1..100 and is ignored",
		}},
		{`gender in [7]`, []string{
			"1:1: gender in [7]: no listed value is in gender's range -1..3, so this condition can never hold",
			"1:12: gender in [7]: 7 is outside gender's range -1..3 and is ignored",
			nothing,
		}},
		{`gender in []`, []string{"1:1: gender in []: the list is empty, so this condition can never hold", nothing}},
		{`gender in [-1, 0, 1, 2, 3]`, []string{"1:1: gender in [-1, 0, 1, 2, 3]: gender's range is -1..3, so this condition always holds"}},

		// W5: a species range reaching below 1
		{`pokemon in 0..3 && iv == 100`, []string{"1:1: pokemon in 0..3: species ids start at 1; 0 is ignored"}},
		{`pokemon in -5..3`, []string{"1:1: pokemon in -5..3: species ids start at 1; -5..0 is ignored"}},
		{`pokemon not in 0..3`, []string{"1:1: pokemon not in 0..3: species ids start at 1; 0 is ignored"}},
		{`pokemon in -5..0`, []string{"1:1: pokemon in -5..0: -5 is outside pokemon's range 1..32767, so this condition can never hold", nothing}},

		// source order across lines and stages
		{"!(great <= 1) ||\n  gender in [9]", []string{
			"1:3: negating great <= 1" + pvp,
			"2:3: gender in [9]: no listed value is in gender's range -1..3, so this condition can never hold",
			"2:14: gender in [9]: 9 is outside gender's range -1..3 and is ignored",
		}},
	}
	for _, c := range cases {
		comp, err := Compile(c.src)
		if err != nil {
			t.Errorf("%q: %v", c.src, err)
			continue
		}
		if !slices.Equal(comp.Warnings, c.want) {
			t.Errorf("%q:\n got %q\nwant %q", c.src, comp.Warnings, c.want)
		}
	}
}

func TestWarningsDedupeInSourceOrder(t *testing.T) {
	w := &warnings{}
	at := func(line, col, off int) Position { return Position{Line: line, Column: col, Offset: off} }
	w.add(at(1, 9, 8), "b")
	w.add(at(1, 1, 0), "a")
	w.add(at(1, 9, 8), "b")
	w.add(at(1, 1, 0), "c")
	if got, want := w.list(), []string{"1:1: a", "1:1: c", "1:9: b"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	var none *warnings
	none.add(at(1, 1, 0), "ignored")
	if none.list() != nil {
		t.Error("a nil collector must discard")
	}
}

// Warnings never change what is emitted: each warned expression compiles to
// the same clauses as its unwarned equivalent.
func TestWarningsDoNotChangeClauses(t *testing.T) {
	pairs := [][2]string{
		{`iv in 50..200 && cp == 1`, `iv in 50..100 && cp == 1`},
		{`iv < 200 && cp == 1`, `iv in -1..100 && cp == 1`},
		{`gender in [1, 7]`, `gender == 1`},
		{`pokemon in 0..3 && iv == 100`, `pokemon in 1..3 && iv == 100`},
		{`!(great <= 100)`, `great > 100`},
		{`great not in 1..10`, `great > 10`},
		{`!(great <= 100 && iv == 100)`, `great > 100 || iv != 100`},
		{`iv > 100 || cp == 1`, `cp == 1`},
	}
	for _, p := range pairs {
		a, err := Compile(p[0])
		if err != nil {
			t.Fatal(err)
		}
		b, err := Compile(p[1])
		if err != nil {
			t.Fatal(err)
		}
		ja, _ := json.Marshal(a.Filters)
		jb, _ := json.Marshal(b.Filters)
		if len(a.Warnings) == 0 || string(ja) != string(jb) {
			t.Errorf("%q: %s (warnings %q)\n%q: %s", p[0], ja, a.Warnings, p[1], jb)
		}
	}
}
