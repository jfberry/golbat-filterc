// Package filterc compiles boolean filter expressions into Golbat
// pokemon scan (v3) requests. See docs/superpowers/specs for the design.
package filterc

import "github.com/jfberry/golbat-filterc/internal/lang"

// field identifies one of the integer fields an expression may compare.
type field uint8

const (
	fPokemon field = iota
	fForm
	fIv
	fAtk
	fDef
	fSta
	fLevel
	fCp
	fGender
	fSize
	fLittle
	fGreat
	fUltra
	nFields
)

var fieldNames = [nFields]string{
	"pokemon", "form", "iv", "atk", "def", "sta", "level", "cp",
	"gender", "size", "little", "great", "ultra",
}

var fieldByName = func() map[string]field {
	m := make(map[string]field, nFields)
	for f, name := range fieldNames {
		m[name] = field(f)
	}
	return m
}()

func (f field) String() string { return fieldNames[f] }

// isSpecies reports whether f selects a scan group key (pokemon or form)
// rather than a condition inside a clause.
func (f field) isSpecies() bool { return f == fPokemon || f == fForm }

// isPvp reports whether f is a PvP rank, absent for a pokemon without PvP
// data.
func (f field) isPvp() bool { return f == fLittle || f == fGreat || f == fUltra }

// domains holds, per field, the values Golbat's lookup can carry. -1 is a
// real value meaning "no encounter data" for the encounter fields; PvP
// ranks have no such member (a pokemon without PvP data has no rank at
// all) and use 4096 for "has PvP data, unranked in this league".
//
// The encounter tops are what the game produces: iv 100 and per-stat IVs
// 15. Golbat's lookup can hold larger values from legacy rows (lookupIv
// and lookupInt8 in decoder/pokemonRtree.go saturate at 127), which Golbat
// treats as garbage; every emitted range stops at these tops, so a
// compiled filter never returns such rows. Keeping the tops at 100 and 15
// lets iv >= 90 emit max 100, as a person would write it.
//
// PvP ranks are 1..4096: calculatePokemonPvpLookup in
// decoder/pokemonRtree.go starts each league at 4096 and only lowers it,
// and ranks start at 1.
var domains = [nFields]interval{
	fPokemon: {Lo: 1, Hi: 32767},
	fForm:    {Lo: 0, Hi: 32767},
	fIv:      {Lo: -1, Hi: 100},
	fAtk:     {Lo: -1, Hi: 15},
	fDef:     {Lo: -1, Hi: 15},
	fSta:     {Lo: -1, Hi: 15},
	fLevel:   {Lo: -1, Hi: 127},
	fCp:      {Lo: -1, Hi: 32767},
	fGender:  {Lo: -1, Hi: 3},
	fSize:    {Lo: -1, Hi: 5},
	fLittle:  {Lo: 1, Hi: 4096},
	fGreat:   {Lo: 1, Hi: 4096},
	fUltra:   {Lo: 1, Hi: 4096},
}

// pvpNull describes the PvP ranks: absent for a pokemon without PvP data,
// for which Golbat fails every PvP condition.
var pvpNull = &lang.NullGroup{
	Whole:   "holds for every pokemon with PvP data and excludes pokemon without PvP data",
	Negated: "never matches a pokemon without PvP data; a pokemon with no PvP data fails every PvP comparison, negated or not",
	Note:    "note that negated PvP conditions never match pokemon without PvP data",
}
