package filterc

import (
	"cmp"
	"maps"
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

// applies reports whether conjunction c holds for a representative pokemon
// of key k. For a species key the representative is a form of the species
// with no exact key of its own; every form named negatively has an exact
// key, so such a representative is outside every negative form set.
func applies(c conjunction, k key) bool {
	if c.hasSpecies && !c.speciesPos.contains(k.species) {
		return false
	}
	if c.speciesNeg.contains(k.species) {
		return false
	}
	if k.form == anyForm {
		return !c.hasForm
	}
	if c.hasForm && !c.formPos.contains(k.form) {
		return false
	}
	return !c.formNeg.contains(k.form)
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

// dispatch computes, per (species, form) key the expression names, the
// conjunctions Golbat's probe order (exact, then species, then everything
// else) would select for it, and emits clauses so that no group needs to
// inherit from another at scan time.
func dispatch(conjs []conjunction, maxClauses int) ([]Clause, error) {
	distinguished := map[key]struct{}{}
	for _, c := range conjs {
		if c.hasSpecies {
			for _, s := range c.speciesPos {
				if c.hasForm {
					for _, f := range c.formPos {
						distinguished[key{s, f}] = struct{}{}
					}
				} else {
					distinguished[key{s, anyForm}] = struct{}{}
				}
				for _, f := range c.formNeg {
					distinguished[key{s, f}] = struct{}{}
				}
			}
		}
		for _, s := range c.speciesNeg {
			distinguished[key{s, anyForm}] = struct{}{}
		}
	}
	keys := slices.SortedFunc(maps.Keys(distinguished), func(a, b key) int {
		return cmp.Or(cmp.Compare(a.species, b.species), cmp.Compare(a.form, b.form))
	})

	clauses := []Clause{}
	used := make(map[key]bool, len(keys))
	for _, c := range conjs {
		if !c.hasSpecies {
			clauses = append(clauses, clauseFor(c, nil))
		}
		var ids []PokemonId
		for _, k := range keys {
			if applies(c, k) {
				ids = append(ids, k.pokemonId())
				used[k] = true
			}
		}
		if len(ids) > 0 {
			clauses = append(clauses, clauseFor(c, ids))
		}
	}
	for _, k := range keys {
		if !used[k] {
			clauses = append(clauses, blockClause(k))
		}
	}
	if len(clauses) > maxClauses {
		return nil, tooMany(maxClauses)
	}
	return clauses, nil
}
