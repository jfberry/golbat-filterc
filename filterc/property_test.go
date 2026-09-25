package filterc

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// Values per field chosen around the domain edges and the sentinels, so
// complements and boundaries are exercised. Species ids are few so that
// clauses overlap and negation bites; rows also use species 5, which no
// expression names by id, and the top of the species domain, which large
// species bounds reach.
var genRangeFields = []struct {
	f    field
	vals []int
}{
	{fIv, []int{-1, 0, 49, 50, 51, 89, 90, 100}},
	{fAtk, []int{-1, 0, 14, 15}},
	{fDef, []int{-1, 0, 14, 15}},
	{fSta, []int{-1, 0, 14, 15}},
	{fLevel, []int{-1, 1, 29, 30, 31, 50}},
	{fCp, []int{-1, 0, 1499, 1500, 1501}},
	{fGender, []int{-1, 0, 1, 2, 3}},
	{fSize, []int{-1, 1, 3, 5}},
	{fLittle, []int{1, 100, 101, 4095, 4096}},
	{fGreat, []int{1, 100, 101, 4095, 4096}},
	{fUltra, []int{1, 100, 101, 4095, 4096}},
}

var genOps = []string{"==", "!=", "<", "<=", ">", ">="}

func pick[T any](rng *rand.Rand, xs []T) T { return xs[rng.Intn(len(xs))] }

func genAtom(rng *rand.Rand) string {
	switch rng.Intn(12) {
	case 0, 1, 2, 3: // range comparison, sometimes with the literal first
		g := pick(rng, genRangeFields)
		op, v := pick(rng, genOps), pick(rng, g.vals)
		if rng.Intn(4) == 0 {
			return fmt.Sprintf("%d %s %s", v, flip(op), g.f)
		}
		return fmt.Sprintf("%s %s %d", g.f, op, v)
	case 4: // list membership
		g := pick(rng, genRangeFields)
		not := ""
		if rng.Intn(2) == 0 {
			not = "not "
		}
		return fmt.Sprintf("%s %sin [%d, %d]", g.f, not, pick(rng, g.vals), pick(rng, g.vals))
	case 5: // range membership
		g := pick(rng, genRangeFields)
		return fmt.Sprintf("%s in %d..%d", g.f, pick(rng, g.vals), pick(rng, g.vals))
	case 6, 7: // species
		s := 1 + rng.Intn(4)
		switch rng.Intn(4) {
		case 0:
			return fmt.Sprintf("pokemon == %d", s)
		case 1:
			return fmt.Sprintf("pokemon != %d", s)
		case 2:
			return fmt.Sprintf("pokemon in [%d, %d]", s, 1+rng.Intn(4))
		}
		return fmt.Sprintf("pokemon not in [%d, %d]", s, 1+rng.Intn(4))
	case 8, 9: // species ranges and comparisons (small sides stay small, so keys stay few)
		switch rng.Intn(6) {
		case 0:
			return fmt.Sprintf("pokemon in %d..%d", rng.Intn(6), rng.Intn(6))
		case 1:
			return fmt.Sprintf("pokemon > %d", rng.Intn(6))
		case 2:
			return fmt.Sprintf("pokemon not in %d..%d", rng.Intn(6), rng.Intn(6))
		case 3:
			return fmt.Sprintf("pokemon %s %d", pick(rng, []string{"<=", ">="}), rng.Intn(6))
		case 4: // bounds at the top of the domain: two large sides can meet in a small set
			hi := domains[fPokemon].hi
			switch rng.Intn(4) {
			case 0:
				return fmt.Sprintf("pokemon > %d", hi-rng.Intn(8))
			case 1:
				return fmt.Sprintf("pokemon < %d", hi-rng.Intn(8))
			case 2:
				return fmt.Sprintf("pokemon in %d..%d", rng.Intn(6), hi)
			}
			return fmt.Sprintf("pokemon not in %d..%d", hi-rng.Intn(8), hi)
		}
		return fmt.Sprintf("pokemon < %d", 1+rng.Intn(6))
	}
	// species with a form, by id, comparison or range
	s := 1 + rng.Intn(4)
	if rng.Intn(4) == 0 {
		return fmt.Sprintf("(pokemon == %d && form in %d..%d)", s, rng.Intn(3), rng.Intn(3))
	}
	return fmt.Sprintf("(pokemon == %d && form %s %d)", s, pick(rng, genOps), rng.Intn(3))
}

func genExpr(rng *rand.Rand, depth int) string {
	if depth >= 3 || rng.Intn(3) == 0 {
		return genAtom(rng)
	}
	switch rng.Intn(5) {
	case 0, 1:
		return "(" + genExpr(rng, depth+1) + " && " + genExpr(rng, depth+1) + ")"
	case 2, 3:
		return "(" + genExpr(rng, depth+1) + " || " + genExpr(rng, depth+1) + ")"
	}
	return "!(" + genExpr(rng, depth+1) + ")"
}

func genRow(rng *rand.Rand) row {
	r := row{pokemonId: 1 + rng.Intn(5), form: rng.Intn(4)}
	if rng.Intn(8) == 0 { // the top of the species domain, named by large bounds
		r.pokemonId = domains[fPokemon].hi - rng.Intn(8)
	}
	r.iv, r.atk, r.def, r.sta = pick(rng, genRangeFields[0].vals), pick(rng, genRangeFields[1].vals), pick(rng, genRangeFields[2].vals), pick(rng, genRangeFields[3].vals)
	r.level, r.cp, r.gender, r.size = pick(rng, genRangeFields[4].vals), pick(rng, genRangeFields[5].vals), pick(rng, genRangeFields[6].vals), pick(rng, genRangeFields[7].vals)
	if rng.Intn(10) >= 3 {
		r.pvp = &pvpRow{pick(rng, genRangeFields[8].vals), pick(rng, genRangeFields[9].vals), pick(rng, genRangeFields[10].vals)}
	}
	return r
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func isBlock(cl Clause) bool {
	return len(cl.Pokemon) == 1 && cl.Iv != nil && *cl.Iv == (MinMax{1, 0}) &&
		cl.AtkIv == nil && cl.DefIv == nil && cl.StaIv == nil && cl.Level == nil && cl.Cp == nil &&
		cl.Gender == nil && cl.Size == nil && cl.Little == nil && cl.Great == nil && cl.Ultra == nil
}

// checkInvariants holds for every compiled output regardless of the input.
func checkInvariants(t *testing.T, src string, c *Compiled) {
	t.Helper()
	again, err := Compile(src)
	if err != nil || mustJSON(again.Filters) != mustJSON(c.Filters) {
		t.Fatalf("%q: not deterministic", src)
	}
	inDomain := func(mm *MinMax, f field) bool {
		return mm == nil || (mm.Min <= mm.Max && mm.Min >= domains[f].lo && mm.Max <= domains[f].hi)
	}
	listed := map[dnfKey]bool{}
	var blocks []dnfKey
	for _, cl := range c.Filters {
		for _, p := range cl.Pokemon {
			if p.Id < 1 || (p.Form != nil && *p.Form < 0) {
				t.Fatalf("%q: bad key %+v", src, p)
			}
			k := dnfKey{p.Id, -1}
			if p.Form != nil {
				k.form = *p.Form
			}
			if isBlock(cl) {
				blocks = append(blocks, k)
			} else {
				listed[k] = true
			}
		}
		if isBlock(cl) {
			continue
		}
		if !inDomain(cl.Iv, fIv) || !inDomain(cl.AtkIv, fAtk) || !inDomain(cl.DefIv, fDef) || !inDomain(cl.StaIv, fSta) ||
			!inDomain(cl.Level, fLevel) || !inDomain(cl.Cp, fCp) || !inDomain(cl.Size, fSize) ||
			!inDomain(cl.Little, fLittle) || !inDomain(cl.Great, fGreat) || !inDomain(cl.Ultra, fUltra) {
			t.Fatalf("%q: range outside its domain in %s", src, mustJSON(cl))
		}
		for _, g := range cl.Gender {
			if g < domains[fGender].lo || g > domains[fGender].hi {
				t.Fatalf("%q: gender %d outside its domain", src, g)
			}
		}
	}
	for _, k := range blocks {
		if listed[k] {
			t.Fatalf("%q: block clause for a key that also has clauses: %+v", src, k)
		}
	}
}

func TestCompileMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(20260925))
	const samples = 3000
	skipped := 0
	for i := 0; i < samples; i++ {
		src := genExpr(rng, 0)
		c, err := Compile(src)
		if err != nil {
			var e *Error
			if errors.As(err, &e) && strings.HasPrefix(e.Msg, "form needs a pokemon id") {
				skipped++ // negation over a species+form atom; a documented limitation
				continue
			}
			t.Fatalf("%q: %v", src, err)
		}
		checkInvariants(t, src, c)
		index := indexClauses(c.Filters)
		for j := 0; j < 40; j++ {
			r := genRow(rng)
			want := reference(t, src, r) == tTrue
			if got := matchV3(index, r); got != want {
				t.Fatalf("expression %q\nrow %+v (pvp %+v)\nmatcher %v, reference %v\nfilters %s", src, r, r.pvp, got, want, mustJSON(c.Filters))
			}
		}
	}
	if skipped > samples/2 {
		t.Fatalf("skipped %d of %d samples; the generator negates too many form atoms", skipped, samples)
	}
	t.Logf("checked %d expressions (%d skipped)", samples-skipped, skipped)
}
