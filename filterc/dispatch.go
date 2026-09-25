package filterc

import (
	"cmp"
	"slices"
)

// key is a scan group: a species and either a specific form or anyForm.
type key struct{ species, form int }

const anyForm = -1

func (k key) pokemonId() PokemonId {
	if k.form == anyForm {
		return PokemonId{Id: k.species}
	}
	f := k.form
	return PokemonId{Id: k.species, Form: &f}
}

// keyPlan is how one conjunction takes part in dispatch. For a species set
// S (or form set F) the small side is S itself when |S| <= |D| - |S|, else
// its complement; keys are drawn from the small side only.
type keyPlan struct {
	c          conjunction
	generic    bool        // applies to the "everything else" group
	species    intervalSet // small side of S; unused when S is unconstrained
	speciesNeg bool        // the small side is the complement of S
	form       intervalSet // small side of F; unused when F is unconstrained
	formNeg    bool        // the small side is the complement of F
}

func planFor(c conjunction) keyPlan {
	p := keyPlan{c: c, generic: true}
	if c.has[fPokemon] {
		p.species, p.speciesNeg = smallSide(fPokemon, c.ranges[fPokemon])
		p.generic = p.speciesNeg
	}
	// a form set covering the whole domain passes validate without a species;
	// its small side is the empty complement, so it constrains nothing
	if c.has[fForm] {
		p.form, p.formNeg = smallSide(fForm, c.ranges[fForm])
	}
	return p
}

// keyCount is the number of keys the plan names, from interval sizes.
// validate guarantees F is unconstrained (or the whole domain) unless the
// species small side is positive.
func (p keyPlan) keyCount() int {
	switch {
	case !p.c.has[fPokemon]:
		return 0
	case p.speciesNeg || !p.c.has[fForm]:
		return p.species.size()
	case p.formNeg:
		return p.species.size() * (p.form.size() + 1)
	}
	return p.species.size() * p.form.size()
}

// ownEntries is the number of keys the plan's own keyed clause lists, from
// interval sizes: (s, f) for each species and each form of a positive form
// small side, or (s, any) for each species when F is unconstrained or its
// small side is negative (applies is false for the excluded forms' exact
// keys). A negative species small side names only keys it excludes.
func (p keyPlan) ownEntries() int {
	switch {
	case !p.c.has[fPokemon] || p.speciesNeg:
		return 0
	case !p.c.has[fForm] || p.formNeg:
		return p.species.size()
	}
	return p.species.size() * p.form.size()
}

// appendKeys enumerates the plan's keys: (s, any) for each species on the
// small side of a negative or form-free S; otherwise (s, f) for each form on
// F's small side, plus (s, any) when that side is negative (those exact
// keys are the forms excluded).
func (p keyPlan) appendKeys(keys []key) []key {
	if !p.c.has[fPokemon] {
		return keys
	}
	byForm := !p.speciesNeg && p.c.has[fForm]
	var forms []int
	if byForm {
		forms = p.form.values()
	}
	for _, s := range p.species.values() {
		if !byForm {
			keys = append(keys, key{s, anyForm})
			continue
		}
		for _, f := range forms {
			keys = append(keys, key{s, f})
		}
		if p.formNeg {
			keys = append(keys, key{s, anyForm})
		}
	}
	return keys
}

// applies reports whether the plan's conjunction holds for a representative
// pokemon of key k. For a species key the representative is a form of the
// species with no exact key of its own; every form outside a negative small
// side has an exact key, so such a representative is inside F exactly when
// F's small side is negative.
func (p keyPlan) applies(k key) bool {
	c := p.c
	if c.has[fPokemon] && !c.ranges[fPokemon].contains(k.species) {
		return false
	}
	if !c.has[fForm] {
		return true
	}
	if k.form == anyForm {
		return p.formNeg
	}
	return c.ranges[fForm].contains(k.form)
}

func blockClause(k key) Clause {
	return Clause{Pokemon: []PokemonId{k.pokemonId()}, Iv: &MinMax{Min: 1, Max: 0}}
}

func clauseFor(c conjunction, ids []PokemonId) Clause {
	cl := Clause{Pokemon: ids}
	for f := range c.ranges {
		if !c.has[f] {
			continue
		}
		if field(f) == fGender {
			cl.Gender = c.ranges[f].values()
			continue
		}
		iv := c.ranges[f][0] // exactly one interval after split
		cl.setRange(field(f), &MinMax{Min: iv.lo, Max: iv.hi})
	}
	return cl
}

func tooManyKeys(limit int) *Error {
	return errorf(Position{Line: 1, Column: 1}, "expression names more than %d species/form keys; simplify it", limit)
}

func tooManyClauses(limit int) *Error {
	return errorf(Position{Line: 1, Column: 1}, "expression compiles to more than %d clauses; simplify it", limit)
}

// distinguishedKeys collects the (species, form) keys the conjunctions
// name, refusing before any key is enumerated when interval sizes already
// prove a cap is exceeded:
//
//   - a conjunction naming more than maxKeys keys has more than maxKeys
//     distinct keys;
//   - every conjunction's own clause lists exactly ownEntries() keys, so
//     the sum of those counts is a lower bound on the emitted pokemon
//     entries (the exclusion keys of a negative species or form small side
//     are left out: they may be shared, or become single block entries).
//
// Otherwise keys are enumerated conjunction by conjunction into a
// deduplicated set, refusing as soon as the distinct count exceeds maxKeys,
// so the work is bounded by conjunctions × maxKeys.
func distinguishedKeys(plans []keyPlan, maxKeys, maxIds int) ([]key, error) {
	listed := 0
	for _, p := range plans {
		n := p.keyCount()
		if n > maxKeys {
			return nil, tooManyKeys(maxKeys)
		}
		if listed += p.ownEntries(); listed > maxIds {
			return nil, tooManyIds(maxIds)
		}
	}
	seen := map[key]struct{}{}
	var keys, buf []key
	for _, p := range plans {
		buf = p.appendKeys(buf[:0])
		for _, k := range buf {
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			if len(seen) > maxKeys {
				return nil, tooManyKeys(maxKeys)
			}
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b key) int {
		return cmp.Or(cmp.Compare(a.species, b.species), cmp.Compare(a.form, b.form))
	})
	return keys, nil
}

func tooManyIds(limit int) *Error {
	return errorf(Position{Line: 1, Column: 1}, "expression emits more than %d pokemon entries; simplify it", limit)
}

// dispatch is dispatchCapped with the default pokemon-entry cap.
func dispatch(conjs []conjunction, maxClauses, maxKeys int) ([]Clause, error) {
	return dispatchCapped(conjs, maxClauses, maxKeys, DefaultMaxIds)
}

// dispatchCapped computes, per (species, form) key the expression names,
// the conjunctions Golbat's probe order (exact, then species, then
// everything else) would select for it, and emits clauses so that no group
// needs to inherit from another at scan time. Every generic conjunction
// lists every key it applies to, so the emitted pokemon entries are capped
// at maxIds as they are appended, block clauses included.
func dispatchCapped(conjs []conjunction, maxClauses, maxKeys, maxIds int) ([]Clause, error) {
	plans := make([]keyPlan, len(conjs))
	for i, c := range conjs {
		plans[i] = planFor(c)
	}
	keys, err := distinguishedKeys(plans, maxKeys, maxIds)
	if err != nil {
		return nil, err
	}

	clauses := []Clause{}
	emit := func(cl Clause) error {
		if len(clauses) >= maxClauses {
			return tooManyClauses(maxClauses)
		}
		clauses = append(clauses, cl)
		return nil
	}
	ids := 0
	addId := func() error {
		if ids >= maxIds {
			return tooManyIds(maxIds)
		}
		ids++
		return nil
	}
	used := make(map[key]bool, len(keys))
	for _, p := range plans {
		if p.generic {
			if err := emit(clauseFor(p.c, nil)); err != nil {
				return nil, err
			}
		}
		var list []PokemonId
		for _, k := range keys {
			if p.applies(k) {
				if err := addId(); err != nil {
					return nil, err
				}
				list = append(list, k.pokemonId())
				used[k] = true
			}
		}
		if len(list) > 0 {
			if err := emit(clauseFor(p.c, list)); err != nil {
				return nil, err
			}
		}
	}
	for _, k := range keys {
		if !used[k] {
			if err := addId(); err != nil {
				return nil, err
			}
			if err := emit(blockClause(k)); err != nil {
				return nil, err
			}
		}
	}
	return clauses, nil
}
