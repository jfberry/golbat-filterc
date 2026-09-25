package filterc

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func compileJSON(t *testing.T, src string) (string, []string) {
	t.Helper()
	c, err := Compile(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	b, err := json.Marshal(c.Filters)
	if err != nil {
		t.Fatal(err)
	}
	return string(b), c.Warnings
}

func TestCompileGolden(t *testing.T) {
	cases := []struct{ src, want string }{
		// spec examples
		{`size == 5 && pokemon != 710`,
			`[{"size":{"min":5,"max":5}},{"pokemon":[{"id":710}],"iv":{"min":1,"max":0}}]`},
		{`iv == 100 || (pokemon == 1 && gender == 2)`,
			`[{"iv":{"min":100,"max":100}},{"pokemon":[{"id":1}],"iv":{"min":100,"max":100}},{"pokemon":[{"id":1}],"gender":[2]}]`},
		{`pokemon == 1 && form != 0 && iv >= 90`,
			`[{"pokemon":[{"id":1}],"iv":{"min":90,"max":100}},{"pokemon":[{"id":1,"form":0}],"iv":{"min":1,"max":0}}]`},
		{`pokemon in [1, 4, 7] && iv == 100`,
			`[{"pokemon":[{"id":1},{"id":4},{"id":7}],"iv":{"min":100,"max":100}}]`},
		{`iv != 50`,
			`[{"iv":{"min":-1,"max":49}},{"iv":{"min":51,"max":100}}]`},
		{`!(iv >= 90)`,
			`[{"iv":{"min":-1,"max":89}}]`},
		// exact key inherits the species-level conjunction; species key precedes exact keys
		{`(pokemon == 1 && form == 0 && iv == 100) || (pokemon == 1 && gender == 2)`,
			`[{"pokemon":[{"id":1,"form":0}],"iv":{"min":100,"max":100}},{"pokemon":[{"id":1},{"id":1,"form":0}],"gender":[2]}]`},
		// negated species with two generic conjunctions: the shared ones are copied, the excluded one is not
		{`(size == 5 && pokemon != 710) || iv == 100`,
			`[{"size":{"min":5,"max":5}},{"iv":{"min":100,"max":100}},{"pokemon":[{"id":710}],"iv":{"min":100,"max":100}}]`},
		{`great <= 100 || little <= 100`,
			`[{"pvp_great":{"min":1,"max":100}},{"pvp_little":{"min":1,"max":100}}]`},
		{`great == 4096`,
			`[{"pvp_great":{"min":4096,"max":4096}}]`},
		// PvP complements stop at the 4096 top
		{`!(great <= 100)`,
			`[{"pvp_great":{"min":101,"max":4096}}]`},
		{`ultra != 4096`,
			`[{"pvp_ultra":{"min":1,"max":4095}}]`},
		{`great > 4096`, `[]`},
		{`gender != 2`,
			`[{"gender":[-1,0,1,3]}]`},
		{`atk == 15 && def == 15 && sta == 15 && level >= 30 && cp in 1500..2500`,
			`[{"atk_iv":{"min":15,"max":15},"def_iv":{"min":15,"max":15},"sta_iv":{"min":15,"max":15},"level":{"min":30,"max":127},"cp":{"min":1500,"max":2500}}]`},
		// literal-first comparisons: a fixed anchor for flip, which the
		// reference and the generator share with the compiler
		{`90 <= iv`,
			`[{"iv":{"min":90,"max":100}}]`},
		{`5 < cp`,
			`[{"cp":{"min":6,"max":32767}}]`},
		// species ranges and comparisons lower to id sets
		{`pokemon in 1..3 && iv == 100`,
			`[{"pokemon":[{"id":1},{"id":2},{"id":3}],"iv":{"min":100,"max":100}}]`},
		{`pokemon > 5 && iv == 100`,
			`[{"iv":{"min":100,"max":100}},{"pokemon":[{"id":1}],"iv":{"min":1,"max":0}},{"pokemon":[{"id":2}],"iv":{"min":1,"max":0}},{"pokemon":[{"id":3}],"iv":{"min":1,"max":0}},{"pokemon":[{"id":4}],"iv":{"min":1,"max":0}},{"pokemon":[{"id":5}],"iv":{"min":1,"max":0}}]`},
		// two large sides intersect to a small positive set: keys, no generic clause
		{`pokemon > 20000 && pokemon < 20010 && iv == 100`,
			`[{"pokemon":[{"id":20001},{"id":20002},{"id":20003},{"id":20004},{"id":20005},{"id":20006},{"id":20007},{"id":20008},{"id":20009}],"iv":{"min":100,"max":100}}]`},
		// the small side of pokemon != 1 && pokemon != 4 is {1, 4}: blocks
		{`pokemon != 1 && pokemon != 4`,
			`[{},{"pokemon":[{"id":1}],"iv":{"min":1,"max":0}},{"pokemon":[{"id":4}],"iv":{"min":1,"max":0}}]`},
		// a form set covering the whole domain constrains nothing, even without a species
		{`!(pokemon == 2 && form < 0) && iv == 1`,
			`[{"iv":{"min":1,"max":1}},{"iv":{"min":1,"max":1}},{"pokemon":[{"id":2}],"iv":{"min":1,"max":1}}]`},
		// unsatisfiable: no clause, and no key (a key would block)
		{`pokemon == 1 && iv > 100`, `[]`},
		{`iv >= 90 && iv < 50`, `[]`},
	}
	for _, c := range cases {
		if got, _ := compileJSON(t, c.src); got != c.want {
			t.Errorf("%q:\n got %s\nwant %s", c.src, got, c.want)
		}
	}
}

func TestCompileWarnsWhenNothingMatches(t *testing.T) {
	_, warnings := compileJSON(t, `iv >= 90 && iv < 50`)
	if len(warnings) != 1 || warnings[0] != "the expression can never hold; the request matches nothing" {
		t.Errorf("warnings = %q", warnings)
	}
	if _, warnings := compileJSON(t, `iv == 100`); len(warnings) != 0 {
		t.Errorf("unexpected warnings %q", warnings)
	}
}

func TestCompileErrorsAndLimits(t *testing.T) {
	if _, err := Compile(`x == 1`); err == nil || err.Error() != `1:1: unknown field "x"` {
		t.Errorf("err = %v", err)
	}
	if _, err := Compile(`iv != 1 && level != 1`, WithMaxConjunctions(2)); err == nil ||
		err.Error() != "1:1: expression expands to more than 2 conjunctions; simplify it" {
		t.Errorf("conjunction limit: err = %v", err)
	}
	// a species range too large to negate hits the key cap
	if _, err := Compile(`pokemon in 1..15000 && iv == 100`); err == nil ||
		err.Error() != "1:1: expression names more than 10000 species/form keys; simplify it" {
		t.Errorf("species range: err = %v", err)
	}
}

// Each cap passes at count == cap and fails at cap+1, with its own message.
func TestCompileCapBoundaries(t *testing.T) {
	// one generic clause plus three block clauses
	const fourClauses = `pokemon != 1 && pokemon != 2 && pokemon != 3`
	if _, err := Compile(fourClauses, WithMaxClauses(4)); err != nil {
		t.Errorf("clauses == cap: %v", err)
	}
	if _, err := Compile(fourClauses, WithMaxClauses(3)); err == nil ||
		err.Error() != "1:1: expression compiles to more than 3 clauses; simplify it" {
		t.Errorf("clauses > cap: err = %v", err)
	}
	// three keys in one clause
	const threeKeys = `pokemon in [1, 2, 3] && iv == 100`
	if _, err := Compile(threeKeys, WithMaxKeys(3)); err != nil {
		t.Errorf("keys == cap: %v", err)
	}
	if _, err := Compile(threeKeys, WithMaxKeys(2)); err == nil ||
		err.Error() != "1:1: expression names more than 2 species/form keys; simplify it" {
		t.Errorf("keys > cap: err = %v", err)
	}
}

// The id cap counts pokemon entries across every clause, blocks included,
// and passes at count == cap.
func TestCompileIdCapBoundaries(t *testing.T) {
	const threeIds = `pokemon in [1, 2, 3] && iv == 100`
	if _, err := Compile(threeIds, WithMaxIds(3)); err != nil {
		t.Errorf("ids == cap: %v", err)
	}
	if _, err := Compile(threeIds, WithMaxIds(2)); err == nil ||
		err.Error() != "1:1: expression emits more than 2 pokemon entries; simplify it" {
		t.Errorf("ids > cap: err = %v", err)
	}
	// three block clauses, one entry each
	const threeBlocks = `pokemon != 1 && pokemon != 2 && pokemon != 3`
	if _, err := Compile(threeBlocks, WithMaxIds(3)); err != nil {
		t.Errorf("block ids == cap: %v", err)
	}
	if _, err := Compile(threeBlocks, WithMaxIds(2)); err == nil ||
		err.Error() != "1:1: expression emits more than 2 pokemon entries; simplify it" {
		t.Errorf("block ids > cap: err = %v", err)
	}
	// keyed and generic entries together: 2 keys in the keyed clause, then
	// the generic conjunction's keyed copy lists both again
	const fourIds = `pokemon in [1, 2] || iv == 100`
	if _, err := Compile(fourIds, WithMaxIds(4)); err != nil {
		t.Errorf("mixed ids == cap: %v", err)
	}
	if _, err := Compile(fourIds, WithMaxIds(3)); err == nil ||
		err.Error() != "1:1: expression emits more than 3 pokemon entries; simplify it" {
		t.Errorf("mixed ids > cap: err = %v", err)
	}
}

// Every generic conjunction repeats every key, so a few hundred bytes can
// ask for millions of entries; the id cap must refuse while clauses are
// built. Here 10,000 keys meet 100 generic conjunctions.
func TestIdCapRefusesBeforeBuilding(t *testing.T) {
	src := "pokemon in 1..10000 || " + oddList("cp", 1, 199)
	n, err := parse(src)
	if err != nil {
		t.Fatal(err)
	}
	conjs, err := toDNF(nnf(n, nil), DefaultMaxConjunctions)
	if err != nil {
		t.Fatal(err)
	}
	if conjs, err = split(conjs, DefaultMaxConjunctions); err != nil {
		t.Fatal(err)
	}
	bytes := allocDuring(func() {
		_, err = dispatchCapped(conjs, DefaultMaxClauses, DefaultMaxKeys, DefaultMaxIds)
	})
	if err == nil || err.Error() != fmt.Sprintf("1:1: expression emits more than %d pokemon entries; simplify it", DefaultMaxIds) {
		t.Errorf("err = %v", err)
	}
	if bytes > 16<<20 {
		t.Errorf("dispatch allocated %d MiB before refusing", bytes>>20)
	}
}

// The key cap counts distinct keys: 101 conjunctions after the split each
// name the same 100 species, which is 100 keys, not 10,100.
func TestKeyCapCountsDistinctKeys(t *testing.T) {
	c, err := Compile("pokemon in 1..100 && " + oddList("cp", 1, 201))
	if err != nil {
		t.Fatal(err)
	}
	keys, entries := map[PokemonId]bool{}, 0
	for _, cl := range c.Filters {
		for _, p := range cl.Pokemon {
			keys[p] = true
			entries++
		}
	}
	if len(c.Filters) != 101 || len(keys) != 100 || entries != 10100 {
		t.Errorf("%d clauses, %d distinct keys, %d entries; want 101, 100, 10100", len(c.Filters), len(keys), entries)
	}
}

// Positive species small sides list their keys in their own clause, so
// their summed counts refuse over the id cap before any key is enumerated.
func TestIdCapPrecheckFromIntervalSizes(t *testing.T) {
	src := "pokemon in 1..50 && cp in [1, 3, 5]" // 3 conjunctions × 50 keys
	n, err := parse(src)
	if err != nil {
		t.Fatal(err)
	}
	conjs, err := toDNF(nnf(n, nil), DefaultMaxConjunctions)
	if err != nil {
		t.Fatal(err)
	}
	if conjs, err = split(conjs, DefaultMaxConjunctions); err != nil {
		t.Fatal(err)
	}
	plans := make([]keyPlan, len(conjs))
	for i, c := range conjs {
		plans[i] = planFor(c)
	}
	if _, err := distinguishedKeys(plans, DefaultMaxKeys, 100); err == nil ||
		err.Error() != "1:1: expression emits more than 100 pokemon entries; simplify it" {
		t.Errorf("precheck: err = %v", err)
	}
	if _, err := Compile(src, WithMaxIds(100)); err == nil ||
		err.Error() != "1:1: expression emits more than 100 pokemon entries; simplify it" {
		t.Errorf("compile: err = %v", err)
	}
}

// A negative form small side's exclusion keys are not in the conjunction's
// own clause, so the id pre-check does not count them: 11 conjunctions each
// name 5,000 × 2 keys by size (110,000), but list 5,000 each in their own
// clause; the 5,000 exclusion keys become one block each.
func TestNegativeFormSmallSidesCompile(t *testing.T) {
	c, err := Compile("pokemon in 1..5000 && form != 0 && " + oddList("cp", 1, 21))
	if err != nil {
		t.Fatal(err)
	}
	entries := 0
	for _, cl := range c.Filters {
		entries += len(cl.Pokemon)
	}
	if len(c.Filters) != 5011 || entries != 60000 {
		t.Errorf("%d clauses, %d entries; want 5011, 60000", len(c.Filters), entries)
	}
}

// Negative small sides are not counted towards the id pre-check: 20
// conjunctions each excluding a different 8,000-id range name 160,000 keys
// by size, but 8,019 distinct ones, emitted mostly as single blocks.
func TestNegativeSmallSidesCompile(t *testing.T) {
	parts := make([]string, 20)
	for k := range parts {
		parts[k] = fmt.Sprintf("(pokemon not in %d..%d && cp == %d)", 1+k, 8000+k, k)
	}
	c, err := Compile(strings.Join(parts, " || "))
	if err != nil {
		t.Fatal(err)
	}
	keys := map[PokemonId]bool{}
	for _, cl := range c.Filters {
		for _, p := range cl.Pokemon {
			keys[p] = true
		}
	}
	if len(keys) != 8019 {
		t.Errorf("%d distinct keys, want 8019", len(keys))
	}
}

// pokemon × form lists name N×M keys in one clause; the key cap must refuse
// them while the key set is being built, not after the buckets are.
func TestKeyCapRefusesBeforeBuilding(t *testing.T) {
	list := func(lo, n int) string {
		strs := make([]string, n)
		for i := range strs {
			strs[i] = fmt.Sprint(lo + i)
		}
		return "[" + strings.Join(strs, ",") + "]"
	}
	src := "pokemon in " + list(1, 2000) + " && form in " + list(0, 2000)
	n, err := parse(src)
	if err != nil {
		t.Fatal(err)
	}
	conjs, err := toDNF(nnf(n, nil), DefaultMaxConjunctions)
	if err != nil {
		t.Fatal(err)
	}
	bytes := allocDuring(func() { _, err = dispatch(conjs, DefaultMaxClauses, DefaultMaxKeys) })
	if err == nil || err.Error() != fmt.Sprintf("1:1: expression names more than %d species/form keys; simplify it", DefaultMaxKeys) {
		t.Errorf("err = %v", err)
	}
	if bytes > 8<<20 {
		t.Errorf("dispatch allocated %d MiB before refusing", bytes>>20)
	}
}

// A species set is counted from its intervals: 1,500 copies of a
// half-domain range must hit the key cap without any id being enumerated
// (the old lowering allocated gigabytes here).
func TestSpeciesRangesRefuseBeforeEnumerating(t *testing.T) {
	src := strings.Repeat("pokemon in 1..16383 && ", 1499) + "pokemon in 1..16383"
	n, err := parse(src)
	if err != nil {
		t.Fatal(err)
	}
	bytes := allocDuring(func() {
		var conjs []conjunction
		if conjs, err = toDNF(nnf(n, nil), DefaultMaxConjunctions); err != nil {
			return
		}
		if conjs, err = split(conjs, DefaultMaxConjunctions); err != nil {
			return
		}
		if err = validate(conjs); err != nil {
			return
		}
		_, err = dispatch(conjs, DefaultMaxClauses, DefaultMaxKeys)
	})
	if err == nil || err.Error() != fmt.Sprintf("1:1: expression names more than %d species/form keys; simplify it", DefaultMaxKeys) {
		t.Errorf("err = %v", err)
	}
	if bytes > 16<<20 {
		t.Errorf("compiling allocated %d MiB before refusing", bytes>>20)
	}
}

func TestRequestShape(t *testing.T) {
	c, err := Compile(`iv == 100`)
	if err != nil {
		t.Fatal(err)
	}
	req := c.Request(Bounds{Min: LatLon{51.4, -0.2}, Max: LatLon{51.6, 0.1}}, 500)
	b, _ := json.Marshal(req)
	want := `{"min":{"lat":51.4,"lon":-0.2},"max":{"lat":51.6,"lon":0.1},"limit":500,"filters":[{"iv":{"min":100,"max":100}}]}`
	if string(b) != want {
		t.Errorf("request = %s\nwant %s", b, want)
	}
	empty, _ := Compile(`iv > 100`)
	if b, _ := json.Marshal(empty.Request(Bounds{}, 0)); string(b) != `{"min":{"lat":0,"lon":0},"max":{"lat":0,"lon":0},"filters":[]}` {
		t.Errorf("empty request = %s", b)
	}
}
