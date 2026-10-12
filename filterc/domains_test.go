package filterc

import (
	"testing"

	"github.com/jfberry/golbat-filterc/internal/lang"
)

func TestFieldNamesRoundTrip(t *testing.T) {
	for name, f := range fieldByName {
		if fieldNames[f] != name {
			t.Errorf("field %d: String() = %q, want %q", f, fieldNames[f], name)
		}
	}
	if len(fieldByName) != int(nFields) {
		t.Errorf("fieldByName has %d entries, want %d", len(fieldByName), nFields)
	}
}

func TestDomains(t *testing.T) {
	want := map[field]interval{
		fPokemon: {Lo: 1, Hi: 32767}, fForm: {Lo: 0, Hi: 32767},
		fIv: {Lo: -1, Hi: 100}, fAtk: {Lo: -1, Hi: 15}, fDef: {Lo: -1, Hi: 15}, fSta: {Lo: -1, Hi: 15},
		fLevel: {Lo: -1, Hi: 127}, fCp: {Lo: -1, Hi: 32767}, fGender: {Lo: -1, Hi: 3}, fSize: {Lo: -1, Hi: 5},
		fLittle: {Lo: 1, Hi: 4096}, fGreat: {Lo: 1, Hi: 4096}, fUltra: {Lo: 1, Hi: 4096},
	}
	for f, d := range want {
		if domains[f] != d {
			t.Errorf("%s domain = %v, want %v", fieldNames[f], domains[f], d)
		}
	}
	if !isSpecies(fPokemon) || !isSpecies(fForm) || isSpecies(fIv) {
		t.Error("isSpecies must be true for pokemon and form only")
	}
}

func TestErrorFormat(t *testing.T) {
	err := &Error{Msg: "unknown field \"x\"", Pos: Position{Line: 1, Column: 3, Offset: 2}}
	if got, want := err.Error(), `1:3: unknown field "x"`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// pokemonModel is built from the table: every field in order, with its
// domain, the scan keys checked as ids, gender a list, PvP NULL-able.
func TestPokemonModelMatchesTable(t *testing.T) {
	if pokemonModel.Len() != int(nFields) {
		t.Fatalf("model has %d fields, want %d", pokemonModel.Len(), nFields)
	}
	for f := range nFields {
		fd := pokemonModel.Field(f)
		if fd.Name != fieldNames[f] || !fd.Domain.Equal(intervalSet{domains[f]}) {
			t.Errorf("field %d = %s %v, want %s %v", f, fd.Name, fd.Domain, fieldNames[f], domains[f])
		}
		if fd.IDs != isSpecies(f) || (fd.Null != nil) != isPvp(f) {
			t.Errorf("%s: IDs %v Null %v", fd.Name, fd.IDs, fd.Null != nil)
		}
		wantKind := lang.Range
		switch {
		case isSpecies(f):
			wantKind = lang.Key
		case f == fGender:
			wantKind = lang.List
		}
		if fd.Kind != wantKind {
			t.Errorf("%s: kind %d, want %d", fd.Name, fd.Kind, wantKind)
		}
	}
}
