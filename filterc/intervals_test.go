package filterc

import (
	"slices"
	"testing"
)

func TestSetOfNormalises(t *testing.T) {
	got := setOf(interval{5, 9}, interval{1, 3}, interval{4, 4}, interval{20, 10})
	want := intervalSet{{1, 9}} // 4 joins 1..3 and 5..9; 20..10 is empty
	if !got.equal(want) {
		t.Errorf("setOf = %v, want %v", got, want)
	}
	if len(setOf()) != 0 {
		t.Error("setOf() must be empty")
	}
}

func TestIntersect(t *testing.T) {
	a := setOf(interval{-1, 49}, interval{51, 100})
	b := setOf(interval{40, 60})
	if got, want := a.intersect(b), setOf(interval{40, 49}, interval{51, 60}); !got.equal(want) {
		t.Errorf("intersect = %v, want %v", got, want)
	}
	if got := a.intersect(setOf(interval{50, 50})); len(got) != 0 {
		t.Errorf("disjoint intersect = %v, want empty", got)
	}
}

func TestComplement(t *testing.T) {
	dom := interval{-1, 100}
	cases := []struct{ in, want intervalSet }{
		{setOf(interval{90, 100}), setOf(interval{-1, 89})},
		{setOf(interval{50, 50}), setOf(interval{-1, 49}, interval{51, 100})},
		{setOf(), setOf(dom)},
		{setOf(dom), setOf()},
		{setOf(interval{-1, -1}), setOf(interval{0, 100})},
	}
	for _, c := range cases {
		if got := c.in.complement(dom); !got.equal(c.want) {
			t.Errorf("complement(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestValues(t *testing.T) {
	got := setOf(interval{-1, 0}, interval{2, 3}).values()
	if !slices.Equal(got, []int{-1, 0, 2, 3}) {
		t.Errorf("values = %v", got)
	}
}

func TestSizeAndContains(t *testing.T) {
	s := setOf(interval{-1, 0}, interval{5, 9}, interval{20, 20})
	if s.size() != 8 {
		t.Errorf("size = %d, want 8", s.size())
	}
	for v := -3; v <= 22; v++ {
		want := v == -1 || v == 0 || (v >= 5 && v <= 9) || v == 20
		if s.contains(v) != want {
			t.Errorf("contains(%d) = %v", v, !want)
		}
	}
	if setOf().contains(0) || setOf().size() != 0 {
		t.Error("empty set")
	}
}

func TestSmallSide(t *testing.T) {
	cases := []struct {
		f    field
		in   intervalSet
		want intervalSet
		neg  bool
	}{
		{fPokemon, setOf(interval{1, 5}), setOf(interval{1, 5}), false},
		{fPokemon, setOf(interval{6, 32767}), setOf(interval{1, 5}), true},
		{fPokemon, setOf(interval{1, 16383}), setOf(interval{1, 16383}), false},    // 16383 of 32767
		{fPokemon, setOf(interval{1, 16384}), setOf(interval{16385, 32767}), true}, // 16384 of 32767
		{fForm, setOf(interval{0, 16383}), setOf(interval{0, 16383}), false},       // a tie keeps the set
		{fForm, setOf(interval{0, 32767}), setOf(), true},                          // the whole domain
	}
	for _, c := range cases {
		got, neg := smallSide(c.f, c.in)
		if !got.equal(c.want) || neg != c.neg {
			t.Errorf("smallSide(%s, %v) = %v, %v; want %v, %v", c.f, c.in, got, neg, c.want, c.neg)
		}
	}
}
