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

func TestIdSet(t *testing.T) {
	a := idsOf(4, 1, 4, 7)
	if !slices.Equal(a, idSet{1, 4, 7}) {
		t.Errorf("idsOf = %v", a)
	}
	b := idsOf(7, 9, 1)
	if got := a.intersect(b); !slices.Equal(got, idSet{1, 7}) {
		t.Errorf("intersect = %v", got)
	}
	if got := a.union(b); !slices.Equal(got, idSet{1, 4, 7, 9}) {
		t.Errorf("union = %v", got)
	}
	if got := a.minus(b); !slices.Equal(got, idSet{4}) {
		t.Errorf("minus = %v", got)
	}
	if !a.contains(4) || a.contains(5) {
		t.Error("contains")
	}
}
