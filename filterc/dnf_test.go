package filterc

import (
	"fmt"
	"runtime"
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
	conjs, err := toDNF(nnf(n, nil), maxConj)
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
		{"(pokemon == 1 || pokemon == 4) && iv == 100", "pokemon[1,1] iv[100,100]\npokemon[4,4] iv[100,100]"},
		{"pokemon in [1, 4] && pokemon != 4", "pokemon[1,1]"},
		{"pokemon == 1 && pokemon != 1", ""},
		{"pokemon == 1 && form == 0 && form != 0", ""},
		{"pokemon == 1 && form != 0 && iv >= 90", "pokemon[1,1] form[1,32767] iv[90,100]"},
		{"size == 5 && pokemon != 710", "pokemon[1,709][711,32767] size[5,5]"},
		{"iv == 100 || (pokemon == 1 && gender == 2)", "iv[100,100]\npokemon[1,1] gender[2,2]"},
		{"pokemon not in [1, 4] || great == 4096", "pokemon[2,3][5,32767]\ngreat[4096,4096]"},
		// species sets intersect like any field: two large sides meet in nine ids
		{"pokemon > 20000 && pokemon < 20010", "pokemon[20001,20009]"},
		{"pokemon != 1 && pokemon != 4", "pokemon[2,3][5,32767]"},
		// species and form are never split
		{"pokemon in [1, 3] && form in [0, 2] && iv != 50", "pokemon[1,1][3,3] form[0,0][2,2] iv[-1,49]\npokemon[1,1][3,3] form[0,0][2,2] iv[51,100]"},
		// a small positive side may come from the intersection of two negatives
		{"pokemon > 20000 && pokemon < 20010 && form == 0", "pokemon[20001,20009] form[0,0]"},
		// a form set covering the whole domain constrains nothing
		{"pokemon != 1 && form >= 0", "pokemon[2,32767] form[0,32767]"},
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
		// a species set whose small side is its complement names no species
		{"pokemon > 5 && form == 0", 512, "1:16: form needs a pokemon id in the same conjunction (after a negation, write the species explicitly: pokemon != X || (pokemon == X && form != F))"},
		// the position is the first form literal, even one covering the domain
		{"form >= 0 && form == 1", 512, "1:1: form needs a pokemon id in the same conjunction (after a negation, write the species explicitly: pokemon != X || (pokemon == X && form != F))"},
		{"(iv == 1 || iv == 2) && (level == 1 || level == 2) && (cp == 1 || cp == 2)", 4, "1:1: expression expands to more than 4 conjunctions; simplify it"},
		{"iv != 1 && level != 1 && cp != 1", 4, "1:1: expression expands to more than 4 conjunctions; simplify it"},
		{"(iv != 1 && level != 1) || cp == 5", 4, "1:1: expression expands to more than 4 conjunctions; simplify it"},
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

// allocDuring reports the bytes f allocates.
func allocDuring(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// oddList renders "field in [lo, lo+2, ..., hi]": one interval per member.
func oddList(f string, lo, hi int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s in [", f)
	for v := lo; v <= hi; v += 2 {
		if v > lo {
			b.WriteString(",")
		}
		fmt.Fprint(&b, v)
	}
	b.WriteString("]")
	return b.String()
}

// split must refuse a field's product before building it: the product here
// is 459 × 4500 conjunctions (gigabytes), the cap 512.
func TestSplitChecksCapBeforeBuilding(t *testing.T) {
	src := oddList("iv", -1, 99) + " && " + oddList("atk", -1, 15) + " && " + oddList("cp", 1, 8999)
	n, err := parse(src)
	if err != nil {
		t.Fatal(err)
	}
	conjs, err := toDNF(nnf(n, nil), 512)
	if err != nil {
		t.Fatal(err)
	}
	bytes := allocDuring(func() { _, err = split(conjs, 512) })
	if err == nil || err.Error() != "1:1: expression expands to more than 512 conjunctions; simplify it" {
		t.Errorf("err = %v", err)
	}
	if bytes > 8<<20 {
		t.Errorf("split allocated %d MiB before refusing", bytes>>20)
	}
	// small cap: 5 × 5 parts against a cap of 10
	if _, err := pipeline(t, "iv in [1,3,5,7,9] && atk in [1,3,5,7,9]", 10); err == nil ||
		err.Error() != "1:1: expression expands to more than 10 conjunctions; simplify it" {
		t.Errorf("small cap: err = %v", err)
	}
}
