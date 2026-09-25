package filterc

import "testing"

func TestFieldNamesRoundTrip(t *testing.T) {
	for name, f := range fieldByName {
		if f.String() != name {
			t.Errorf("field %d: String() = %q, want %q", f, f.String(), name)
		}
	}
	if len(fieldByName) != int(nFields) {
		t.Errorf("fieldByName has %d entries, want %d", len(fieldByName), nFields)
	}
}

func TestDomains(t *testing.T) {
	want := map[field]interval{
		fPokemon: {1, 32767}, fForm: {0, 32767},
		fIv: {-1, 100}, fAtk: {-1, 15}, fDef: {-1, 15}, fSta: {-1, 15},
		fLevel: {-1, 127}, fCp: {-1, 32767}, fGender: {-1, 3}, fSize: {-1, 5},
		fLittle: {1, 32767}, fGreat: {1, 32767}, fUltra: {1, 32767},
	}
	for f, d := range want {
		if domains[f] != d {
			t.Errorf("%s domain = %v, want %v", f, domains[f], d)
		}
	}
	if !fPokemon.isSpecies() || !fForm.isSpecies() || fIv.isSpecies() {
		t.Error("isSpecies must be true for pokemon and form only")
	}
}

func TestErrorFormat(t *testing.T) {
	err := &Error{Msg: "unknown field \"x\"", Pos: Position{Line: 1, Column: 3, Offset: 2}}
	if got, want := err.Error(), `1:3: unknown field "x"`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
