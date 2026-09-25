package filterc

import (
	"fmt"
	"strings"
)

// conjunction is one AND of literals. The zero value is unconstrained.
type conjunction struct {
	has        [nFields]bool        // ranges[f] is a constraint (possibly empty = unsatisfiable)
	ranges     [nFields]intervalSet // species slots unused
	hasSpecies bool                 // speciesPos is a constraint
	speciesPos idSet
	speciesNeg idSet
	hasForm    bool
	formPos    idSet
	formNeg    idSet
	formAt     *Position // first form literal, for the species check
}

func (c conjunction) String() string {
	var parts []string
	ids := func(s idSet) string {
		strs := make([]string, len(s))
		for i, v := range s {
			strs[i] = fmt.Sprint(v)
		}
		return "{" + strings.Join(strs, ",") + "}"
	}
	if c.hasSpecies {
		parts = append(parts, "pokemon"+ids(c.speciesPos))
	}
	if len(c.speciesNeg) > 0 {
		parts = append(parts, "!pokemon"+ids(c.speciesNeg))
	}
	if c.hasForm {
		parts = append(parts, "form"+ids(c.formPos))
	}
	if len(c.formNeg) > 0 {
		parts = append(parts, "!form"+ids(c.formNeg))
	}
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
	return errorf(Position{Line: 1, Column: 1}, "expression expands to more than %d clauses; simplify it", limit)
}

// fromLit makes a conjunction of one literal; ok is false when it can never
// hold (an empty set).
func fromLit(n node) (c conjunction, ok bool) {
	switch v := n.(type) {
	case *rangeLit:
		c.has[v.f], c.ranges[v.f] = true, v.set
		return c, len(v.set) > 0
	case *idLit:
		pos := v.pos
		switch {
		case v.f == fPokemon && v.neg:
			c.speciesNeg = v.ids
		case v.f == fPokemon:
			c.hasSpecies, c.speciesPos = true, v.ids
		case v.neg:
			c.formNeg, c.formAt = v.ids, &pos
		default:
			c.hasForm, c.formPos, c.formAt = true, v.ids, &pos
		}
		return c, v.neg || len(v.ids) > 0 // a positive empty set can never hold
	}
	return c, false
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
	if b.hasSpecies {
		if c.hasSpecies {
			c.speciesPos = c.speciesPos.intersect(b.speciesPos)
		} else {
			c.hasSpecies, c.speciesPos = true, b.speciesPos
		}
	}
	c.speciesNeg = c.speciesNeg.union(b.speciesNeg)
	if c.hasSpecies {
		c.speciesPos = c.speciesPos.minus(c.speciesNeg)
		if len(c.speciesPos) == 0 {
			return c, false
		}
	}
	if b.hasForm {
		if c.hasForm {
			c.formPos = c.formPos.intersect(b.formPos)
		} else {
			c.hasForm, c.formPos = true, b.formPos
		}
	}
	c.formNeg = c.formNeg.union(b.formNeg)
	if c.hasForm {
		c.formPos = c.formPos.minus(c.formNeg)
		if len(c.formPos) == 0 {
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
// field). gender is emitted as a list, so it is not split.
func split(conjs []conjunction, maxConj int) ([]conjunction, error) {
	var out []conjunction
	for _, c := range conjs {
		parts := []conjunction{c}
		for f := range c.ranges {
			if !c.has[f] || len(c.ranges[f]) <= 1 || field(f) == fGender {
				continue
			}
			var next []conjunction
			for _, p := range parts {
				for _, iv := range c.ranges[f] {
					q := p
					q.ranges[f] = intervalSet{iv}
					next = append(next, q)
				}
			}
			parts = next
			if len(out)+len(parts) > maxConj {
				return nil, tooMany(maxConj)
			}
		}
		out = append(out, parts...)
	}
	return out, nil
}

// validate rejects a form constraint without a positive species: forms
// belong to a species, and the v3 model has no "any species, this form" key.
func validate(conjs []conjunction) error {
	for _, c := range conjs {
		if (c.hasForm || len(c.formNeg) > 0) && !c.hasSpecies {
			return errorf(*c.formAt, "form needs a pokemon id in the same conjunction (after a negation, write the species explicitly: pokemon != X || (pokemon == X && form != F))")
		}
	}
	return nil
}
