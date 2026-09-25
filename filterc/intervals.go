package filterc

import "slices"

// intervalSet is a union of closed intervals, kept sorted, disjoint and
// with adjacent intervals merged, so two sets with the same members are
// equal as slices.
type intervalSet []interval

// setOf builds a normalised set; empty intervals (lo > hi) are ignored.
func setOf(ivs ...interval) intervalSet {
	var s intervalSet
	for _, iv := range ivs {
		if iv.lo <= iv.hi {
			s = append(s, iv)
		}
	}
	slices.SortFunc(s, func(a, b interval) int { return a.lo - b.lo })
	out := s[:0]
	for _, iv := range s {
		if n := len(out); n > 0 && iv.lo <= out[n-1].hi+1 {
			out[n-1].hi = max(out[n-1].hi, iv.hi)
			continue
		}
		out = append(out, iv)
	}
	return out
}

func (s intervalSet) intersect(t intervalSet) intervalSet {
	var out intervalSet
	for _, a := range s {
		for _, b := range t {
			lo, hi := max(a.lo, b.lo), min(a.hi, b.hi)
			if lo <= hi {
				out = append(out, interval{lo, hi})
			}
		}
	}
	return setOf(out...)
}

// complement returns dom minus s.
func (s intervalSet) complement(dom interval) intervalSet {
	var out intervalSet
	next := dom.lo
	for _, iv := range s.intersect(intervalSet{dom}) {
		if iv.lo > next {
			out = append(out, interval{next, iv.lo - 1})
		}
		next = iv.hi + 1
	}
	if next <= dom.hi {
		out = append(out, interval{next, dom.hi})
	}
	return setOf(out...)
}

// values enumerates the members: gender lists, and species/form id sets
// of at most half their domain.
func (s intervalSet) values() []int {
	var out []int
	for _, iv := range s {
		for v := iv.lo; v <= iv.hi; v++ {
			out = append(out, v)
		}
	}
	return out
}

func (s intervalSet) equal(t intervalSet) bool { return slices.Equal(s, t) }

// idSet is a sorted set of species or form ids.
type idSet []int

func idsOf(vs ...int) idSet {
	s := slices.Clone(vs)
	slices.Sort(s)
	return slices.Compact(s)
}

func (a idSet) contains(v int) bool {
	_, ok := slices.BinarySearch(a, v)
	return ok
}

func (a idSet) intersect(b idSet) idSet {
	var out idSet
	for _, v := range a {
		if b.contains(v) {
			out = append(out, v)
		}
	}
	return out
}

func (a idSet) union(b idSet) idSet { return idsOf(append(slices.Clone(a), b...)...) }

func (a idSet) minus(b idSet) idSet {
	var out idSet
	for _, v := range a {
		if !b.contains(v) {
			out = append(out, v)
		}
	}
	return out
}
