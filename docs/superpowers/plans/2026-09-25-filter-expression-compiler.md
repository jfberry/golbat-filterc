# Filter Expression Compiler Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go library, CLI and small server that compile a boolean filter expression into a Golbat `POST /api/pokemon/v3/scan` request, proven against a reference evaluator, and able to run the scan against a configured Golbat.

**Architecture:** Expr's parser produces an AST; a whitelist walk lowers it to a tiny boolean tree of literals over 13 integer fields; the tree goes through negation normal form, disjunctive normal form (folding literals into conjunctions as they form), splitting of multi-interval fields, then a dispatch stage that computes, at compile time, the per-`(species, form)` clause groups Golbat's probe order would select — so the emitted request needs no server-side merging. A three-valued reference evaluator over the Expr AST and a verbatim copy of Golbat's v3 matcher give a property test that the compiler is correct.

**Tech Stack:** Go 1.24+, `github.com/expr-lang/expr` v1.17.8 (parser only), `github.com/BurntSushi/toml` v1.6.0 (CLI config), standard library `net/http` and `flag`.

**Spec:** `docs/superpowers/specs/2026-09-25-filter-expression-compiler-design.md`

## Global Constraints

- Module path `github.com/jfberry/golbat-filterc`; `go 1.24` in `go.mod`.
- Runtime dependencies: `expr-lang/expr` and `BurntSushi/toml` only. Expr's VM, checker and compiler packages are never imported; the whitelist walk is the type check for this subset.
- Output is the v3 wire shape exactly: `pokemon` (`id`, optional `form`), `iv`, `atk_iv`, `def_iv`, `sta_iv`, `level`, `cp`, `gender` (list), `size`, `pvp_little`, `pvp_great`, `pvp_ultra`; every range carries both `min` and `max`.
- Domains (spec table): `pokemon` 1..32767, `form` 0..32767, `iv` −1..100, `atk/def/sta` −1..15, `level` −1..127, `cp` −1..32767, `size` −1..5, `gender` −1..3, `little/great/ultra` 1..32767.
- Limits: 512 conjunctions, 10,000 clauses, 64 KiB expression, all overridable by option; the server caps request bodies at 64 KiB.
- The block clause is exactly `{"pokemon": [key], "iv": {"min": 1, "max": 0}}`.
- Errors carry a position: 1-based line and column, plus byte offset.
- Every commit passes `gofmt -l` (empty) and `go vet ./...`.
- License: Unlicense (already in the repo).

## Review Focus

Inputs the spec implies but no headline example exercises; each has a test in the task that owns the code:

1. `not great <= 100` — Expr parses it as `(not great) <= 100`. A person expects either a clear error or the obvious meaning; Task 3 rejects `not` over a non-boolean with a message that says to write `not (great <= 100)`.
2. `!(pokemon == 1 && form == 0)` — De Morgan yields a `form != 0` disjunct with no species, which the model cannot express. Task 4 reports the form literal's position and tells the user to write `pokemon != 1 || (pokemon == 1 && form != 0)`.
3. A form named only negatively (`pokemon == 1 && form != 0`) — the exact key `{1, 0}` must exist and block, or form-0 Bulbasaur would fall through to the species group. Task 5 golden test.
4. An unranked league (`great == 4096`) and a pokemon with no PvP data — both must behave exactly as Golbat's matcher: Task 6's row generator produces both, and the vendored matcher decides.
5. A species key with an *empty* string of conditions after intersection (`pokemon == 1 && iv > 100`) — the conjunction is unsatisfiable and must vanish, and if nothing else names Bulbasaur, no key for it may be emitted (else it would block). Task 4 unit test and Task 5 golden test.

---

### Task 1: Module scaffold, fields and domains, error type

**Files:**
- Create: `go.mod`, `filterc/domains.go`, `filterc/errors.go`
- Test: `filterc/domains_test.go`

**Interfaces:**
- Produces: `type field uint8` with constants `fPokemon, fForm, fIv, fAtk, fDef, fSta, fLevel, fCp, fGender, fSize, fLittle, fGreat, fUltra, nFields`; `fieldByName map[string]field`; `func (f field) String() string`; `func (f field) isSpecies() bool`; `type interval struct{ lo, hi int }`; `var domains [nFields]interval`; `type Position struct{ Line, Column, Offset int }`; `type Error struct{ Msg string; Pos Position }` implementing `error`.

- [ ] **Step 1: Initialise the module and pin dependencies**

```bash
cd /Users/james/GolandProjects/golbat-filterc
go mod init github.com/jfberry/golbat-filterc
go get github.com/expr-lang/expr@v1.17.8 github.com/BurntSushi/toml@v1.6.0
```

Expected: `go.mod` with `go 1.24` (edit the `go` line down to `1.24` if the toolchain wrote a higher one) and both requires.

- [ ] **Step 2: Write the failing test**

`filterc/domains_test.go`:

```go
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./filterc/ -run 'TestFieldNames|TestDomains|TestErrorFormat'`
Expected: FAIL — `undefined: fieldByName` (compile error).

- [ ] **Step 4: Write the implementation**

`filterc/domains.go`:

```go
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
```

`filterc/errors.go`:

```go
package filterc

import "fmt"

// Position locates an error in the expression source. Line and Column are
// 1-based; Offset is the byte offset.
type Position struct {
	Line, Column, Offset int
}

// Error is a compile error at a position in the expression.
type Error struct {
	Msg string
	Pos Position
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Column, e.Msg)
}

func errorf(pos Position, format string, args ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, args...), Pos: pos}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `gofmt -l . && go vet ./... && go test ./filterc/`
Expected: PASS, no gofmt output.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum filterc/
git commit -m "feat: module scaffold, fields, domains and error type"
```

---

### Task 2: Interval sets and id sets

**Files:**
- Create: `filterc/intervals.go`
- Test: `filterc/intervals_test.go`

**Interfaces:**
- Consumes: `interval`, `domains` from Task 1.
- Produces: `type intervalSet []interval` (sorted, disjoint, merged); `func setOf(ivs ...interval) intervalSet` (normalises); `func (s intervalSet) intersect(t intervalSet) intervalSet`; `func (s intervalSet) complement(dom interval) intervalSet`; `func (s intervalSet) values() []int`; `func (s intervalSet) equal(t intervalSet) bool`; `type idSet []int` (sorted, unique); `func idsOf(vs ...int) idSet`; `func (a idSet) intersect(b idSet) idSet`; `func (a idSet) union(b idSet) idSet`; `func (a idSet) minus(b idSet) idSet`; `func (a idSet) contains(v int) bool`.

- [ ] **Step 1: Write the failing test**

`filterc/intervals_test.go`:

```go
package filterc

import (
	"slices"
	"testing"
)

func TestSetOfNormalises(t *testing.T) {
	got := setOf(interval{5, 9}, interval{1, 3}, interval{4, 4}, interval{20, 10})
	want := intervalSet{{1, 9}} // 4 joins 1..3 and 5..9; 20..10 is empty
	if !got.equal(want) {
		t.Errorf("setOf = %v, want %v", got, want)
	}
	if len(setOf()) != 0 {
		t.Error("setOf() must be empty")
	}
}

func TestIntersect(t *testing.T) {
	a := setOf(interval{-1, 49}, interval{51, 100})
	b := setOf(interval{40, 60})
	if got, want := a.intersect(b), setOf(interval{40, 49}, interval{51, 60}); !got.equal(want) {
		t.Errorf("intersect = %v, want %v", got, want)
	}
	if got := a.intersect(setOf(interval{50, 50})); len(got) != 0 {
		t.Errorf("disjoint intersect = %v, want empty", got)
	}
}

func TestComplement(t *testing.T) {
	dom := interval{-1, 100}
	cases := []struct{ in, want intervalSet }{
		{setOf(interval{90, 100}), setOf(interval{-1, 89})},
		{setOf(interval{50, 50}), setOf(interval{-1, 49}, interval{51, 100})},
		{setOf(), setOf(dom)},
		{setOf(dom), setOf()},
		{setOf(interval{-1, -1}), setOf(interval{0, 100})},
	}
	for _, c := range cases {
		if got := c.in.complement(dom); !got.equal(c.want) {
			t.Errorf("complement(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestValues(t *testing.T) {
	got := setOf(interval{-1, 0}, interval{2, 3}).values()
	if !slices.Equal(got, []int{-1, 0, 2, 3}) {
		t.Errorf("values = %v", got)
	}
}

func TestIdSet(t *testing.T) {
	a := idsOf(4, 1, 4, 7)
	if !slices.Equal(a, idSet{1, 4, 7}) {
		t.Errorf("idsOf = %v", a)
	}
	b := idsOf(7, 9, 1)
	if got := a.intersect(b); !slices.Equal(got, idSet{1, 7}) {
		t.Errorf("intersect = %v", got)
	}
	if got := a.union(b); !slices.Equal(got, idSet{1, 4, 7, 9}) {
		t.Errorf("union = %v", got)
	}
	if got := a.minus(b); !slices.Equal(got, idSet{4}) {
		t.Errorf("minus = %v", got)
	}
	if !a.contains(4) || a.contains(5) {
		t.Error("contains")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./filterc/ -run 'TestSetOf|TestIntersect|TestComplement|TestValues|TestIdSet'`
Expected: FAIL — `undefined: setOf`.

- [ ] **Step 3: Write the implementation**

`filterc/intervals.go`:

```go
package filterc

import "slices"

// intervalSet is a union of closed intervals, kept sorted, disjoint and
// with adjacent intervals merged, so two sets with the same members are
// equal as slices.
type intervalSet []interval

// setOf builds a normalised set; empty intervals (lo > hi) are ignored.
func setOf(ivs ...interval) intervalSet {
	var s intervalSet
	for _, iv := range ivs {
		if iv.lo <= iv.hi {
			s = append(s, iv)
		}
	}
	slices.SortFunc(s, func(a, b interval) int { return a.lo - b.lo })
	out := s[:0]
	for _, iv := range s {
		if n := len(out); n > 0 && iv.lo <= out[n-1].hi+1 {
			out[n-1].hi = max(out[n-1].hi, iv.hi)
			continue
		}
		out = append(out, iv)
	}
	return out
}

func (s intervalSet) intersect(t intervalSet) intervalSet {
	var out intervalSet
	for _, a := range s {
		for _, b := range t {
			lo, hi := max(a.lo, b.lo), min(a.hi, b.hi)
			if lo <= hi {
				out = append(out, interval{lo, hi})
			}
		}
	}
	return setOf(out...)
}

// complement returns dom minus s.
func (s intervalSet) complement(dom interval) intervalSet {
	var out intervalSet
	next := dom.lo
	for _, iv := range s.intersect(intervalSet{dom}) {
		if iv.lo > next {
			out = append(out, interval{next, iv.lo - 1})
		}
		next = iv.hi + 1
	}
	if next <= dom.hi {
		out = append(out, interval{next, dom.hi})
	}
	return setOf(out...)
}

// values enumerates the members; only used for small domains (gender).
func (s intervalSet) values() []int {
	var out []int
	for _, iv := range s {
		for v := iv.lo; v <= iv.hi; v++ {
			out = append(out, v)
		}
	}
	return out
}

func (s intervalSet) equal(t intervalSet) bool { return slices.Equal(s, t) }

// idSet is a sorted set of species or form ids.
type idSet []int

func idsOf(vs ...int) idSet {
	s := slices.Clone(vs)
	slices.Sort(s)
	return slices.Compact(s)
}

func (a idSet) contains(v int) bool {
	_, ok := slices.BinarySearch(a, v)
	return ok
}

func (a idSet) intersect(b idSet) idSet {
	var out idSet
	for _, v := range a {
		if b.contains(v) {
			out = append(out, v)
		}
	}
	return out
}

func (a idSet) union(b idSet) idSet { return idsOf(append(slices.Clone(a), b...)...) }

func (a idSet) minus(b idSet) idSet {
	var out idSet
	for _, v := range a {
		if !b.contains(v) {
			out = append(out, v)
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `gofmt -l . && go vet ./... && go test ./filterc/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add filterc/intervals.go filterc/intervals_test.go
git commit -m "feat: interval sets and id sets"
```

---

### Task 3: Parse with Expr and lower to the boolean tree

**Files:**
- Create: `filterc/ast.go`, `filterc/parse.go`
- Test: `filterc/parse_test.go`

**Interfaces:**
- Consumes: Task 1 (`field`, `domains`, `Error`, `errorf`), Task 2 (`intervalSet`, `setOf`, `idSet`, `idsOf`).
- Produces: `type node interface{}` with concrete `*andNode{l, r node}`, `*orNode{l, r node}`, `*notNode{x node}`, `*rangeLit{f field; set intervalSet; pos Position}` (every non-species field, gender included), `*idLit{f field; ids idSet; neg bool; pos Position}` (pokemon or form); `func parse(src string) (node, error)`; `func format(n node) string` (debug form used by tests); `const maxExpressionBytes = 64 << 10`.

Facts about Expr v1.17.8 this task relies on (verified): operators keep their spelling (`and`/`&&`, `or`/`||`, `not`/`!`); `x not in y` parses as `UnaryNode{"not", BinaryNode{"in", x, y}}`; `a..b` is `BinaryNode{"..", a, b}`; `-1` is `UnaryNode{"-", IntegerNode{1}}`; `not great <= 100` parses as `BinaryNode{"<=", UnaryNode{"not", great}, 100}`; a node's `Location().From` is the byte offset of its operator token; `(&file.Error{Location: loc}).Bind(tree.Source)` fills a 1-based `Line` and a 0-based `Column`; parse errors are `*file.Error` with `Line`, `Column` (0-based), `From`, `Message`.

- [ ] **Step 1: Write the failing test**

`filterc/parse_test.go`:

```go
package filterc

import "testing"

func TestParseLowers(t *testing.T) {
	cases := []struct{ src, want string }{
		{"iv >= 90", "iv{[90,100]}"},
		{"90 <= iv", "iv{[90,100]}"},
		{"cp > 1500", "cp{[1501,32767]}"},
		{"iv != 50", "iv{[-1,49],[51,100]}"},
		{"iv > 100", "iv{}"},
		{"iv in -1..100", "iv{[-1,100]}"},
		{"level not in 1..29", "not(level{[1,29]})"},
		{"gender in [3, 1]", "gender{[1,1],[3,3]}"},
		{"gender in []", "gender{}"},
		{"pokemon in [4, 1, 4]", "pokemon{1,4}"},
		{"pokemon != 710", "!pokemon{710}"},
		{"pokemon not in [1, 4]", "not(pokemon{1,4})"},
		{"pokemon == 1 and form == 0", "and(pokemon{1},form{0})"},
		{"!(iv >= 90) || great <= 100 && cp == 1500", "or(not(iv{[90,100]}),and(great{[1,100]},cp{[1500,1500]}))"},
		{"not (great <= 100)", "not(great{[1,100]})"},
		{"great == 4096", "great{[4096,4096]}"},
	}
	for _, c := range cases {
		n, err := parse(c.src)
		if err != nil {
			t.Errorf("%q: %v", c.src, err)
			continue
		}
		if got := format(n); got != c.want {
			t.Errorf("%q: lowered to %s, want %s", c.src, got, c.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{"iv >= 90 &&", "1:11: unexpected token EOF"},
		{"x == 1", `1:1: unknown field "x"`},
		{"iv == 1.5", "1:7: expected an integer, got 1.5"},
		{"iv.foo == 1", "1:4: expected a field on one side of =="},
		{"iv >= level", "1:7: expected an integer, got level"},
		{"5 == 6", "1:1: expected a field on one side of =="},
		{"iv", "1:1: expected a comparison"},
		{"not great <= 100", "1:1: not binds tighter than <=; write not (…)"},
		{"pokemon == 0", `1:12: pokemon 0 is not a species id; leave pokemon unconstrained for "everything else"`},
		{"pokemon < 5", "1:1: pokemon supports ==, !=, in and not in only"},
		{"pokemon in 1..5", "1:1: pokemon supports a list, not a range"},
		{"form == -1", "1:9: form -1 is out of range 0..32767"},
		{"iv == 99999999", "1:7: integer 99999999 is out of range"},
		{"iv ?? 1", `1:4: unsupported operator "??"`},
	}
	for _, c := range cases {
		_, err := parse(c.src)
		if err == nil {
			t.Errorf("%q: expected an error", c.src)
			continue
		}
		if err.Error() != c.want {
			t.Errorf("%q: error %q, want %q", c.src, err.Error(), c.want)
		}
	}
}

func TestParseRejectsLongExpression(t *testing.T) {
	src := make([]byte, maxExpressionBytes+1)
	for i := range src {
		src[i] = ' '
	}
	if _, err := parse(string(src)); err == nil {
		t.Error("expected an error for an over-long expression")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./filterc/ -run 'TestParse'`
Expected: FAIL — `undefined: parse`.

- [ ] **Step 3: Write the boolean tree**

`filterc/ast.go`:

```go
package filterc

import (
	"fmt"
	"strings"
)

// node is the boolean tree an expression lowers to: and/or/not over
// literals. The only literal kinds are a range field with an interval set
// and a species/form id set with a negated flag.
type node interface{ isNode() }

type andNode struct{ l, r node }
type orNode struct{ l, r node }
type notNode struct{ x node }

// rangeLit constrains a non-species field to a set of values.
type rangeLit struct {
	f   field
	set intervalSet
	pos Position
}

// idLit constrains pokemon or form to an id set (or its complement).
type idLit struct {
	f   field
	ids idSet
	neg bool
	pos Position
}

func (*andNode) isNode()  {}
func (*orNode) isNode()   {}
func (*notNode) isNode()  {}
func (*rangeLit) isNode() {}
func (*idLit) isNode()    {}

// format renders a node for tests and debugging: and(a,b), or(a,b),
// not(a), iv{[90,100]}, pokemon{1,4}, !pokemon{710}.
func format(n node) string {
	switch v := n.(type) {
	case *andNode:
		return "and(" + format(v.l) + "," + format(v.r) + ")"
	case *orNode:
		return "or(" + format(v.l) + "," + format(v.r) + ")"
	case *notNode:
		return "not(" + format(v.x) + ")"
	case *rangeLit:
		parts := make([]string, len(v.set))
		for i, iv := range v.set {
			parts[i] = fmt.Sprintf("[%d,%d]", iv.lo, iv.hi)
		}
		return v.f.String() + "{" + strings.Join(parts, ",") + "}"
	case *idLit:
		parts := make([]string, len(v.ids))
		for i, id := range v.ids {
			parts[i] = fmt.Sprint(id)
		}
		neg := ""
		if v.neg {
			neg = "!"
		}
		return neg + v.f.String() + "{" + strings.Join(parts, ",") + "}"
	}
	return fmt.Sprintf("<%T>", n)
}
```

- [ ] **Step 4: Write the parser and lowering**

`filterc/parse.go`:

```go
package filterc

import (
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/file"
	"github.com/expr-lang/expr/parser"
)

const (
	maxExpressionBytes = 64 << 10
	maxLiteral         = 1 << 20 // keeps v±1 arithmetic far from overflow
)

// parse turns expression source into the boolean tree, using Expr's parser
// and accepting only the subset the compiler understands. The whitelist walk
// is the type check: every accepted node is a comparison between a known
// field and an integer, or and/or/not over such comparisons.
func parse(src string) (node, error) {
	if len(src) > maxExpressionBytes {
		return nil, errorf(Position{Line: 1, Column: 1}, "expression longer than %d bytes", maxExpressionBytes)
	}
	tree, err := parser.Parse(src)
	if err != nil {
		if fe, ok := err.(*file.Error); ok {
			return nil, &Error{Msg: fe.Message, Pos: Position{Line: fe.Line, Column: fe.Column + 1, Offset: fe.From}}
		}
		return nil, &Error{Msg: err.Error(), Pos: Position{Line: 1, Column: 1}}
	}
	l := lowerer{src: tree.Source}
	return l.boolean(tree.Node)
}

type lowerer struct{ src file.Source }

func (l *lowerer) pos(n ast.Node) Position {
	e := &file.Error{Location: n.Location()}
	e.Bind(l.src)
	return Position{Line: e.Line, Column: e.Column + 1, Offset: n.Location().From}
}

func (l *lowerer) boolean(n ast.Node) (node, error) {
	switch v := n.(type) {
	case *ast.BinaryNode:
		switch v.Operator {
		case "and", "&&":
			return l.pair(v, func(a, b node) node { return &andNode{a, b} })
		case "or", "||":
			return l.pair(v, func(a, b node) node { return &orNode{a, b} })
		case "==", "!=", "<", "<=", ">", ">=":
			return l.comparison(v)
		case "in":
			return l.membership(v)
		}
		return nil, errorf(l.pos(n), "unsupported operator %q", v.Operator)
	case *ast.UnaryNode:
		if v.Operator != "not" && v.Operator != "!" {
			return nil, errorf(l.pos(n), "unsupported operator %q", v.Operator)
		}
		if !isBoolean(v.Node) {
			return nil, errorf(l.pos(n), "%s applies to a comparison; write %s (…)", v.Operator, v.Operator)
		}
		x, err := l.boolean(v.Node)
		if err != nil {
			return nil, err
		}
		return &notNode{x}, nil
	case *ast.IdentifierNode, *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode, *ast.StringNode:
		return nil, errorf(l.pos(n), "expected a comparison")
	}
	return nil, errorf(l.pos(n), "unsupported construct %s", n.String())
}

func isBoolean(n ast.Node) bool {
	switch v := n.(type) {
	case *ast.BinaryNode:
		switch v.Operator {
		case "and", "&&", "or", "||", "==", "!=", "<", "<=", ">", ">=", "in":
			return true
		}
	case *ast.UnaryNode:
		return v.Operator == "not" || v.Operator == "!"
	}
	return false
}

func (l *lowerer) pair(v *ast.BinaryNode, mk func(a, b node) node) (node, error) {
	a, err := l.boolean(v.Left)
	if err != nil {
		return nil, err
	}
	b, err := l.boolean(v.Right)
	if err != nil {
		return nil, err
	}
	return mk(a, b), nil
}

// comparison lowers `field op int` or `int op field`.
func (l *lowerer) comparison(v *ast.BinaryNode) (node, error) {
	fieldSide, valueSide, op := v.Left, v.Right, v.Operator
	if _, ok := v.Left.(*ast.IdentifierNode); !ok {
		if _, ok := v.Right.(*ast.IdentifierNode); ok {
			fieldSide, valueSide, op = v.Right, v.Left, flip(op)
		} else if u, ok := v.Left.(*ast.UnaryNode); ok && (u.Operator == "not" || u.Operator == "!") {
			return nil, errorf(l.pos(v.Left), "%s binds tighter than %s; write %s (…)", u.Operator, v.Operator, u.Operator)
		} else {
			return nil, errorf(l.pos(v.Left), "expected a field on one side of %s", v.Operator)
		}
	}
	f, err := l.fieldOf(fieldSide)
	if err != nil {
		return nil, err
	}
	val, err := l.intLit(valueSide)
	if err != nil {
		return nil, err
	}
	if f.isSpecies() {
		if op != "==" && op != "!=" {
			return nil, errorf(l.pos(fieldSide), "%s supports ==, !=, in and not in only", f)
		}
		return l.speciesLit(f, op == "!=", []int{val}, l.pos(fieldSide), l.pos(valueSide))
	}
	return &rangeLit{f: f, set: rangeFromOp(f, op, val), pos: l.pos(fieldSide)}, nil
}

func flip(op string) string {
	switch op {
	case "<":
		return ">"
	case "<=":
		return ">="
	case ">":
		return "<"
	case ">=":
		return "<="
	}
	return op
}

// membership lowers `field in [a, b]` and `field in a..b`.
func (l *lowerer) membership(v *ast.BinaryNode) (node, error) {
	f, err := l.fieldOf(v.Left)
	if err != nil {
		return nil, err
	}
	pos := l.pos(v.Left)
	switch r := v.Right.(type) {
	case *ast.ArrayNode:
		vals := make([]int, 0, len(r.Nodes))
		valPos := pos
		for _, e := range r.Nodes {
			val, err := l.intLit(e)
			if err != nil {
				return nil, err
			}
			vals = append(vals, val)
			valPos = l.pos(e)
		}
		if f.isSpecies() {
			return l.speciesLit(f, false, vals, pos, valPos)
		}
		ivs := make([]interval, len(vals))
		for i, val := range vals {
			ivs[i] = interval{val, val}
		}
		return &rangeLit{f: f, set: setOf(ivs...).intersect(intervalSet{domains[f]}), pos: pos}, nil
	case *ast.BinaryNode:
		if r.Operator == ".." {
			if f.isSpecies() {
				return nil, errorf(pos, "%s supports a list, not a range", f)
			}
			lo, err := l.intLit(r.Left)
			if err != nil {
				return nil, err
			}
			hi, err := l.intLit(r.Right)
			if err != nil {
				return nil, err
			}
			return &rangeLit{f: f, set: setOf(interval{lo, hi}).intersect(intervalSet{domains[f]}), pos: pos}, nil
		}
	}
	return nil, errorf(l.pos(v.Right), "expected a list or a range after in")
}

func (l *lowerer) fieldOf(n ast.Node) (field, error) {
	id, ok := n.(*ast.IdentifierNode)
	if !ok {
		return 0, errorf(l.pos(n), "expected a field name, got %s", n.String())
	}
	f, ok := fieldByName[id.Value]
	if !ok {
		return 0, errorf(l.pos(n), "unknown field %q", id.Value)
	}
	return f, nil
}

func (l *lowerer) intLit(n ast.Node) (int, error) {
	val, ok := 0, false
	switch v := n.(type) {
	case *ast.IntegerNode:
		val, ok = v.Value, true
	case *ast.UnaryNode:
		if inner, isInt := v.Node.(*ast.IntegerNode); isInt && v.Operator == "-" {
			val, ok = -inner.Value, true
		}
	}
	if !ok {
		return 0, errorf(l.pos(n), "expected an integer, got %s", n.String())
	}
	if val < -maxLiteral || val > maxLiteral {
		return 0, errorf(l.pos(n), "integer %d is out of range", val)
	}
	return val, nil
}

func (l *lowerer) speciesLit(f field, neg bool, vals []int, pos, valPos Position) (node, error) {
	d := domains[f]
	for _, v := range vals {
		if f == fPokemon && v < 1 {
			return nil, errorf(valPos, `pokemon %d is not a species id; leave pokemon unconstrained for "everything else"`, v)
		}
		if v < d.lo || v > d.hi {
			return nil, errorf(valPos, "%s %d is out of range %d..%d", f, v, d.lo, d.hi)
		}
	}
	return &idLit{f: f, ids: idsOf(vals...), neg: neg, pos: pos}, nil
}

// rangeFromOp is the set of domain values v' with v' op v.
func rangeFromOp(f field, op string, v int) intervalSet {
	d := domains[f]
	var raw interval
	switch op {
	case "==":
		raw = interval{v, v}
	case "!=":
		return setOf(interval{v, v}).complement(d)
	case "<":
		raw = interval{d.lo, v - 1}
	case "<=":
		raw = interval{d.lo, v}
	case ">":
		raw = interval{v + 1, d.hi}
	case ">=":
		raw = interval{v, d.hi}
	}
	return setOf(raw).intersect(intervalSet{d})
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `gofmt -l . && go vet ./... && go test ./filterc/ -run TestParse -v 2>&1 | tail -5`
Expected: PASS. If an error-position case is off by one, check the `Column + 1` conversions before touching the message text.

- [ ] **Step 6: Commit**

```bash
git add filterc/ast.go filterc/parse.go filterc/parse_test.go
git commit -m "feat: parse expressions with Expr and lower to the boolean tree"
```

---

### Task 4: Negation normal form, DNF, splitting and the form-needs-species check

**Files:**
- Create: `filterc/nnf.go`, `filterc/dnf.go`
- Test: `filterc/dnf_test.go`

**Interfaces:**
- Consumes: Task 3's `node` types and `parse`; Task 2's sets.
- Produces: `func nnf(n node) node` (no `*notNode` in the result); `type conjunction struct` with `has [nFields]bool`, `ranges [nFields]intervalSet`, `hasSpecies bool`, `speciesPos idSet`, `speciesNeg idSet`, `hasForm bool`, `formPos idSet`, `formNeg idSet`, `formAt *Position`; `func (c conjunction) String() string`; `func toDNF(n node, maxConj int) ([]conjunction, error)`; `func split(conjs []conjunction, maxConj int) ([]conjunction, error)`; `func validate(conjs []conjunction) error`; `func tooMany(limit int) *Error`.

A conjunction's zero value is unconstrained. `has[f]` distinguishes "unconstrained" from "constrained to the empty set" (which is unsatisfiable). `hasSpecies`/`hasForm` mark a positive id set; the negative sets are always present (empty = none).

- [ ] **Step 1: Write the failing test**

`filterc/dnf_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./filterc/ -run 'TestDNF'`
Expected: FAIL — `undefined: toDNF`.

- [ ] **Step 3: Write negation normal form**

`filterc/nnf.go`:

```go
package filterc

// nnf pushes every negation down to the literals, where it becomes the
// complement within the field's domain. The result has no notNode.
func nnf(n node) node { return pushNot(n, false) }

func pushNot(n node, neg bool) node {
	switch v := n.(type) {
	case *notNode:
		return pushNot(v.x, !neg)
	case *andNode:
		if neg {
			return &orNode{pushNot(v.l, true), pushNot(v.r, true)}
		}
		return &andNode{pushNot(v.l, false), pushNot(v.r, false)}
	case *orNode:
		if neg {
			return &andNode{pushNot(v.l, true), pushNot(v.r, true)}
		}
		return &orNode{pushNot(v.l, false), pushNot(v.r, false)}
	case *rangeLit:
		if !neg {
			return v
		}
		return &rangeLit{f: v.f, set: v.set.complement(domains[v.f]), pos: v.pos}
	case *idLit:
		if !neg {
			return v
		}
		return &idLit{f: v.f, ids: v.ids, neg: !v.neg, pos: v.pos}
	}
	return n
}
```

- [ ] **Step 4: Write DNF, split and validate**

`filterc/dnf.go`:

```go
package filterc

import (
	"fmt"
	"strings"
)

// conjunction is one AND of literals. The zero value is unconstrained.
type conjunction struct {
	has    [nFields]bool        // ranges[f] is a constraint (possibly empty = unsatisfiable)
	ranges [nFields]intervalSet // species slots unused
	hasSpecies bool  // speciesPos is a constraint
	speciesPos idSet
	speciesNeg idSet
	hasForm    bool
	formPos    idSet
	formNeg    idSet
	formAt     *Position // first form literal, for the species check
}

func (c conjunction) String() string {
	var parts []string
	ids := func(s idSet) string {
		strs := make([]string, len(s))
		for i, v := range s {
			strs[i] = fmt.Sprint(v)
		}
		return "{" + strings.Join(strs, ",") + "}"
	}
	if c.hasSpecies {
		parts = append(parts, "pokemon"+ids(c.speciesPos))
	}
	if len(c.speciesNeg) > 0 {
		parts = append(parts, "!pokemon"+ids(c.speciesNeg))
	}
	if c.hasForm {
		parts = append(parts, "form"+ids(c.formPos))
	}
	if len(c.formNeg) > 0 {
		parts = append(parts, "!form"+ids(c.formNeg))
	}
	for f := range c.ranges {
		if !c.has[f] {
			continue
		}
		s := field(f).String()
		for _, iv := range c.ranges[f] {
			s += fmt.Sprintf("[%d,%d]", iv.lo, iv.hi)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

func tooMany(limit int) *Error {
	return errorf(Position{Line: 1, Column: 1}, "expression expands to more than %d clauses; simplify it", limit)
}

// fromLit makes a conjunction of one literal; ok is false when it can never
// hold (an empty set).
func fromLit(n node) (c conjunction, ok bool) {
	switch v := n.(type) {
	case *rangeLit:
		c.has[v.f], c.ranges[v.f] = true, v.set
		return c, len(v.set) > 0
	case *idLit:
		pos := v.pos
		switch {
		case v.f == fPokemon && v.neg:
			c.speciesNeg = v.ids
		case v.f == fPokemon:
			c.hasSpecies, c.speciesPos = true, v.ids
		case v.neg:
			c.formNeg, c.formAt = v.ids, &pos
		default:
			c.hasForm, c.formPos, c.formAt = true, v.ids, &pos
		}
		return c, v.neg || len(v.ids) > 0 // a positive empty set can never hold
	}
	return c, false
}

// merge ANDs two conjunctions; ok is false when the result is unsatisfiable.
func merge(a, b conjunction) (conjunction, bool) {
	c := a
	for f := range c.ranges {
		if !b.has[f] {
			continue
		}
		if !c.has[f] {
			c.has[f], c.ranges[f] = true, b.ranges[f]
			continue
		}
		c.ranges[f] = c.ranges[f].intersect(b.ranges[f])
		if len(c.ranges[f]) == 0 {
			return c, false
		}
	}
	if b.hasSpecies {
		if c.hasSpecies {
			c.speciesPos = c.speciesPos.intersect(b.speciesPos)
		} else {
			c.hasSpecies, c.speciesPos = true, b.speciesPos
		}
	}
	c.speciesNeg = c.speciesNeg.union(b.speciesNeg)
	if c.hasSpecies {
		c.speciesPos = c.speciesPos.minus(c.speciesNeg)
		if len(c.speciesPos) == 0 {
			return c, false
		}
	}
	if b.hasForm {
		if c.hasForm {
			c.formPos = c.formPos.intersect(b.formPos)
		} else {
			c.hasForm, c.formPos = true, b.formPos
		}
	}
	c.formNeg = c.formNeg.union(b.formNeg)
	if c.hasForm {
		c.formPos = c.formPos.minus(c.formNeg)
		if len(c.formPos) == 0 {
			return c, false
		}
	}
	if c.formAt == nil {
		c.formAt = b.formAt
	}
	return c, true
}

// toDNF distributes AND over OR, folding literals into conjunctions as they
// form and dropping unsatisfiable ones, so the intermediate never exceeds
// the output. n must be in negation normal form.
func toDNF(n node, maxConj int) ([]conjunction, error) {
	switch v := n.(type) {
	case *andNode:
		l, err := toDNF(v.l, maxConj)
		if err != nil {
			return nil, err
		}
		r, err := toDNF(v.r, maxConj)
		if err != nil {
			return nil, err
		}
		var out []conjunction
		for _, a := range l {
			for _, b := range r {
				if c, ok := merge(a, b); ok {
					out = append(out, c)
					if len(out) > maxConj {
						return nil, tooMany(maxConj)
					}
				}
			}
		}
		return out, nil
	case *orNode:
		l, err := toDNF(v.l, maxConj)
		if err != nil {
			return nil, err
		}
		r, err := toDNF(v.r, maxConj)
		if err != nil {
			return nil, err
		}
		if len(l)+len(r) > maxConj {
			return nil, tooMany(maxConj)
		}
		return append(l, r...), nil
	}
	if c, ok := fromLit(n); ok {
		return []conjunction{c}, nil
	}
	return nil, nil
}

// split turns a conjunction whose interval set for a field has several
// intervals into one conjunction per interval (a clause holds one range per
// field). gender is emitted as a list, so it is not split.
func split(conjs []conjunction, maxConj int) ([]conjunction, error) {
	var out []conjunction
	for _, c := range conjs {
		parts := []conjunction{c}
		for f := range c.ranges {
			if !c.has[f] || len(c.ranges[f]) <= 1 || field(f) == fGender {
				continue
			}
			var next []conjunction
			for _, p := range parts {
				for _, iv := range c.ranges[f] {
					q := p
					q.ranges[f] = intervalSet{iv}
					next = append(next, q)
				}
			}
			parts = next
			if len(out)+len(parts) > maxConj {
				return nil, tooMany(maxConj)
			}
		}
		out = append(out, parts...)
	}
	return out, nil
}

// validate rejects a form constraint without a positive species: forms
// belong to a species, and the v3 model has no "any species, this form" key.
func validate(conjs []conjunction) error {
	for _, c := range conjs {
		if (c.hasForm || len(c.formNeg) > 0) && !c.hasSpecies {
			return errorf(*c.formAt, "form needs a pokemon id in the same conjunction (after a negation, write the species explicitly: pokemon != X || (pokemon == X && form != F))")
		}
	}
	return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `gofmt -l . && go vet ./... && go test ./filterc/ -run 'TestDNF' -v 2>&1 | tail -4`
Expected: PASS. The `iv != 1 && level != 1 && cp != 1` case must fail in `split` (2×2×2 = 8 > 4), the other cap case in `toDNF`.

- [ ] **Step 6: Commit**

```bash
git add filterc/nnf.go filterc/dnf.go filterc/dnf_test.go
git commit -m "feat: negation normal form, DNF with early pruning, splitting and validation"
```

---

### Task 5: Wire types, dispatch, emission and `Compile`

**Files:**
- Create: `filterc/types.go`, `filterc/dispatch.go`, `filterc/compile.go`
- Test: `filterc/golden_test.go`

**Interfaces:**
- Consumes: Task 4 (`conjunction`, `nnf`, `toDNF`, `split`, `validate`, `tooMany`), Task 3 (`parse`).
- Produces (public API): `type MinMax struct{ Min, Max int }`; `type PokemonId struct{ Id int; Form *int }`; `type Clause struct{...}` (v3 wire shape, see code); `type LatLon struct{ Lat, Lon float64 }`; `type Bounds struct{ Min, Max LatLon }`; `type ScanRequest struct{ Min, Max LatLon; Limit int; Filters []Clause }`; `type Compiled struct{ Filters []Clause; Warnings []string }`; `func (c *Compiled) Request(b Bounds, limit int) ScanRequest`; `type Option func(*options)`; `func WithMaxConjunctions(n int) Option`; `func WithMaxClauses(n int) Option`; `const DefaultMaxConjunctions = 512`, `DefaultMaxClauses = 10000`; `func Compile(expression string, opts ...Option) (*Compiled, error)`. Internal: `type key struct{ species, form int }` with `const anyForm = -1`; `func dispatch(conjs []conjunction, maxClauses int) ([]Clause, error)`; `func applies(c conjunction, k key) bool`; `func blockClause(k key) Clause`.

- [ ] **Step 1: Write the failing golden test**

`filterc/golden_test.go`:

```go
package filterc

import (
	"encoding/json"
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
	if _, err := Compile(`iv != 1 && level != 1`, WithMaxConjunctions(2)); err == nil {
		t.Error("expected the conjunction limit to trip")
	}
	if _, err := Compile(`pokemon in [1, 2, 3]`, WithMaxClauses(0)); err == nil {
		t.Error("expected the clause limit to trip")
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./filterc/ -run 'TestCompile|TestRequest'`
Expected: FAIL — `undefined: Compile`.

- [ ] **Step 3: Write the wire types**

`filterc/types.go`:

```go
package filterc

// MinMax is an inclusive integer range as the v3 API takes it. Both bounds
// are always set by the compiler.
type MinMax struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// PokemonId is one entry of a clause's pokemon list: a species id and an
// optional form. A nil Form means any form of the species.
type PokemonId struct {
	Id   int  `json:"id"`
	Form *int `json:"form,omitempty"`
}

// Clause is one v3 filter clause. A clause with no Pokemon list is filed
// under "everything else".
type Clause struct {
	Pokemon []PokemonId `json:"pokemon,omitempty"`
	Iv      *MinMax     `json:"iv,omitempty"`
	AtkIv   *MinMax     `json:"atk_iv,omitempty"`
	DefIv   *MinMax     `json:"def_iv,omitempty"`
	StaIv   *MinMax     `json:"sta_iv,omitempty"`
	Level   *MinMax     `json:"level,omitempty"`
	Cp      *MinMax     `json:"cp,omitempty"`
	Gender  []int       `json:"gender,omitempty"`
	Size    *MinMax     `json:"size,omitempty"`
	Little  *MinMax     `json:"pvp_little,omitempty"`
	Great   *MinMax     `json:"pvp_great,omitempty"`
	Ultra   *MinMax     `json:"pvp_ultra,omitempty"`
}

func (cl *Clause) setRange(f field, mm *MinMax) {
	switch f {
	case fIv:
		cl.Iv = mm
	case fAtk:
		cl.AtkIv = mm
	case fDef:
		cl.DefIv = mm
	case fSta:
		cl.StaIv = mm
	case fLevel:
		cl.Level = mm
	case fCp:
		cl.Cp = mm
	case fSize:
		cl.Size = mm
	case fLittle:
		cl.Little = mm
	case fGreat:
		cl.Great = mm
	case fUltra:
		cl.Ultra = mm
	}
}

// LatLon is a coordinate as the scan API takes it.
type LatLon struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// Bounds is the scan's bounding box.
type Bounds struct {
	Min LatLon
	Max LatLon
}

// ScanRequest is the body of POST /api/pokemon/v3/scan.
type ScanRequest struct {
	Min     LatLon   `json:"min"`
	Max     LatLon   `json:"max"`
	Limit   int      `json:"limit,omitempty"`
	Filters []Clause `json:"filters"`
}

// Compiled is the result of compiling an expression.
type Compiled struct {
	Filters  []Clause // never nil, so it marshals as []
	Warnings []string
}

// Request wraps the filters into a full scan request body.
func (c *Compiled) Request(b Bounds, limit int) ScanRequest {
	return ScanRequest{Min: b.Min, Max: b.Max, Limit: limit, Filters: c.Filters}
}
```

- [ ] **Step 4: Write dispatch**

`filterc/dispatch.go`:

```go
package filterc

import (
	"cmp"
	"maps"
	"slices"
)

// key is a scan group: a species and either a specific form or anyForm.
type key struct{ species, form int }

const anyForm = -1

func (k key) pokemonId() PokemonId {
	if k.form == anyForm {
		return PokemonId{Id: k.species}
	}
	f := k.form
	return PokemonId{Id: k.species, Form: &f}
}

// applies reports whether conjunction c holds for a representative pokemon
// of key k. For a species key the representative is a form of the species
// with no exact key of its own; every form named negatively has an exact
// key, so such a representative is outside every negative form set.
func applies(c conjunction, k key) bool {
	if c.hasSpecies && !c.speciesPos.contains(k.species) {
		return false
	}
	if c.speciesNeg.contains(k.species) {
		return false
	}
	if k.form == anyForm {
		return !c.hasForm
	}
	if c.hasForm && !c.formPos.contains(k.form) {
		return false
	}
	return !c.formNeg.contains(k.form)
}

func blockClause(k key) Clause {
	return Clause{Pokemon: []PokemonId{k.pokemonId()}, Iv: &MinMax{Min: 1, Max: 0}}
}

func clauseFor(c conjunction, ids []PokemonId) Clause {
	cl := Clause{Pokemon: ids}
	for f := range c.ranges {
		if !c.has[f] {
			continue
		}
		if field(f) == fGender {
			cl.Gender = c.ranges[f].values()
			continue
		}
		iv := c.ranges[f][0] // exactly one interval after split
		cl.setRange(field(f), &MinMax{Min: iv.lo, Max: iv.hi})
	}
	return cl
}

// dispatch computes, per (species, form) key the expression names, the
// conjunctions Golbat's probe order (exact, then species, then everything
// else) would select for it, and emits clauses so that no group needs to
// inherit from another at scan time.
func dispatch(conjs []conjunction, maxClauses int) ([]Clause, error) {
	distinguished := map[key]struct{}{}
	for _, c := range conjs {
		if c.hasSpecies {
			for _, s := range c.speciesPos {
				if c.hasForm {
					for _, f := range c.formPos {
						distinguished[key{s, f}] = struct{}{}
					}
				} else {
					distinguished[key{s, anyForm}] = struct{}{}
				}
				for _, f := range c.formNeg {
					distinguished[key{s, f}] = struct{}{}
				}
			}
		}
		for _, s := range c.speciesNeg {
			distinguished[key{s, anyForm}] = struct{}{}
		}
	}
	keys := slices.SortedFunc(maps.Keys(distinguished), func(a, b key) int {
		return cmp.Or(cmp.Compare(a.species, b.species), cmp.Compare(a.form, b.form))
	})

	clauses := []Clause{}
	used := make(map[key]bool, len(keys))
	for _, c := range conjs {
		if !c.hasSpecies {
			clauses = append(clauses, clauseFor(c, nil))
		}
		var ids []PokemonId
		for _, k := range keys {
			if applies(c, k) {
				ids = append(ids, k.pokemonId())
				used[k] = true
			}
		}
		if len(ids) > 0 {
			clauses = append(clauses, clauseFor(c, ids))
		}
	}
	for _, k := range keys {
		if !used[k] {
			clauses = append(clauses, blockClause(k))
		}
	}
	if len(clauses) > maxClauses {
		return nil, tooMany(maxClauses)
	}
	return clauses, nil
}
```

- [ ] **Step 5: Write `Compile`**

`filterc/compile.go`:

```go
package filterc

const (
	DefaultMaxConjunctions = 512
	DefaultMaxClauses      = 10000
)

type options struct {
	maxConjunctions, maxClauses int
}

// Option adjusts Compile's limits.
type Option func(*options)

// WithMaxConjunctions caps the number of conjunctions the expression may
// expand to (default DefaultMaxConjunctions).
func WithMaxConjunctions(n int) Option { return func(o *options) { o.maxConjunctions = n } }

// WithMaxClauses caps the number of emitted clauses (default DefaultMaxClauses).
func WithMaxClauses(n int) Option { return func(o *options) { o.maxClauses = n } }

// Compile turns an expression into v3 filter clauses. Errors are *Error
// with a position.
func Compile(expression string, opts ...Option) (*Compiled, error) {
	o := options{maxConjunctions: DefaultMaxConjunctions, maxClauses: DefaultMaxClauses}
	for _, opt := range opts {
		opt(&o)
	}
	n, err := parse(expression)
	if err != nil {
		return nil, err
	}
	conjs, err := toDNF(nnf(n), o.maxConjunctions)
	if err != nil {
		return nil, err
	}
	if conjs, err = split(conjs, o.maxConjunctions); err != nil {
		return nil, err
	}
	if err := validate(conjs); err != nil {
		return nil, err
	}
	clauses, err := dispatch(conjs, o.maxClauses)
	if err != nil {
		return nil, err
	}
	c := &Compiled{Filters: clauses}
	if len(clauses) == 0 {
		c.Warnings = append(c.Warnings, "the expression can never hold; the request matches nothing")
	}
	return c, nil
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `gofmt -l . && go vet ./... && go test ./filterc/ -v 2>&1 | grep -E '^(--- FAIL|ok|FAIL)'`
Expected: `ok`. If a golden case differs only in key order, check the sort in `dispatch` (species ascending, then form ascending with `anyForm = -1` first) before changing the expectation.

- [ ] **Step 7: Commit**

```bash
git add filterc/types.go filterc/dispatch.go filterc/compile.go filterc/golden_test.go
git commit -m "feat: dispatch to scan groups, emit v3 clauses, Compile API"
```

---

### Task 6: Reference evaluator, vendored Golbat matcher, property test, invariants, fuzz

**Files:**
- Create (all `_test.go`): `filterc/row_test.go`, `filterc/reference_test.go`, `filterc/matcher_test.go`, `filterc/property_test.go`, `filterc/fuzz_test.go`

**Interfaces:**
- Consumes: `Compile`, `Clause`, `MinMax`, `PokemonId`, `Error`, `field` constants, `domains`.
- Produces (test-only): `type row struct{...}` with `func (r row) get(f field) (v int, known bool)`; `type tv int8` with `tFalse, tTrue, tUnknown`; `func reference(t *testing.T, src string, r row) tv`; `type dnfKey struct{ pokemon, form int }`; `func indexClauses(filters []Clause) map[dnfKey][]Clause`; `func matchV3(index map[dnfKey][]Clause, r row) bool`; `func genExpr(rng *rand.Rand, depth int) string`; `func genRow(rng *rand.Rand) row`; `func checkInvariants(t *testing.T, src string, c *Compiled)`.

The reference evaluator walks Expr's AST directly (op and literal value), so it shares nothing with lowering, NNF, DNF or dispatch. The matcher is Golbat's, transcribed line for line.

- [ ] **Step 1: Write the row and the reference evaluator**

`filterc/row_test.go`:

```go
package filterc

// row is one pokemon as Golbat's lookup cache holds it: -1 for "no
// encounter data" in the encounter fields; pvp nil when the pokemon has no
// PvP data at all, else ranks with 4096 for "unranked in this league".
type row struct {
	pokemonId, form                          int
	iv, atk, def, sta, level, cp, gender, size int
	pvp                                      *pvpRow
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
```

`filterc/reference_test.go`:

```go
package filterc

import (
	"testing"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// tv is a three-valued truth value (SQL style): comparisons on a missing
// PvP rank are unknown, not unknown is unknown, and/or follow Kleene.
type tv int8

const (
	tFalse tv = iota
	tTrue
	tUnknown
)

func kleeneAnd(a, b tv) tv {
	if a == tFalse || b == tFalse {
		return tFalse
	}
	if a == tTrue && b == tTrue {
		return tTrue
	}
	return tUnknown
}

func kleeneOr(a, b tv) tv {
	if a == tTrue || b == tTrue {
		return tTrue
	}
	if a == tFalse && b == tFalse {
		return tFalse
	}
	return tUnknown
}

func kleeneNot(a tv) tv {
	switch a {
	case tTrue:
		return tFalse
	case tFalse:
		return tTrue
	}
	return tUnknown
}

func boolTV(b bool) tv {
	if b {
		return tTrue
	}
	return tFalse
}

// reference evaluates src for r straight from Expr's AST. It is the
// definition of the language the compiler is tested against.
func reference(t *testing.T, src string, r row) tv {
	t.Helper()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("reference: parse %q: %v", src, err)
	}
	return refNode(t, tree.Node, r)
}

func refNode(t *testing.T, n ast.Node, r row) tv {
	t.Helper()
	switch v := n.(type) {
	case *ast.BinaryNode:
		switch v.Operator {
		case "and", "&&":
			return kleeneAnd(refNode(t, v.Left, r), refNode(t, v.Right, r))
		case "or", "||":
			return kleeneOr(refNode(t, v.Left, r), refNode(t, v.Right, r))
		case "==", "!=", "<", "<=", ">", ">=":
			return refCompare(t, v, r)
		case "in":
			return refIn(t, v, r)
		}
	case *ast.UnaryNode:
		if v.Operator == "not" || v.Operator == "!" {
			return kleeneNot(refNode(t, v.Node, r))
		}
	}
	t.Fatalf("reference: unsupported node %s", n.String())
	return tUnknown
}

func refInt(t *testing.T, n ast.Node) int {
	t.Helper()
	switch v := n.(type) {
	case *ast.IntegerNode:
		return v.Value
	case *ast.UnaryNode:
		if inner, ok := v.Node.(*ast.IntegerNode); ok && v.Operator == "-" {
			return -inner.Value
		}
	}
	t.Fatalf("reference: not an integer: %s", n.String())
	return 0
}

func refField(t *testing.T, n ast.Node, r row) (int, bool) {
	t.Helper()
	id, ok := n.(*ast.IdentifierNode)
	if !ok {
		t.Fatalf("reference: not a field: %s", n.String())
	}
	return r.get(fieldByName[id.Value])
}

func refCompare(t *testing.T, v *ast.BinaryNode, r row) tv {
	t.Helper()
	fieldSide, valueSide, op := v.Left, v.Right, v.Operator
	if _, ok := v.Left.(*ast.IdentifierNode); !ok {
		fieldSide, valueSide, op = v.Right, v.Left, flip(op)
	}
	x, known := refField(t, fieldSide, r)
	if !known {
		return tUnknown
	}
	y := refInt(t, valueSide)
	switch op {
	case "==":
		return boolTV(x == y)
	case "!=":
		return boolTV(x != y)
	case "<":
		return boolTV(x < y)
	case "<=":
		return boolTV(x <= y)
	case ">":
		return boolTV(x > y)
	}
	return boolTV(x >= y)
}

func refIn(t *testing.T, v *ast.BinaryNode, r row) tv {
	t.Helper()
	x, known := refField(t, v.Left, r)
	if !known {
		return tUnknown
	}
	switch right := v.Right.(type) {
	case *ast.ArrayNode:
		for _, e := range right.Nodes {
			if refInt(t, e) == x {
				return tTrue
			}
		}
		return tFalse
	case *ast.BinaryNode:
		if right.Operator == ".." {
			return boolTV(refInt(t, right.Left) <= x && x <= refInt(t, right.Right))
		}
	}
	t.Fatalf("reference: bad membership %s", v.String())
	return tUnknown
}

func TestReferenceSemantics(t *testing.T) {
	noPvp := row{pokemonId: 1, iv: 100}
	unranked := row{pokemonId: 1, iv: -1, pvp: &pvpRow{little: 4096, great: 4096, ultra: 4096}}
	cases := []struct {
		src  string
		r    row
		want tv
	}{
		{"great <= 100", noPvp, tUnknown},
		{"!(great <= 100)", noPvp, tUnknown},
		{"great <= 100 || iv == 100", noPvp, tTrue},
		{"great <= 100 && iv == 100", noPvp, tUnknown},
		{"!(great <= 100)", unranked, tTrue},
		{"great == 4096", unranked, tTrue},
		{"!(iv >= 90)", unranked, tTrue},
		{"iv in -1..100", unranked, tTrue},
		{"pokemon not in [1, 4]", noPvp, tFalse},
		{"100 == iv", noPvp, tTrue},
	}
	for _, c := range cases {
		if got := reference(t, c.src, c.r); got != c.want {
			t.Errorf("%q on %+v = %v, want %v", c.src, c.r, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the reference tests**

Run: `go test ./filterc/ -run TestReferenceSemantics`
Expected: PASS (this test pins the language's semantics before the matcher is involved).

- [ ] **Step 3: Write the vendored matcher and its recipe test**

`filterc/matcher_test.go`:

```go
package filterc

import (
	"slices"
	"testing"
)

// matchV3 and isPokemonDnfMatch are Golbat's, transcribed from
// decoder/api_pokemon_common.go (the probe chain) and
// decoder/api_pokemon_scan_v3.go (the clause match) at commit 5cd3ed7
// (2026-09-24), with Clause in place of ApiPokemonDnfFilter3 and int in
// place of int8/int16. Re-sync by hand when Golbat's matcher changes.

type dnfKey struct{ pokemon, form int }

func indexClauses(filters []Clause) map[dnfKey][]Clause {
	dnfFilters := make(map[dnfKey][]Clause)
	for _, filter := range filters {
		if len(filter.Pokemon) > 0 {
			for _, keyString := range filter.Pokemon {
				pokemonId := keyString.Id
				if pokemonId == 0 {
					pokemonId = -1
				}
				formId := -1
				if keyString.Form != nil {
					formId = *keyString.Form
				}
				key := dnfKey{pokemon: pokemonId, form: formId}
				dnfFilters[key] = append(dnfFilters[key], filter)
			}
		} else {
			key := dnfKey{pokemon: -1, form: -1}
			dnfFilters[key] = append(dnfFilters[key], filter)
		}
	}
	return dnfFilters
}

func matchV3(dnfFilters map[dnfKey][]Clause, r row) bool {
	filters, found := dnfFilters[dnfKey{pokemon: r.pokemonId, form: r.form}]
	if !found {
		filters, found = dnfFilters[dnfKey{pokemon: r.pokemonId, form: -1}]
		if !found {
			filters, found = dnfFilters[dnfKey{pokemon: -1, form: -1}]
			if !found {
				return false
			}
		}
	}
	for x := 0; x < len(filters); x++ {
		if isPokemonDnfMatch(r, &filters[x]) {
			return true
		}
	}
	return false
}

func isPokemonDnfMatch(pokemonLookup row, filter *Clause) bool {
	if filter.Iv != nil && (pokemonLookup.iv < filter.Iv.Min || pokemonLookup.iv > filter.Iv.Max) ||
		filter.StaIv != nil && (pokemonLookup.sta < filter.StaIv.Min || pokemonLookup.sta > filter.StaIv.Max) ||
		filter.AtkIv != nil && (pokemonLookup.atk < filter.AtkIv.Min || pokemonLookup.atk > filter.AtkIv.Max) ||
		filter.DefIv != nil && (pokemonLookup.def < filter.DefIv.Min || pokemonLookup.def > filter.DefIv.Max) ||
		filter.Level != nil && (pokemonLookup.level < filter.Level.Min || pokemonLookup.level > filter.Level.Max) ||
		filter.Cp != nil && (pokemonLookup.cp < filter.Cp.Min || pokemonLookup.cp > filter.Cp.Max) ||
		(len(filter.Gender) > 0 && !slices.Contains(filter.Gender, pokemonLookup.gender)) ||
		filter.Size != nil && (pokemonLookup.size < filter.Size.Min || pokemonLookup.size > filter.Size.Max) {
		return false
	}
	pvpLookup := pokemonLookup.pvp
	if filter.Little != nil && (pvpLookup == nil || pvpLookup.little < filter.Little.Min || pvpLookup.little > filter.Little.Max) ||
		filter.Great != nil && (pvpLookup == nil || pvpLookup.great < filter.Great.Min || pvpLookup.great > filter.Great.Max) ||
		filter.Ultra != nil && (pvpLookup == nil || pvpLookup.ultra < filter.Ultra.Min || pvpLookup.ultra > filter.Ultra.Max) {
		return false
	}
	return true
}

// The matcher reproduces the recipes in Golbat's api.md "Filter semantics".
func TestMatcherRecipes(t *testing.T) {
	mm := func(lo, hi int) *MinMax { return &MinMax{lo, hi} }
	id := func(i int) PokemonId { return PokemonId{Id: i} }
	maleBulbasaur := row{pokemonId: 1, gender: 1, iv: 100}
	femaleBulbasaur := row{pokemonId: 1, gender: 2, iv: 100}
	malePidgey := row{pokemonId: 16, gender: 1, iv: 100}

	everythingElse := indexClauses([]Clause{
		{Pokemon: []PokemonId{id(1)}, Gender: []int{2}, Iv: mm(100, 100)},
		{Iv: mm(100, 100)},
	})
	if matchV3(everythingElse, maleBulbasaur) || !matchV3(everythingElse, femaleBulbasaur) || !matchV3(everythingElse, malePidgey) {
		t.Error("everything else must not apply to a species with its own clause")
	}
	merged := indexClauses([]Clause{
		{Pokemon: []PokemonId{id(1)}, Gender: []int{2}},
		{Pokemon: []PokemonId{id(1)}, Iv: mm(100, 100)},
		{Iv: mm(100, 100)},
	})
	if !matchV3(merged, maleBulbasaur) {
		t.Error("a shared clause listed under the species must apply there")
	}
	block := indexClauses([]Clause{
		{Size: mm(5, 5)},
		{Pokemon: []PokemonId{id(710)}, Iv: mm(1, 0)},
	})
	if !matchV3(block, row{pokemonId: 1, size: 5}) || matchV3(block, row{pokemonId: 710, size: 5}) {
		t.Error("a clause that can never hold hides the species")
	}
	if matchV3(indexClauses(nil), malePidgey) || !matchV3(indexClauses([]Clause{{}}), malePidgey) {
		t.Error("empty filters match nothing; one empty clause matches everything")
	}
}
```

- [ ] **Step 4: Run the matcher test**

Run: `go test ./filterc/ -run TestMatcherRecipes`
Expected: PASS.

- [ ] **Step 5: Write the generators, invariants and the property test**

`filterc/property_test.go`:

```go
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
// expression names.
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
	switch rng.Intn(10) {
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
	}
	// species with a form
	return fmt.Sprintf("(pokemon == %d && form %s %d)", 1+rng.Intn(4), pick(rng, []string{"==", "!="}), rng.Intn(3))
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
```

- [ ] **Step 6: Run the property test**

Run: `go test ./filterc/ -run TestCompileMatchesReference -v 2>&1 | tail -3`
Expected: PASS with a log line like `checked 27xx expressions`. A failure prints the expression, the row and the compiled filters; reduce by hand and add the case to `TestCompileGolden` before fixing the compiler.

- [ ] **Step 7: Write the fuzz test and run it briefly**

`filterc/fuzz_test.go`:

```go
package filterc

import (
	"errors"
	"testing"
)

func FuzzCompile(f *testing.F) {
	for _, s := range []string{
		"size == 5 && pokemon != 710",
		"iv == 100 || (pokemon == 1 && gender == 2)",
		"!(iv >= 90)",
		"pokemon in [1, 4] && form != 0",
		"great <= 100 || little not in 1..10",
		"not great <= 100",
		"iv >= 90 &&",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		c, err := Compile(src)
		if err != nil {
			var e *Error
			if !errors.As(err, &e) {
				t.Fatalf("%q: error is not *Error: %v", src, err)
			}
			return
		}
		checkInvariants(t, src, c)
	})
}
```

Run: `go test ./filterc/ -run xxx -fuzz FuzzCompile -fuzztime 20s 2>&1 | tail -3`
Expected: no crashers (`ok` or "elapsed" summary). If one is found, the reproducer lands in `testdata/fuzz/FuzzCompile/`; commit it with the fix.

- [ ] **Step 8: Commit**

```bash
gofmt -l . && go vet ./... && go test ./filterc/
git add filterc/row_test.go filterc/reference_test.go filterc/matcher_test.go filterc/property_test.go filterc/fuzz_test.go
git commit -m "test: reference evaluator, vendored Golbat matcher, property and fuzz tests"
```

---

### Task 7: Golbat client

**Files:**
- Create: `golbat/client.go`
- Test: `golbat/client_test.go`

**Interfaces:**
- Consumes: `filterc.ScanRequest`.
- Produces: `type Client struct{ URL, Secret string; HTTP *http.Client }`; `type ScanResponse struct{ Pokemon []json.RawMessage; Examined, Skipped, Total int; LimitReached bool }`; `type StatusError struct{ Status int; Body string }` implementing `error`; `func (c *Client) ScanPokemon(ctx context.Context, req filterc.ScanRequest) (*ScanResponse, error)`.

- [ ] **Step 1: Write the failing test**

`golbat/client_test.go`:

```go
package golbat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jfberry/golbat-filterc/filterc"
)

func TestScanPokemon(t *testing.T) {
	var gotPath, gotSecret, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotSecret = r.URL.Path, r.Header.Get("X-Golbat-Secret")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"pokemon":[{"id":"1","pokemon_id":25}],"examined":10,"skipped":1,"total":11,"limit_reached":false}`)
	}))
	defer srv.Close()

	c := &Client{URL: srv.URL + "/", Secret: "s3cret"}
	resp, err := c.ScanPokemon(context.Background(), filterc.ScanRequest{
		Min: filterc.LatLon{Lat: 1, Lon: 2}, Max: filterc.LatLon{Lat: 3, Lon: 4}, Limit: 5,
		Filters: []filterc.Clause{{Iv: &filterc.MinMax{Min: 100, Max: 100}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/pokemon/v3/scan" || gotSecret != "s3cret" {
		t.Errorf("path %q secret %q", gotPath, gotSecret)
	}
	if want := `{"min":{"lat":1,"lon":2},"max":{"lat":3,"lon":4},"limit":5,"filters":[{"iv":{"min":100,"max":100}}]}`; gotBody != want {
		t.Errorf("body %s, want %s", gotBody, want)
	}
	if len(resp.Pokemon) != 1 || resp.Examined != 10 || resp.Skipped != 1 || resp.Total != 11 || resp.LimitReached {
		t.Errorf("response %+v", resp)
	}
	var first map[string]any
	if json.Unmarshal(resp.Pokemon[0], &first) != nil || first["pokemon_id"] != float64(25) {
		t.Errorf("pokemon passthrough = %s", resp.Pokemon[0])
	}
}

func TestScanPokemonStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	_, err := (&Client{URL: srv.URL}).ScanPokemon(context.Background(), filterc.ScanRequest{Filters: []filterc.Clause{}})
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 401 || se.Body != "unauthorized\n" {
		t.Errorf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./golbat/`
Expected: FAIL — `undefined: Client`.

- [ ] **Step 3: Write the client**

`golbat/client.go`:

```go
// Package golbat is a minimal client for the Golbat scan API used by
// filterc to run compiled requests.
package golbat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/jfberry/golbat-filterc/filterc"
)

// Client calls a Golbat instance. Secret is sent as X-Golbat-Secret when
// set. A nil HTTP uses http.DefaultClient.
type Client struct {
	URL    string
	Secret string
	HTTP   *http.Client
}

// ScanResponse is Golbat's v3 scan response with the pokemon left as raw
// JSON so nothing is lost or reinterpreted.
type ScanResponse struct {
	Pokemon      []json.RawMessage `json:"pokemon"`
	Examined     int               `json:"examined"`
	Skipped      int               `json:"skipped"`
	Total        int               `json:"total"`
	LimitReached bool              `json:"limit_reached"`
}

// StatusError is a non-2xx reply from Golbat.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("golbat returned %d: %s", e.Status, strings.TrimSpace(e.Body))
}

// ScanPokemon posts req to /api/pokemon/v3/scan.
func (c *Client) ScanPokemon(ctx context.Context, req filterc.ScanRequest) (*ScanResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(c.URL, "/") + "/api/pokemon/v3/scan"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.Secret != "" {
		httpReq.Header.Set("X-Golbat-Secret", c.Secret)
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &StatusError{Status: resp.StatusCode, Body: string(b)}
	}
	var out ScanResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding golbat response: %w", err)
	}
	return &out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `gofmt -l . && go vet ./... && go test ./golbat/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add golbat/
git commit -m "feat: minimal Golbat scan client"
```

---

### Task 8: CLI — config file, `compile` and `scan`

**Files:**
- Create: `cmd/filterc/config.go`, `cmd/filterc/main.go`
- Test: `cmd/filterc/main_test.go`

**Interfaces:**
- Consumes: `filterc.Compile`, `Compiled.Request`, `filterc.Bounds`, `filterc.LatLon`, `*filterc.Error`, `golbat.Client`.
- Produces: `type Config struct{ Listen string; Golbat struct{ URL, Secret string }; Bounds struct{ Min, Max latLon; Limit int } }` with `func (c Config) bounds() filterc.Bounds`; `func loadConfig(path string) (Config, error)`; `func run(args []string, stdout, stderr io.Writer) int`; `func main()`. Task 9 adds `serve` to `run`.

- [ ] **Step 1: Write the failing test**

`cmd/filterc/main_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestCompileCommand(t *testing.T) {
	code, out, errb := runCLI(t, "compile", "--json", "--min-lat", "1", "--min-lon", "2", "--max-lat", "3", "--max-lon", "4", "--limit", "10", "size == 5 && pokemon != 710")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	want := `{"min":{"lat":1,"lon":2},"max":{"lat":3,"lon":4},"limit":10,"filters":[{"size":{"min":5,"max":5}},{"pokemon":[{"id":710}],"iv":{"min":1,"max":0}}]}` + "\n"
	if out != want {
		t.Errorf("stdout %s\nwant %s", out, want)
	}
}

func TestCompileCommandErrorShowsCaret(t *testing.T) {
	code, out, errb := runCLI(t, "compile", "iv >= 90 && x == 1")
	if code != 1 || out != "" {
		t.Errorf("exit %d stdout %q", code, out)
	}
	want := "error: 1:13: unknown field \"x\"\n  iv >= 90 && x == 1\n  " + strings.Repeat(" ", 12) + "^\n"
	if errb != want {
		t.Errorf("stderr %q\nwant %q", errb, want)
	}
}

func TestCompileWarningsGoToStderr(t *testing.T) {
	code, _, errb := runCLI(t, "compile", "iv > 100")
	if code != 0 || !strings.Contains(errb, "warning: the expression can never hold") {
		t.Errorf("exit %d stderr %q", code, errb)
	}
}

func TestConfigFileAndFlagPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)                      // no stray ./filterc.toml
	t.Setenv("XDG_CONFIG_HOME", dir)  // no stray ~/.config/filterc/filterc.toml
	path := filepath.Join(dir, "filterc.toml")
	os.WriteFile(path, []byte(`
listen = ":9999"
[golbat]
url = "http://golbat.example:9001"
secret = "abc"
[bounds]
min = { lat = 51.4, lon = -0.2 }
max = { lat = 51.6, lon = 0.1 }
limit = 300
`), 0o644)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":9999" || cfg.Golbat.URL != "http://golbat.example:9001" || cfg.Golbat.Secret != "abc" ||
		cfg.Bounds.Min.Lat != 51.4 || cfg.Bounds.Max.Lon != 0.1 || cfg.Bounds.Limit != 300 {
		t.Errorf("config %+v", cfg)
	}
	code, out, errb := runCLI(t, "compile", "--json", "--config", path, "--max-lat", "52", "iv == 100")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	if want := `{"min":{"lat":51.4,"lon":-0.2},"max":{"lat":52,"lon":0.1},"limit":300,"filters":[{"iv":{"min":100,"max":100}}]}` + "\n"; out != want {
		t.Errorf("stdout %s\nwant %s", out, want)
	}
	if _, err := loadConfig(filepath.Join(dir, "missing.toml")); err == nil {
		t.Error("an explicit missing config file must be an error")
	}
	if cfg, err := loadConfig(""); err != nil || cfg.Listen != ":8080" {
		t.Errorf("no config file: %+v %v", cfg, err)
	}
}

func TestScanCommandNeedsGolbat(t *testing.T) {
	code, _, errb := runCLI(t, "scan", "iv == 100")
	if code != 1 || !strings.Contains(errb, "golbat url") {
		t.Errorf("exit %d stderr %q", code, errb)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/filterc/`
Expected: FAIL — `undefined: run`.

- [ ] **Step 3: Write the config**

`cmd/filterc/config.go`:

```go
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/jfberry/golbat-filterc/filterc"
)

type latLon struct {
	Lat float64 `toml:"lat"`
	Lon float64 `toml:"lon"`
}

// Config is filterc.toml. Flags override it; it overrides the defaults.
type Config struct {
	Listen string `toml:"listen"`
	Golbat struct {
		URL    string `toml:"url"`
		Secret string `toml:"secret"`
	} `toml:"golbat"`
	Bounds struct {
		Min   latLon `toml:"min"`
		Max   latLon `toml:"max"`
		Limit int    `toml:"limit"`
	} `toml:"bounds"`
}

func defaultConfig() Config {
	var c Config
	c.Listen = ":8080"
	return c
}

func (c Config) bounds() filterc.Bounds {
	return filterc.Bounds{
		Min: filterc.LatLon{Lat: c.Bounds.Min.Lat, Lon: c.Bounds.Min.Lon},
		Max: filterc.LatLon{Lat: c.Bounds.Max.Lat, Lon: c.Bounds.Max.Lon},
	}
}

// loadConfig reads path, or with an empty path the first of ./filterc.toml
// and $XDG_CONFIG_HOME/filterc/filterc.toml (default ~/.config) that exists.
// An explicit path must exist; a missing default is not an error.
func loadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	explicit := path != ""
	if !explicit {
		candidates := []string{"filterc.toml"}
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			candidates = append(candidates, filepath.Join(xdg, "filterc", "filterc.toml"))
		} else if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".config", "filterc", "filterc.toml"))
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				path = c
				break
			}
		}
		if path == "" {
			return cfg, nil
		}
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if explicit || !errors.Is(err, os.ErrNotExist) {
			return cfg, fmt.Errorf("config %s: %w", path, err)
		}
	}
	return cfg, nil
}
```

- [ ] **Step 4: Write the command**

`cmd/filterc/main.go`:

```go
// Command filterc compiles filter expressions into Golbat pokemon scan
// requests, and can run them.
//
//	filterc compile [flags] 'expression'   print the v3 request body
//	filterc scan    [flags] 'expression'   compile, call Golbat, print the response
//	filterc serve   [flags]                HTTP server (see serve.go)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jfberry/golbat-filterc/filterc"
	"github.com/jfberry/golbat-filterc/golbat"
)

const usage = `usage:
  filterc compile [flags] 'expression'   print the v3 scan request body
  filterc scan    [flags] 'expression'   compile, run against Golbat, print the response
  filterc serve   [flags]                HTTP server: POST /compile, POST /scan, GET /healthz

flags (all commands; a flag overrides filterc.toml, which overrides the defaults):
  --config path   config file (default ./filterc.toml, then $XDG_CONFIG_HOME/filterc/filterc.toml)
  --min-lat --min-lon --max-lat --max-lon   scan bounds
  --limit n       result limit (0 = Golbat's default)
  --golbat url    Golbat base URL          --secret s   Golbat api_secret
  --listen addr   serve address            --json       compact output
`

// settings is the config with flags applied.
type settings struct {
	Config
	compact bool
}

func parseFlags(args []string, stderr io.Writer) (settings, []string, error) {
	fs := flag.NewFlagSet("filterc", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "")
	minLat, minLon := fs.Float64("min-lat", 0, ""), fs.Float64("min-lon", 0, "")
	maxLat, maxLon := fs.Float64("max-lat", 0, ""), fs.Float64("max-lon", 0, "")
	limit := fs.Int("limit", 0, "")
	golbatURL, secret := fs.String("golbat", "", ""), fs.String("secret", "", "")
	listen := fs.String("listen", "", "")
	compact := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil {
		return settings{}, nil, err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return settings{}, nil, err
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "min-lat":
			cfg.Bounds.Min.Lat = *minLat
		case "min-lon":
			cfg.Bounds.Min.Lon = *minLon
		case "max-lat":
			cfg.Bounds.Max.Lat = *maxLat
		case "max-lon":
			cfg.Bounds.Max.Lon = *maxLon
		case "limit":
			cfg.Bounds.Limit = *limit
		case "golbat":
			cfg.Golbat.URL = *golbatURL
		case "secret":
			cfg.Golbat.Secret = *secret
		case "listen":
			cfg.Listen = *listen
		}
	})
	return settings{Config: cfg, compact: *compact}, fs.Args(), nil
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd := args[0]
	s, rest, err := parseFlags(args[1:], stderr)
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(stderr, "error: %v\n", err)
		}
		return 2
	}
	switch cmd {
	case "compile", "scan":
		if len(rest) != 1 {
			fmt.Fprintf(stderr, "usage: filterc %s [flags] 'expression'\n", cmd)
			return 2
		}
		return runExpression(cmd, s, rest[0], stdout, stderr)
	case "serve":
		return runServe(s, stdout, stderr)
	}
	fmt.Fprint(stderr, usage)
	return 2
}

func runExpression(cmd string, s settings, expression string, stdout, stderr io.Writer) int {
	compiled, err := filterc.Compile(expression)
	if err != nil {
		reportCompileError(stderr, expression, err)
		return 1
	}
	for _, w := range compiled.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	req := compiled.Request(s.bounds(), s.Bounds.Limit)
	if cmd == "compile" {
		return writeJSON(stdout, stderr, req, s.compact)
	}
	if s.Golbat.URL == "" {
		fmt.Fprintln(stderr, "error: scan needs a golbat url (--golbat or [golbat] url in filterc.toml)")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := &golbat.Client{URL: s.Golbat.URL, Secret: s.Golbat.Secret}
	resp, err := client.ScanPokemon(ctx, req)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, resp, s.compact)
}

// reportCompileError prints the error and, for a positioned error, the
// offending line with a caret under the column.
func reportCompileError(stderr io.Writer, expression string, err error) {
	fmt.Fprintf(stderr, "error: %v\n", err)
	var ce *filterc.Error
	if !errors.As(err, &ce) || ce.Pos.Line < 1 {
		return
	}
	lines := strings.Split(expression, "\n")
	if ce.Pos.Line > len(lines) {
		return
	}
	fmt.Fprintf(stderr, "  %s\n  %s^\n", lines[ce.Pos.Line-1], strings.Repeat(" ", max(ce.Pos.Column-1, 0)))
}

func writeJSON(stdout, stderr io.Writer, v any, compact bool) int {
	enc := json.NewEncoder(stdout)
	if !compact {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
```

Until Task 9 adds `serve.go`, add this stub so the package builds, and replace it in Task 9:

```go
// cmd/filterc/serve.go (stub, replaced in Task 9)
package main

import (
	"fmt"
	"io"
)

func runServe(s settings, stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "serve: not implemented yet")
	return 2
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `gofmt -l . && go vet ./... && go test ./cmd/filterc/ && go run ./cmd/filterc compile 'iv == 100 || (pokemon == 1 && gender == 2)'`
Expected: tests PASS and the command prints the indented request from the spec's second example with zero bounds.

- [ ] **Step 6: Commit**

```bash
git add cmd/
git commit -m "feat: filterc CLI with compile and scan, TOML config"
```

---

### Task 9: `filterc serve` — `/compile`, `/scan`, `/healthz` — and the README

**Files:**
- Replace: `cmd/filterc/serve.go` (the Task 8 stub)
- Modify: `README.md`
- Test: `cmd/filterc/serve_test.go`

**Interfaces:**
- Consumes: `settings`, `Config.bounds()`, `filterc.Compile`, `Compiled.Request`, `golbat.Client`, `*golbat.StatusError`.
- Produces: `func newHandler(s settings, client *golbat.Client) http.Handler`; `func runServe(s settings, stdout, stderr io.Writer) int`.

- [ ] **Step 1: Write the failing test**

`cmd/filterc/serve_test.go`:

```go
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jfberry/golbat-filterc/golbat"
)

func testSettings() settings {
	s := settings{Config: defaultConfig()}
	s.Bounds.Min, s.Bounds.Max, s.Bounds.Limit = latLon{51.4, -0.2}, latLon{51.6, 0.1}, 300
	return s
}

func post(t *testing.T, h http.Handler, path, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: bad JSON %q", path, rec.Body.String())
		}
	}
	return rec.Code, out
}

func TestCompileEndpoint(t *testing.T) {
	h := newHandler(testSettings(), nil)
	code, out := post(t, h, "/compile", `{"expression": "iv == 100"}`)
	if code != 200 {
		t.Fatalf("status %d: %v", code, out)
	}
	req := out["request"].(map[string]any)
	if req["limit"] != float64(300) || req["min"].(map[string]any)["lat"] != 51.4 {
		t.Errorf("config bounds not applied: %v", req)
	}
	if w, ok := out["warnings"].([]any); !ok || len(w) != 0 {
		t.Errorf("warnings = %v", out["warnings"])
	}

	code, out = post(t, h, "/compile", `{"expression": "iv > 100", "bounds": {"min": {"lat": 1, "lon": 2}, "max": {"lat": 3, "lon": 4}}, "limit": 7}`)
	req = out["request"].(map[string]any)
	if code != 200 || req["limit"] != float64(7) || req["max"].(map[string]any)["lon"] != float64(4) || len(req["filters"].([]any)) != 0 {
		t.Errorf("override: %d %v", code, out)
	}
	if w := out["warnings"].([]any); len(w) != 1 {
		t.Errorf("warnings = %v", w)
	}
}

func TestCompileEndpointErrors(t *testing.T) {
	h := newHandler(testSettings(), nil)
	code, out := post(t, h, "/compile", `{"expression": "iv >= 90 && x == 1"}`)
	e := out["error"].(map[string]any)
	pos := e["position"].(map[string]any)
	if code != 400 || e["message"] != `unknown field "x"` || pos["line"] != float64(1) || pos["column"] != float64(13) {
		t.Errorf("%d %v", code, out)
	}
	if code, out := post(t, h, "/compile", `{"expression": `); code != 400 || out["error"] == nil {
		t.Errorf("bad JSON: %d %v", code, out)
	}
	if code, _ := post(t, h, "/compile", `{"expression": "`+strings.Repeat("x", 70<<10)+`"}`); code != 413 {
		t.Errorf("oversize body: %d", code)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/compile", nil))
	if rec.Code != 405 {
		t.Errorf("GET /compile = %d", rec.Code)
	}
}

func TestScanEndpoint(t *testing.T) {
	var gotBody string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, `{"pokemon":[{"id":"9"}],"examined":3,"skipped":0,"total":3,"limit_reached":false}`)
	}))
	defer fake.Close()

	h := newHandler(testSettings(), &golbat.Client{URL: fake.URL})
	code, out := post(t, h, "/scan", `{"expression": "size == 5 && pokemon != 710"}`)
	if code != 200 {
		t.Fatalf("status %d: %v", code, out)
	}
	if !strings.Contains(gotBody, `"filters":[{"size":{"min":5,"max":5}},{"pokemon":[{"id":710}],"iv":{"min":1,"max":0}}]`) {
		t.Errorf("golbat received %s", gotBody)
	}
	resp := out["response"].(map[string]any)
	if resp["examined"] != float64(3) || len(resp["pokemon"].([]any)) != 1 || out["request"] == nil {
		t.Errorf("response %v", out)
	}

	fake.Close()
	if code, out := post(t, h, "/scan", `{"expression": "iv == 100"}`); code != 502 || out["error"] == nil {
		t.Errorf("golbat down: %d %v", code, out)
	}
	if code, out := post(t, newHandler(testSettings(), nil), "/scan", `{"expression": "iv == 100"}`); code != 503 || out["error"] == nil {
		t.Errorf("no golbat configured: %d %v", code, out)
	}
}

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	newHandler(testSettings(), nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != 200 {
		t.Errorf("healthz = %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/filterc/`
Expected: FAIL — `undefined: newHandler`.

- [ ] **Step 3: Write the server**

`cmd/filterc/serve.go` (replacing the stub):

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/jfberry/golbat-filterc/filterc"
	"github.com/jfberry/golbat-filterc/golbat"
)

const maxBodyBytes = 64 << 10

type compileRequest struct {
	Expression string `json:"expression"`
	Bounds     *struct {
		Min filterc.LatLon `json:"min"`
		Max filterc.LatLon `json:"max"`
	} `json:"bounds"`
	Limit *int `json:"limit"`
}

type position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type errorResponse struct {
	Error struct {
		Message  string    `json:"message"`
		Position *position `json:"position,omitempty"`
	} `json:"error"`
}

type compileResponse struct {
	Request  filterc.ScanRequest  `json:"request"`
	Warnings []string             `json:"warnings"`
	Response *golbat.ScanResponse `json:"response,omitempty"`
}

type server struct {
	s      settings
	client *golbat.Client // nil when no Golbat URL is configured
}

// newHandler serves POST /compile, POST /scan and GET /healthz. There is
// no authentication: the server is a local tool, and its /scan carries the
// configured secret's scan capability.
func newHandler(s settings, client *golbat.Client) http.Handler {
	sv := &server{s: s, client: client}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("POST /compile", sv.compile)
	mux.HandleFunc("POST /scan", sv.scan)
	return mux
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string, pos *position) {
	var body errorResponse
	body.Error.Message, body.Error.Position = msg, pos
	writeJSONStatus(w, status, body)
}

// build reads and compiles the request; on failure it has written the
// error response and returns ok=false.
func (sv *server) build(w http.ResponseWriter, r *http.Request) (out compileResponse, ok bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var in compileRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body over 64 KiB", nil)
			return out, false
		}
		writeError(w, http.StatusBadRequest, "bad JSON: "+err.Error(), nil)
		return out, false
	}
	compiled, err := filterc.Compile(in.Expression)
	if err != nil {
		var ce *filterc.Error
		if errors.As(err, &ce) {
			writeError(w, http.StatusBadRequest, ce.Msg, &position{Line: ce.Pos.Line, Column: ce.Pos.Column})
		} else {
			writeError(w, http.StatusBadRequest, err.Error(), nil)
		}
		return out, false
	}
	bounds, limit := sv.s.bounds(), sv.s.Bounds.Limit
	if in.Bounds != nil {
		bounds = filterc.Bounds{Min: in.Bounds.Min, Max: in.Bounds.Max}
	}
	if in.Limit != nil {
		limit = *in.Limit
	}
	out.Request = compiled.Request(bounds, limit)
	out.Warnings = compiled.Warnings
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	return out, true
}

func (sv *server) compile(w http.ResponseWriter, r *http.Request) {
	out, ok := sv.build(w, r)
	if !ok {
		return
	}
	writeJSONStatus(w, http.StatusOK, out)
}

func (sv *server) scan(w http.ResponseWriter, r *http.Request) {
	if sv.client == nil {
		writeError(w, http.StatusServiceUnavailable, "no golbat url configured (--golbat or [golbat] url in filterc.toml)", nil)
		return
	}
	out, ok := sv.build(w, r)
	if !ok {
		return
	}
	resp, err := sv.client.ScanPokemon(r.Context(), out.Request)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error(), nil)
		return
	}
	out.Response = resp
	writeJSONStatus(w, http.StatusOK, out)
}

func runServe(s settings, stdout, stderr io.Writer) int {
	var client *golbat.Client
	if s.Golbat.URL != "" {
		client = &golbat.Client{URL: s.Golbat.URL, Secret: s.Golbat.Secret, HTTP: &http.Client{Timeout: 60 * time.Second}}
	}
	srv := &http.Server{Addr: s.Listen, Handler: newHandler(s, client), ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	fmt.Fprintf(stdout, "filterc listening on %s (golbat: %q)\n", s.Listen, s.Golbat.URL)
	select {
	case err := <-errc:
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
		return 0
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: all packages PASS.

- [ ] **Step 5: Update the README**

Replace the "Status: design stage" paragraph in `README.md` with usage:

```markdown
## Usage

```
go install github.com/jfberry/golbat-filterc/cmd/filterc@latest

filterc compile 'size == 5 && pokemon != 710'
filterc compile --min-lat 51.4 --min-lon -0.2 --max-lat 51.6 --max-lon 0.1 --limit 300 'iv == 100'
filterc scan --golbat http://127.0.0.1:9001 --secret … 'iv == 100 || (pokemon == 1 && gender == 2)'
filterc serve --listen :8080
```

`filterc.toml` (in the working directory or `$XDG_CONFIG_HOME/filterc/`)
holds the defaults; flags override it:

```toml
listen = ":8080"

[golbat]
url    = "http://127.0.0.1:9001"
secret = ""

[bounds]
min = { lat = 51.4, lon = -0.2 }
max = { lat = 51.6, lon = 0.1 }
limit = 300
```

The server has no authentication of its own; `/scan` runs scans with the
configured secret, so keep it on a private interface.

- `POST /compile` `{"expression": "…", "bounds"?: {"min", "max"}, "limit"?: n}` → `{"request": {…}, "warnings": […]}`
- `POST /scan` — the same, plus `"response"`: Golbat's reply
- `GET /healthz`

## The language

Fields: `pokemon`, `form`, `iv`, `atk`, `def`, `sta`, `level`, `cp`,
`gender`, `size`, `little`, `great`, `ultra`. Operators: `== != < <= > >=`,
`in [..]`, `not in`, `a..b` ranges, `&& || !` (or `and or not`), parentheses.
Integers only. A form literal needs a `pokemon` id in the same conjunction.

Semantics follow Golbat's matcher exactly, which the property test in
`filterc/property_test.go` checks: `-1` is a real value ("no encounter
data") for the encounter fields, so `!(iv >= 90)` includes un-encountered
pokemon; PvP ranks are absent for a pokemon with no PvP data (any comparison
on them is unknown, never true) and `4096` means "unranked in that league".

Design: [docs/superpowers/specs/2026-09-25-filter-expression-compiler-design.md](docs/superpowers/specs/2026-09-25-filter-expression-compiler-design.md).
Background: [UnownHash/Golbat#417](https://github.com/UnownHash/Golbat/issues/417).
```

- [ ] **Step 6: Commit**

```bash
git add cmd/filterc/serve.go cmd/filterc/serve_test.go README.md
git commit -m "feat: filterc serve with /compile, /scan and /healthz; README usage"
```

---

## Done when

- `go test ./...` passes, including the property test (≥ 1,000 expressions checked) and a 20 s fuzz run with no crashers.
- `filterc compile` reproduces every example in the spec.
- `filterc scan` and `POST /scan` return results from a running Golbat for `iv == 100` (manual check against a local instance; the e2e test in the spec's "Testing" section is a follow-up once a test Golbat with seeded pokemon exists).
