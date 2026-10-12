package lang

import (
	"slices"
	"testing"
)

func TestSetOfNormalises(t *testing.T) {
	got := SetOf(Interval{5, 9}, Interval{1, 3}, Interval{4, 4}, Interval{20, 10})
	want := IntervalSet{{1, 9}} // 4 joins 1..3 and 5..9; 20..10 is empty
	if !got.Equal(want) {
		t.Errorf("setOf = %v, want %v", got, want)
	}
	if len(SetOf()) != 0 {
		t.Error("setOf() must be empty")
	}
}

func TestIntersect(t *testing.T) {
	a := SetOf(Interval{-1, 49}, Interval{51, 100})
	b := SetOf(Interval{40, 60})
	if got, want := a.Intersect(b), SetOf(Interval{40, 49}, Interval{51, 60}); !got.Equal(want) {
		t.Errorf("intersect = %v, want %v", got, want)
	}
	if got := a.Intersect(SetOf(Interval{50, 50})); len(got) != 0 {
		t.Errorf("disjoint intersect = %v, want empty", got)
	}
}

func TestComplement(t *testing.T) {
	dom := IntervalSet{{-1, 100}}
	cases := []struct{ in, want IntervalSet }{
		{SetOf(Interval{90, 100}), SetOf(Interval{-1, 89})},
		{SetOf(Interval{50, 50}), SetOf(Interval{-1, 49}, Interval{51, 100})},
		{SetOf(), dom},
		{dom, SetOf()},
		{SetOf(Interval{-1, -1}), SetOf(Interval{0, 100})},
	}
	for _, c := range cases {
		if got := c.in.Complement(dom); !got.Equal(c.want) {
			t.Errorf("complement(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestValues(t *testing.T) {
	got := SetOf(Interval{-1, 0}, Interval{2, 3}).Values()
	if !slices.Equal(got, []int{-1, 0, 2, 3}) {
		t.Errorf("values = %v", got)
	}
}

func TestSizeAndContains(t *testing.T) {
	s := SetOf(Interval{-1, 0}, Interval{5, 9}, Interval{20, 20})
	if s.Size() != 8 {
		t.Errorf("size = %d, want 8", s.Size())
	}
	for v := -3; v <= 22; v++ {
		want := v == -1 || v == 0 || (v >= 5 && v <= 9) || v == 20
		if s.Contains(v) != want {
			t.Errorf("contains(%d) = %v", v, !want)
		}
	}
	if SetOf().Contains(0) || SetOf().Size() != 0 {
		t.Error("empty set")
	}
}

// A domain with gaps (phase 3's enum domains): the complement is dom minus
// the set, whether the set lies inside dom, in a gap, or outside it.
func TestComplementGappyDomain(t *testing.T) {
	dom := IntervalSet{{1, 5}, {10, 12}}
	cases := []struct{ in, want IntervalSet }{
		{SetOf(Interval{3, 3}), IntervalSet{{1, 2}, {4, 5}, {10, 12}}},
		{SetOf(), dom},
		{SetOf(Interval{0, 20}), nil},
		{SetOf(Interval{6, 9}), dom},
		{SetOf(Interval{5, 10}), IntervalSet{{1, 4}, {11, 12}}},
	}
	for _, c := range cases {
		if got := c.in.Complement(dom); !got.Equal(c.want) {
			t.Errorf("%v.Complement(%v) = %v, want %v", c.in, dom, got, c.want)
		}
	}
	if got := SetOf(Interval{1, 1}).Complement(nil); len(got) != 0 {
		t.Errorf("complement within an empty domain = %v, want empty", got)
	}
}

func TestLoHi(t *testing.T) {
	s := IntervalSet{{1, 5}, {10, 12}}
	if s.Lo() != 1 || s.Hi() != 12 {
		t.Errorf("Lo, Hi = %d, %d, want 1, 12", s.Lo(), s.Hi())
	}
}
