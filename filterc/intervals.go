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

// values enumerates the members: gender lists, and dispatch's species and
// form keys once their count has been checked against the key cap.
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

// size is the number of members, computed from the intervals.
func (s intervalSet) size() int {
	n := 0
	for _, iv := range s {
		n += iv.hi - iv.lo + 1
	}
	return n
}

// contains reports whether v is a member, by binary search.
func (s intervalSet) contains(v int) bool {
	i, _ := slices.BinarySearchFunc(s, v, func(iv interval, v int) int {
		switch {
		case iv.hi < v:
			return -1
		case iv.lo > v:
			return 1
		}
		return 0
	})
	return i < len(s) && s[i].lo <= v && v <= s[i].hi
}

// smallSideNegative reports whether the small side of s over f's domain is
// its complement: s holds more than half the domain (a tie keeps s).
func smallSideNegative(f field, s intervalSet) bool {
	d := domains[f]
	n := s.size()
	return n > d.hi-d.lo+1-n
}

// smallSide returns the smaller of s and its complement over f's domain,
// and whether it is the complement.
func smallSide(f field, s intervalSet) (intervalSet, bool) {
	if smallSideNegative(f, s) {
		return s.complement(domains[f]), true
	}
	return s, false
}
