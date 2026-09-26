package filterc

import (
	"fmt"
	"strings"
)

// conjunction is one AND of literals. The zero value is unconstrained.
// pokemon and form are slots like any other field.
type conjunction struct {
	has    [nFields]bool // ranges[f] is a constraint (possibly empty = unsatisfiable)
	ranges [nFields]intervalSet
	formAt *Position // first form literal, for the species check
}

func (c conjunction) String() string {
	var parts []string
	for f := range c.ranges {
		if !c.has[f] {
			continue
		}
		s := field(f).String()
		for _, iv := range c.ranges[f] {
			s += fmt.Sprintf("[%d,%d]", iv.lo, iv.hi)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

func tooMany(limit int) *Error {
	return errorf(Position{Line: 1, Column: 1}, "expression expands to more than %d conjunctions; simplify it", limit)
}

// fromLit makes a conjunction of one literal; ok is false when it can never
// hold (an empty set).
func fromLit(n node) (c conjunction, ok bool) {
	v, isLit := n.(*rangeLit)
	if !isLit {
		return c, false
	}
	c.has[v.f], c.ranges[v.f] = true, v.set
	if v.f == fForm {
		pos := v.pos
		c.formAt = &pos
	}
	return c, len(v.set) > 0
}

// merge ANDs two conjunctions; ok is false when the result is unsatisfiable.
func merge(a, b conjunction) (conjunction, bool) {
	c := a
	for f := range c.ranges {
		if !b.has[f] {
			continue
		}
		if !c.has[f] {
			c.has[f], c.ranges[f] = true, b.ranges[f]
			continue
		}
		c.ranges[f] = c.ranges[f].intersect(b.ranges[f])
		if len(c.ranges[f]) == 0 {
			return c, false
		}
	}
	if c.formAt == nil {
		c.formAt = b.formAt
	}
	return c, true
}

// toDNF distributes AND over OR, folding literals into conjunctions as they
// form and dropping unsatisfiable ones, so the intermediate never exceeds
// the output. n must be in negation normal form.
func toDNF(n node, maxConj int) ([]conjunction, error) {
	switch v := n.(type) {
	case *andNode:
		l, err := toDNF(v.l, maxConj)
		if err != nil {
			return nil, err
		}
		r, err := toDNF(v.r, maxConj)
		if err != nil {
			return nil, err
		}
		var out []conjunction
		for _, a := range l {
			for _, b := range r {
				if c, ok := merge(a, b); ok {
					out = append(out, c)
					if len(out) > maxConj {
						return nil, tooMany(maxConj)
					}
				}
			}
		}
		return out, nil
	case *orNode:
		l, err := toDNF(v.l, maxConj)
		if err != nil {
			return nil, err
		}
		r, err := toDNF(v.r, maxConj)
		if err != nil {
			return nil, err
		}
		if len(l)+len(r) > maxConj {
			return nil, tooMany(maxConj)
		}
		return append(l, r...), nil
	}
	if c, ok := fromLit(n); ok {
		return []conjunction{c}, nil
	}
	return nil, nil
}

// split turns a conjunction whose interval set for a field has several
// intervals into one conjunction per interval (a clause holds one range per
// field). gender is emitted as a list, and pokemon and form become keys,
// so none of them is split.
func split(conjs []conjunction, maxConj int) ([]conjunction, error) {
	var out []conjunction
	for _, c := range conjs {
		parts := []conjunction{c}
		for f := range c.ranges {
			if !c.has[f] || len(c.ranges[f]) <= 1 || field(f) == fGender || field(f).isSpecies() {
				continue
			}
			// refuse the product before building it
			if len(out)+len(parts)*len(c.ranges[f]) > maxConj {
				return nil, tooMany(maxConj)
			}
			next := make([]conjunction, 0, len(parts)*len(c.ranges[f]))
			for _, p := range parts {
				for _, iv := range c.ranges[f] {
					q := p
					q.ranges[f] = intervalSet{iv}
					next = append(next, q)
				}
			}
			parts = next
		}
		out = append(out, parts...)
		if len(out) > maxConj {
			return nil, tooMany(maxConj)
		}
	}
	return out, nil
}

// validate rejects a form constraint without a species constraint whose
// small side is positive: forms belong to a species, and the v3 model has
// no "any species, this form" key. A form set covering the whole domain
// constrains nothing.
func validate(conjs []conjunction) error {
	for _, c := range conjs {
		if !c.has[fForm] || c.ranges[fForm].equal(intervalSet{domains[fForm]}) {
			continue
		}
		if !c.has[fPokemon] || smallSideNegative(fPokemon, c.ranges[fPokemon]) {
			return errorf(*c.formAt, "form needs a pokemon id in the same conjunction (after a negation, write the species explicitly: pokemon != X || (pokemon == X && form != F))")
		}
	}
	return nil
}
