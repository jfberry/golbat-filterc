package filterc

// row is one pokemon as Golbat's lookup cache holds it: -1 for "no
// encounter data" in the encounter fields; pvp nil when the pokemon has no
// PvP data at all, else ranks with 4096 for "unranked in this league".
type row struct {
	pokemonId, form                            int
	iv, atk, def, sta, level, cp, gender, size int
	pvp                                        *pvpRow
}

type pvpRow struct{ little, great, ultra int }

// get returns the field's value; known is false for a PvP field when the
// row has no PvP data.
func (r row) get(f field) (v int, known bool) {
	switch f {
	case fPokemon:
		return r.pokemonId, true
	case fForm:
		return r.form, true
	case fIv:
		return r.iv, true
	case fAtk:
		return r.atk, true
	case fDef:
		return r.def, true
	case fSta:
		return r.sta, true
	case fLevel:
		return r.level, true
	case fCp:
		return r.cp, true
	case fGender:
		return r.gender, true
	case fSize:
		return r.size, true
	}
	if r.pvp == nil {
		return 0, false
	}
	switch f {
	case fLittle:
		return r.pvp.little, true
	case fGreat:
		return r.pvp.great, true
	}
	return r.pvp.ultra, true
}
