package filterc

import (
	"slices"
	"testing"
)

// matchV3 and isPokemonDnfMatch are Golbat's, transcribed from
// decoder/api_pokemon_common.go (the probe chain) and
// decoder/api_pokemon_scan_v3.go (the clause match) at commit 5cd3ed7
// (2026-09-24), with Clause in place of ApiPokemonDnfFilter3 and int in
// place of int8/int16. Re-sync by hand when Golbat's matcher changes.

type dnfKey struct{ pokemon, form int }

func indexClauses(filters []Clause) map[dnfKey][]Clause {
	dnfFilters := make(map[dnfKey][]Clause)
	for _, filter := range filters {
		if len(filter.Pokemon) > 0 {
			for _, keyString := range filter.Pokemon {
				pokemonId := keyString.Id
				if pokemonId == 0 {
					pokemonId = -1
				}
				formId := -1
				if keyString.Form != nil {
					formId = *keyString.Form
				}
				key := dnfKey{pokemon: pokemonId, form: formId}
				dnfFilters[key] = append(dnfFilters[key], filter)
			}
		} else {
			key := dnfKey{pokemon: -1, form: -1}
			dnfFilters[key] = append(dnfFilters[key], filter)
		}
	}
	return dnfFilters
}

func matchV3(dnfFilters map[dnfKey][]Clause, r row) bool {
	filters, found := dnfFilters[dnfKey{pokemon: r.pokemonId, form: r.form}]
	if !found {
		filters, found = dnfFilters[dnfKey{pokemon: r.pokemonId, form: -1}]
		if !found {
			filters, found = dnfFilters[dnfKey{pokemon: -1, form: -1}]
			if !found {
				return false
			}
		}
	}
	for x := 0; x < len(filters); x++ {
		if isPokemonDnfMatch(r, &filters[x]) {
			return true
		}
	}
	return false
}

func isPokemonDnfMatch(pokemonLookup row, filter *Clause) bool {
	if filter.Iv != nil && (pokemonLookup.iv < filter.Iv.Min || pokemonLookup.iv > filter.Iv.Max) ||
		filter.StaIv != nil && (pokemonLookup.sta < filter.StaIv.Min || pokemonLookup.sta > filter.StaIv.Max) ||
		filter.AtkIv != nil && (pokemonLookup.atk < filter.AtkIv.Min || pokemonLookup.atk > filter.AtkIv.Max) ||
		filter.DefIv != nil && (pokemonLookup.def < filter.DefIv.Min || pokemonLookup.def > filter.DefIv.Max) ||
		filter.Level != nil && (pokemonLookup.level < filter.Level.Min || pokemonLookup.level > filter.Level.Max) ||
		filter.Cp != nil && (pokemonLookup.cp < filter.Cp.Min || pokemonLookup.cp > filter.Cp.Max) ||
		(len(filter.Gender) > 0 && !slices.Contains(filter.Gender, pokemonLookup.gender)) ||
		filter.Size != nil && (pokemonLookup.size < filter.Size.Min || pokemonLookup.size > filter.Size.Max) {
		return false
	}
	pvpLookup := pokemonLookup.pvp
	if filter.Little != nil && (pvpLookup == nil || pvpLookup.little < filter.Little.Min || pvpLookup.little > filter.Little.Max) ||
		filter.Great != nil && (pvpLookup == nil || pvpLookup.great < filter.Great.Min || pvpLookup.great > filter.Great.Max) ||
		filter.Ultra != nil && (pvpLookup == nil || pvpLookup.ultra < filter.Ultra.Min || pvpLookup.ultra > filter.Ultra.Max) {
		return false
	}
	return true
}

// The matcher reproduces the recipes in Golbat's api.md "Filter semantics".
func TestMatcherRecipes(t *testing.T) {
	mm := func(lo, hi int) *MinMax { return &MinMax{lo, hi} }
	id := func(i int) PokemonId { return PokemonId{Id: i} }
	maleBulbasaur := row{pokemonId: 1, gender: 1, iv: 100}
	femaleBulbasaur := row{pokemonId: 1, gender: 2, iv: 100}
	malePidgey := row{pokemonId: 16, gender: 1, iv: 100}

	everythingElse := indexClauses([]Clause{
		{Pokemon: []PokemonId{id(1)}, Gender: []int{2}, Iv: mm(100, 100)},
		{Iv: mm(100, 100)},
	})
	if matchV3(everythingElse, maleBulbasaur) || !matchV3(everythingElse, femaleBulbasaur) || !matchV3(everythingElse, malePidgey) {
		t.Error("everything else must not apply to a species with its own clause")
	}
	merged := indexClauses([]Clause{
		{Pokemon: []PokemonId{id(1)}, Gender: []int{2}},
		{Pokemon: []PokemonId{id(1)}, Iv: mm(100, 100)},
		{Iv: mm(100, 100)},
	})
	if !matchV3(merged, maleBulbasaur) {
		t.Error("a shared clause listed under the species must apply there")
	}
	block := indexClauses([]Clause{
		{Size: mm(5, 5)},
		{Pokemon: []PokemonId{id(710)}, Iv: mm(1, 0)},
	})
	if !matchV3(block, row{pokemonId: 1, size: 5}) || matchV3(block, row{pokemonId: 710, size: 5}) {
		t.Error("a clause that can never hold hides the species")
	}
	if matchV3(indexClauses(nil), malePidgey) || !matchV3(indexClauses([]Clause{{}}), malePidgey) {
		t.Error("empty filters match nothing; one empty clause matches everything")
	}
}
