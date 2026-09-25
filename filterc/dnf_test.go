package filterc

import (
	"strings"
	"testing"
)

// pipeline runs parse → nnf → DNF → split → validate and renders the
// conjunctions one per line.
func pipeline(t *testing.T, src string, maxConj int) (string, error) {
	t.Helper()
	n, err := parse(src)
	if err != nil {
		t.Fatalf("%q: parse: %v", src, err)
	}
	conjs, err := toDNF(nnf(n), maxConj)
	if err != nil {
		return "", err
	}
	if conjs, err = split(conjs, maxConj); err != nil {
		return "", err
	}
	if err := validate(conjs); err != nil {
		return "", err
	}
	lines := make([]string, len(conjs))
	for i, c := range conjs {
		lines[i] = c.String()
	}
	return strings.Join(lines, "\n"), nil
}

func TestDNF(t *testing.T) {
	cases := []struct{ src, want string }{
		{"iv >= 90 && iv <= 95", "iv[90,95]"},
		{"iv >= 90 && iv < 50", ""},
		{"pokemon == 1 && iv > 100", ""},
		{"!(iv >= 90)", "iv[-1,89]"},
		{"!(iv >= 90 && level <= 30)", "iv[-1,89]\nlevel[31,127]"},
		{"iv != 50", "iv[-1,49]\niv[51,100]"},
		{"gender != 2", "gender[-1,1][3,3]"},
		{"(pokemon == 1 || pokemon == 4) && iv == 100", "pokemon{1} iv[100,100]\npokemon{4} iv[100,100]"},
		{"pokemon in [1, 4] && pokemon != 4", "pokemon{1} !pokemon{4}"},
		{"pokemon == 1 && pokemon != 1", ""},
		{"pokemon == 1 && form == 0 && form != 0", ""},
		{"pokemon == 1 && form != 0 && iv >= 90", "pokemon{1} !form{0} iv[90,100]"},
		{"size == 5 && pokemon != 710", "!pokemon{710} size[5,5]"},
		{"iv == 100 || (pokemon == 1 && gender == 2)", "iv[100,100]\npokemon{1} gender[2,2]"},
		{"pokemon not in [1, 4] || great == 4096", "!pokemon{1,4}\ngreat[4096,4096]"},
	}
	for _, c := range cases {
		got, err := pipeline(t, c.src, 512)
		if err != nil {
			t.Errorf("%q: %v", c.src, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q:\n got %q\nwant %q", c.src, got, c.want)
		}
	}
}

func TestDNFErrors(t *testing.T) {
	cases := []struct {
		src     string
		maxConj int
		want    string
	}{
		{"form == 0", 512, "1:1: form needs a pokemon id in the same conjunction (after a negation, write the species explicitly: pokemon != X || (pokemon == X && form != F))"},
		{"pokemon != 1 && form == 0", 512, "1:17: form needs a pokemon id in the same conjunction (after a negation, write the species explicitly: pokemon != X || (pokemon == X && form != F))"},
		{"!(pokemon == 1 && form == 0)", 512, "1:19: form needs a pokemon id in the same conjunction (after a negation, write the species explicitly: pokemon != X || (pokemon == X && form != F))"},
		{"(iv == 1 || iv == 2) && (level == 1 || level == 2) && (cp == 1 || cp == 2)", 4, "1:1: expression expands to more than 4 clauses; simplify it"},
		{"iv != 1 && level != 1 && cp != 1", 4, "1:1: expression expands to more than 4 clauses; simplify it"},
	}
	for _, c := range cases {
		_, err := pipeline(t, c.src, c.maxConj)
		if err == nil {
			t.Errorf("%q: expected an error", c.src)
			continue
		}
		if err.Error() != c.want {
			t.Errorf("%q: error %q, want %q", c.src, err.Error(), c.want)
		}
	}
}
