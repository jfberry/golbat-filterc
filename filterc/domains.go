// Package filterc compiles boolean filter expressions into Golbat
// pokemon scan (v3) requests. See docs/superpowers/specs for the design.
package filterc

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

// interval is a closed integer interval, lo <= hi.
type interval struct{ lo, hi int }

// domains holds, per field, the values Golbat's lookup can carry. -1 is a
// real value meaning "no encounter data" for the encounter fields; PvP
// ranks have no such member (a pokemon without PvP data has no rank at
// all) and use 4096 for "has PvP data, unranked in this league".
var domains = [nFields]interval{
	fPokemon: {1, 32767},
	fForm:    {0, 32767},
	fIv:      {-1, 100},
	fAtk:     {-1, 15},
	fDef:     {-1, 15},
	fSta:     {-1, 15},
	fLevel:   {-1, 127},
	fCp:      {-1, 32767},
	fGender:  {-1, 3},
	fSize:    {-1, 5},
	fLittle:  {1, 32767},
	fGreat:   {1, 32767},
	fUltra:   {1, 32767},
}
