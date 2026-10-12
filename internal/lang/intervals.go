package lang

import "slices"

// Interval is a closed integer interval, Lo <= Hi.
type Interval struct{ Lo, Hi int }

// IntervalSet is a union of closed intervals, kept sorted, disjoint and
// with adjacent intervals merged, so two sets with the same members are
// equal as slices.
type IntervalSet []Interval

// SetOf builds a normalised set; empty intervals (Lo > Hi) are ignored.
func SetOf(ivs ...Interval) IntervalSet {
	var s IntervalSet
	for _, iv := range ivs {
		if iv.Lo <= iv.Hi {
			s = append(s, iv)
		}
	}
	slices.SortFunc(s, func(a, b Interval) int { return a.Lo - b.Lo })
	out := s[:0]
	for _, iv := range s {
		if n := len(out); n > 0 && iv.Lo <= out[n-1].Hi+1 {
			out[n-1].Hi = max(out[n-1].Hi, iv.Hi)
			continue
		}
		out = append(out, iv)
	}
	return out
}

func (s IntervalSet) Intersect(t IntervalSet) IntervalSet {
	var out IntervalSet
	for _, a := range s {
		for _, b := range t {
			lo, hi := max(a.Lo, b.Lo), min(a.Hi, b.Hi)
			if lo <= hi {
				out = append(out, Interval{lo, hi})
			}
		}
	}
	return SetOf(out...)
}

// Complement returns dom minus s. dom may have gaps; for a single-interval
// dom this is the complement within that interval.
func (s IntervalSet) Complement(dom IntervalSet) IntervalSet {
	if len(dom) == 0 {
		return nil
	}
	span := Interval{dom.Lo(), dom.Hi()}
	var out IntervalSet
	next := span.Lo
	for _, iv := range s.Intersect(IntervalSet{span}) {
		if iv.Lo > next {
			out = append(out, Interval{next, iv.Lo - 1})
		}
		next = iv.Hi + 1
	}
	if next <= span.Hi {
		out = append(out, Interval{next, span.Hi})
	}
	if len(dom) == 1 {
		return SetOf(out...)
	}
	return SetOf(out...).Intersect(dom)
}

// Values enumerates the members, for list fields (small sets).
func (s IntervalSet) Values() []int {
	out := make([]int, 0, s.Size())
	for _, iv := range s {
		for v := iv.Lo; v <= iv.Hi; v++ {
			out = append(out, v)
		}
	}
	return out
}

func (s IntervalSet) Equal(t IntervalSet) bool { return slices.Equal(s, t) }

// Size is the number of members, computed from the intervals.
func (s IntervalSet) Size() int {
	n := 0
	for _, iv := range s {
		n += iv.Hi - iv.Lo + 1
	}
	return n
}

// Contains reports whether v is a member, by binary search.
func (s IntervalSet) Contains(v int) bool {
	i, _ := slices.BinarySearchFunc(s, v, func(iv Interval, v int) int {
		switch {
		case iv.Hi < v:
			return -1
		case iv.Lo > v:
			return 1
		}
		return 0
	})
	return i < len(s) && s[i].Lo <= v && v <= s[i].Hi
}

// Lo is the smallest member; s must not be empty.
func (s IntervalSet) Lo() int { return s[0].Lo }

// Hi is the largest member; s must not be empty.
func (s IntervalSet) Hi() int { return s[len(s)-1].Hi }
