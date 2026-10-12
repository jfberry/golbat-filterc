package filterc

// validate rejects a form constraint without a species constraint whose
// small side is positive: forms belong to a species, and the v3 model has
// no "any species, this form" key. A form set covering the whole domain
// constrains nothing.
func validate(conjs []conjunction) error {
	for _, c := range conjs {
		if !c.Has[fForm] || c.Sets[fForm].Equal(intervalSet{domains[fForm]}) {
			continue
		}
		if !c.Has[fPokemon] || smallSideNegative(fPokemon, c.Sets[fPokemon]) {
			return errorf(c.At[fForm], "form needs a pokemon id in the same conjunction (after a negation, write the species explicitly: pokemon != X || (pokemon == X && form != F))")
		}
	}
	return nil
}
