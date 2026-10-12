package lang

import (
	"fmt"
	"strings"
)

// Conjunction is one AND of literals over a model's fields. It is a value:
// copying one copies its arrays, and no stage writes into a set in place.
// Conjunctions are built only by ToDNF and Split; the zero value has no
// model and is not usable.
type Conjunction struct {
	m    *Model
	Has  [MaxFields]bool        // Sets[f] is a constraint (possibly empty = unsatisfiable)
	Sets [MaxFields]IntervalSet // the allowed values per constrained field
	At   [MaxFields]Position    // the first literal on each constrained field, for errors
}

func (c Conjunction) String() string {
	var parts []string
	for f := range c.m.Len() {
		if !c.Has[f] {
			continue
		}
		s := c.m.Field(FieldID(f)).Name
		for _, iv := range c.Sets[f] {
			s += fmt.Sprintf("[%d,%d]", iv.Lo, iv.Hi)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

// TooMany is the conjunction cap's error.
func TooMany(limit int) *Error {
	return Errorf(Position{Line: 1, Column: 1}, "expression expands to more than %d conjunctions; simplify it", limit)
}

// fromLit makes a conjunction of one literal; ok is false when it can never
// hold (an empty set).
func fromLit(m *Model, n Node) (c Conjunction, ok bool) {
	c.m = m
	v, isLit := n.(*Lit)
	if !isLit {
		return c, false
	}
	c.Has[v.F], c.Sets[v.F], c.At[v.F] = true, v.Set, v.Pos
	return c, len(v.Set) > 0
}

// merge ANDs two conjunctions; ok is false when the result is unsatisfiable.
// A field constrained on both sides keeps a's position.
func merge(a, b Conjunction) (Conjunction, bool) {
	c := a
	for f := range a.m.Len() {
		if !b.Has[f] {
			continue
		}
		if !c.Has[f] {
			c.Has[f], c.Sets[f], c.At[f] = true, b.Sets[f], b.At[f]
			continue
		}
		c.Sets[f] = c.Sets[f].Intersect(b.Sets[f])
		if len(c.Sets[f]) == 0 {
			return c, false
		}
	}
	return c, true
}

// ToDNF distributes AND over OR, folding literals into conjunctions as they
// form and dropping unsatisfiable ones, so the intermediate never exceeds
// the output. n must be in negation normal form.
func ToDNF(m *Model, n Node, maxConj int) ([]Conjunction, error) {
	switch v := n.(type) {
	case *And:
		l, err := ToDNF(m, v.L, maxConj)
		if err != nil {
			return nil, err
		}
		r, err := ToDNF(m, v.R, maxConj)
		if err != nil {
			return nil, err
		}
		var out []Conjunction
		for _, a := range l {
			for _, b := range r {
				if c, ok := merge(a, b); ok {
					out = append(out, c)
					if len(out) > maxConj {
						return nil, TooMany(maxConj)
					}
				}
			}
		}
		return out, nil
	case *Or:
		l, err := ToDNF(m, v.L, maxConj)
		if err != nil {
			return nil, err
		}
		r, err := ToDNF(m, v.R, maxConj)
		if err != nil {
			return nil, err
		}
		if len(l)+len(r) > maxConj {
			return nil, TooMany(maxConj)
		}
		return append(l, r...), nil
	}
	if _, isLit := n.(*Lit); !isLit {
		panic(fmt.Sprintf("lang: ToDNF needs negation normal form, got %T", n))
	}
	if c, ok := fromLit(m, n); ok {
		return []Conjunction{c}, nil
	}
	return nil, nil
}

// Split turns a conjunction whose interval set for a Range field has
// several intervals into one conjunction per interval (a clause holds one
// range per field). List and Key fields are not split.
func Split(conjs []Conjunction, maxConj int) ([]Conjunction, error) {
	var out []Conjunction
	for _, c := range conjs {
		parts := []Conjunction{c}
		for f := range c.m.Len() {
			if !c.Has[f] || len(c.Sets[f]) <= 1 || c.m.Field(FieldID(f)).Kind != Range {
				continue
			}
			// refuse the product before building it
			if len(out)+len(parts)*len(c.Sets[f]) > maxConj {
				return nil, TooMany(maxConj)
			}
			next := make([]Conjunction, 0, len(parts)*len(c.Sets[f]))
			for _, p := range parts {
				for _, iv := range c.Sets[f] {
					q := p
					q.Sets[f] = IntervalSet{iv}
					next = append(next, q)
				}
			}
			parts = next
		}
		out = append(out, parts...)
		if len(out) > maxConj {
			return nil, TooMany(maxConj)
		}
	}
	return out, nil
}
