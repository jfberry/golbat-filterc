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
		{`gender != 2`,
			`[{"gender":[-1,0,1,3]}]`},
		{`atk == 15 && def == 15 && sta == 15 && level >= 30 && cp in 1500..2500`,
			`[{"atk_iv":{"min":15,"max":15},"def_iv":{"min":15,"max":15},"sta_iv":{"min":15,"max":15},"level":{"min":30,"max":127},"cp":{"min":1500,"max":2500}}]`},
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
	_, warnings := compileJSON(t, `iv > 100`)
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
	conjs, err := toDNF(nnf(n), DefaultMaxConjunctions)
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
