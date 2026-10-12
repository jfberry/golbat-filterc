# Phase 1 — model-driven front end (`internal/lang`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move lowering, interval sets, NNF, DNF/split, positions, errors and warnings out of `filterc` into `internal/lang`, driven by a `Model` (a field table), with the pokemon compiler moved onto it and its behaviour byte-identical.

**Architecture:** `internal/lang` knows nothing about pokemon: every place the front end consulted `domains[f]`, `isPvp`, `isSpecies` or the gender/species split rule now consults the model's `Field` (domain, kind, NULL group, id checks). `filterc` keeps the pokemon field table (`domains.go`, now also building `pokemonModel`), the form-needs-species check (`validate.go`), dispatch, the v3 types and `Compile`; thin unexported wrappers (`parse`, `nnf`, `toDNF`, `split`, `format`) keep its white-box tests reading as before. Phases 2–4 of the spec get their own plans, written against the code this phase produces. The `Field` here carries only what pokemon needs (`Kind` is `Range`/`List`/`Key`; no `Enum`, `Positive`, `Open` or scopes yet): the spec's fuller sketch is added by the phase that first uses each part, so nothing in this phase is untested.

**Tech Stack:** Go 1.24, `github.com/expr-lang/expr` (parser only), standard library.

**Spec:** `docs/superpowers/specs/2026-10-12-fort-filters-and-game-names-design.md` (section "Shared front end", and "Order of work" phase 1). Background design: `docs/superpowers/specs/2026-09-25-filter-expression-compiler-design.md`.

## Global Constraints

- No emitted JSON, error text, error position or warning text changes. No golden changes in this phase.
- `filterc`'s public API is unchanged: `Compile`, the four `With…` options and `Default…` constants, `Compiled`, `Clause`, `MinMax`, `PokemonId`, `LatLon`, `Bounds`, `ScanRequest`, `Error`, `Position` (the last two become type aliases of `internal/lang`'s, so `errors.As(err, &ce)` with `var ce *filterc.Error` keeps working).
- Every string literal in the test files of `filterc` at `main` still appears, at least as many times, in the test files of `filterc` + `internal/lang` afterwards (checked by the `litcheck` tool in Task 1). Moving a test is fine; changing an expectation is not. New tests may be added.
- `go test ./filterc/ -run MatchesReference -v` prints exactly `checked 4276 expressions (724 skipped; 1785 on the species×form grid)` (the property test is seeded; the count proves the same expressions compiled).
- `gofmt -l .` empty and `go vet ./...` clean on every commit.
- Dependencies stay `expr-lang/expr` and `BurntSushi/toml`; `internal/lang` imports only the standard library and `expr-lang/expr/{ast,file,parser}`.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

- **Shared backing arrays.** `Conjunction` holds `[MaxFields]IntervalSet` (slice headers) and is copied by value in `merge` and `Split`; a stage that wrote into a set in place would corrupt a sibling conjunction. Expected: merging or splitting never changes its inputs. Pinned by `TestMergeAndSplitLeaveInputsAlone` (Task 2).
- **Complement over a domain with gaps.** Pokemon domains are single intervals, but phase 3's enum domains are not; `Complement(dom IntervalSet)` must return exactly `dom` minus the set, including when the set lies in a gap or outside `dom`. Pinned by the new `TestComplementGappyDomain` cases (Task 1).
- **Malformed models.** A model with duplicate field names, an empty domain or more than `MaxFields` fields must fail at construction, not mis-lower later. Pinned by `TestNewModelRejects` (Task 2).
- **Allocation ceilings.** `Conjunction` grows from ~330 to ~800 bytes (fixed `MaxFields = 16` arrays); the existing ceilings (`TestSplitChecksCapBeforeBuilding` 8 MiB, `TestKeyCapRefusesBeforeBuilding` 8 MiB, `TestSpeciesRangesRefuseBeforeEnumerating` 16 MiB, the parse allocation tests) must still pass unchanged. Run them explicitly in Task 3.
- **Typed-nil errors.** `checkIds` returns `error`; it must return a literal `nil`, never a nil `*Error`, or every comparison on `pokemon`/`form` would fail. Pinned by the existing goldens (`pokemon == 1` compiles) — keep the signature `error` and `return nil`.

---

## File structure after this phase

```
internal/lang/
  errors.go        Position, Error, Errorf, Warnings (moved from filterc/errors.go)
  intervals.go     Interval, IntervalSet and its methods (moved from filterc/intervals.go; complement takes a set domain)
  intervals_test.go
  model.go         FieldID, MaxFields, Kind, NullGroup, Field, Model, NewModel
  model_test.go
  ast.go           Node, And, Or, Not, Lit, Format (moved from filterc/ast.go)
  lower.go         Lower and the whitelist walk (moved from filterc/parse.go)
  lower_test.go    TestPosTableMatchesBind (moved from filterc/parse_test.go)
  nnf.go           NNF (moved from filterc/nnf.go)
  dnf.go           Conjunction, ToDNF, Split, TooMany (moved from filterc/dnf.go)
  dnf_test.go      TestMergeAndSplitLeaveInputsAlone
filterc/
  domains.go       pokemon field ids, names, domains (unchanged table and comments), pvpNull, pokemonModel
  errors.go        aliases Error/Position, errorf wrapper
  stages.go        unexported aliases and wrappers over internal/lang for the compiler and tests
  validate.go      validate (moved from filterc/dnf.go)
  dispatch.go      + smallSide, smallSideNegative (moved from filterc/intervals.go); renames
  compile.go, types.go
  (deleted: ast.go, parse.go, nnf.go, dnf.go, intervals.go)
```

---

### Task 1: `internal/lang` errors, warnings and interval sets

**Files:**
- Create: `internal/lang/errors.go`, `internal/lang/intervals.go`, `internal/lang/intervals_test.go`, `filterc/stages.go`
- Modify: `filterc/errors.go` (becomes aliases), `filterc/dispatch.go`, `filterc/dnf.go`, `filterc/parse.go`, `filterc/nnf.go`, `filterc/domains.go`, `filterc/compile.go`, `filterc/intervals_test.go`, `filterc/warnings_test.go`, `filterc/property_test.go`, `filterc/domains_test.go`
- Delete: `filterc/intervals.go`

**Interfaces:**
- Consumes: nothing.
- Produces (used by Task 2 and by phases 2–4):
  - `lang.Position{Line, Column, Offset int}`, `lang.Error{Msg string; Pos Position}` with `Error() string`, `lang.Errorf(pos Position, format string, args ...any) *Error`
  - `lang.Warnings` with `Add(pos Position, format string, args ...any)`, `List() []string`, field `Negated *NullGroup`
  - `lang.NullGroup{Whole, Negated, Note string}` — declared in `errors.go` in this task because `Warnings` needs it; Task 2 moves the declaration, unchanged, to `model.go`
  - `lang.Interval{Lo, Hi int}`, `lang.IntervalSet []Interval`, `lang.SetOf(ivs ...Interval) IntervalSet`, methods `Intersect(IntervalSet) IntervalSet`, `Complement(dom IntervalSet) IntervalSet`, `Values() []int`, `Equal(IntervalSet) bool`, `Size() int`, `Contains(int) bool`, `Lo() int`, `Hi() int`
  - in `filterc`: `type interval = lang.Interval`, `type intervalSet = lang.IntervalSet`, `type warnings = lang.Warnings`, `func setOf(ivs ...interval) intervalSet`, `type Error = lang.Error`, `type Position = lang.Position`, `func errorf(pos Position, format string, args ...any) *Error`

- [ ] **Step 1: Record the baseline and build the literal checker**

Run from the repo root:

```bash
rm -rf /tmp/filterc-base && mkdir -p /tmp/filterc-base /tmp/litcheck
git archive main filterc | tar -x -C /tmp/filterc-base
go test ./filterc/ -run MatchesReference -v 2>&1 | grep checked > /tmp/filterc-base/checked.txt
cat /tmp/filterc-base/checked.txt
```

Expected: `    property_test.go:280: checked 4276 expressions (724 skipped; 1785 on the species×form grid)`

Write `/tmp/litcheck/main.go` (a throwaway tool, not committed):

```go
// litcheck reports string literals of the base test files that no longer
// appear, as many times, in the test files of the given directories.
package main

import (
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
)

func lits(paths []string) map[string]int {
	out := map[string]int{}
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			panic(err)
		}
		var s scanner.Scanner
		fset := token.NewFileSet()
		s.Init(fset.AddFile(p, -1, len(src)), src, nil, 0)
		for {
			_, tok, lit := s.Scan()
			if tok == token.EOF {
				break
			}
			if tok == token.STRING {
				out[lit]++
			}
		}
	}
	return out
}

func main() {
	base, _ := filepath.Glob(filepath.Join(os.Args[1], "*_test.go"))
	var now []string
	for _, d := range os.Args[2:] {
		m, _ := filepath.Glob(filepath.Join(d, "*_test.go"))
		now = append(now, m...)
	}
	b, n := lits(base), lits(now)
	missing := 0
	for lit, c := range b {
		if n[lit] < c {
			fmt.Printf("%d× → %d×  %s\n", c, n[lit], lit)
			missing++
		}
	}
	if missing > 0 {
		os.Exit(1)
	}
	fmt.Printf("ok: all %d distinct literals of the base tests are still present\n", len(b))
}
```

Run: `go run /tmp/litcheck/main.go /tmp/filterc-base/filterc ./filterc`
Expected: `ok: all N distinct literals of the base tests are still present` (sanity check against an unchanged tree).

- [ ] **Step 2: Write the failing `internal/lang` interval tests**

Create `internal/lang/intervals_test.go`. Move `TestSetOfNormalises`, `TestIntersect`, `TestComplement`, `TestValues` and `TestSizeAndContains` here from `filterc/intervals_test.go`, renamed mechanically (`setOf`→`SetOf`, `interval`→`Interval`, `intervalSet`→`IntervalSet`, `.intersect`→`.Intersect`, `.complement(dom)`→`.Complement(dom)`, `.equal`→`.Equal`, `.values`→`.Values`, `.size`→`.Size`, `.contains`→`.Contains`). **Keep every string literal byte-identical**, including `t.Errorf` messages such as `"setOf = %v, want %v"` and `"intersect = %v, want %v"`. In `TestComplement`, `dom := interval{-1, 100}` becomes `dom := IntervalSet{{-1, 100}}` and `setOf(dom)` becomes `dom`. Then add, at the end of the file:

```go
// A domain with gaps (phase 3's enum domains): the complement is dom minus
// the set, whether the set lies inside dom, in a gap, or outside it.
func TestComplementGappyDomain(t *testing.T) {
	dom := IntervalSet{{1, 5}, {10, 12}}
	cases := []struct{ in, want IntervalSet }{
		{SetOf(Interval{3, 3}), IntervalSet{{1, 2}, {4, 5}, {10, 12}}},
		{SetOf(), dom},
		{SetOf(Interval{0, 20}), nil},
		{SetOf(Interval{6, 9}), dom},
		{SetOf(Interval{5, 10}), IntervalSet{{1, 4}, {11, 12}}},
	}
	for _, c := range cases {
		if got := c.in.Complement(dom); !got.Equal(c.want) {
			t.Errorf("%v.Complement(%v) = %v, want %v", c.in, dom, got, c.want)
		}
	}
	if got := SetOf(Interval{1, 1}).Complement(nil); len(got) != 0 {
		t.Errorf("complement within an empty domain = %v, want empty", got)
	}
}

func TestLoHi(t *testing.T) {
	s := IntervalSet{{1, 5}, {10, 12}}
	if s.Lo() != 1 || s.Hi() != 12 {
		t.Errorf("Lo, Hi = %d, %d, want 1, 12", s.Lo(), s.Hi())
	}
}
```

The file starts with:

```go
package lang

import (
	"slices"
	"testing"
)
```

(drop `slices` if no moved test uses it after the move; `go vet` will say.)

`filterc/intervals_test.go` keeps only `TestSmallSide`, with `.equal` → `.Equal` and its imports trimmed to `testing`.

- [ ] **Step 3: Run the new tests to verify they fail**

Run: `go test ./internal/lang/`
Expected: FAIL — build errors, `undefined: SetOf`, `undefined: Interval`, …

- [ ] **Step 4: Write `internal/lang/errors.go`**

```go
// Package lang is the model-driven front end shared by every filter
// language: Expr parsing and the whitelist walk, interval sets, negation
// normal form, DNF and split, positioned errors and the warnings
// collector. It knows nothing about any one model; a Model describes the
// fields.
package lang

import (
	"fmt"
	"slices"
)

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

// Errorf builds an Error at pos.
func Errorf(pos Position, format string, args ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, args...), Pos: pos}
}

// warning is a diagnostic about one literal: it compiles, but probably not
// to what the author meant.
type warning struct {
	pos  Position
	text string
}

// Warnings collects diagnostics through lowering and NNF. A nil collector
// discards them, for the stage tests that do not care.
type Warnings struct {
	items []warning
	// Negated is the NULL group of the first complemented NULL-able
	// literal warned about, so the "can never hold" warning can point at
	// it; nil when there was none.
	Negated *NullGroup
}

// Add records a warning at pos.
func (w *Warnings) Add(pos Position, format string, args ...any) {
	if w == nil {
		return
	}
	w.items = append(w.items, warning{pos: pos, text: fmt.Sprintf(format, args...)})
}

// List formats the warnings like errors (line:column: message), in source
// order. Repeats of the same formatted text (position included) are
// dropped: a guard against one literal being warned twice, not a feature;
// two literals with the same message keep a caret each.
func (w *Warnings) List() []string {
	if w == nil || len(w.items) == 0 {
		return nil
	}
	items := slices.Clone(w.items)
	slices.SortStableFunc(items, func(a, b warning) int { return a.pos.Offset - b.pos.Offset })
	out := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		s := (&Error{Msg: it.text, Pos: it.pos}).Error()
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// NullGroup describes fields that are NULL (absent) for some rows: every
// condition on such a field is UNKNOWN for them, negated or not. Task 2
// moves this declaration to model.go.
type NullGroup struct {
	// Whole is the verdict for a literal covering the whole domain, which
	// is not a no-op: it excludes the rows where the field is NULL.
	Whole string
	// Negated is appended to the warning on a complemented literal.
	Negated string
	// Note is appended to "can never hold" once such a literal was warned.
	Note string
}
```

- [ ] **Step 5: Write `internal/lang/intervals.go`**

```go
package lang

import "slices"

// Interval is a closed integer interval, Lo <= Hi.
type Interval struct{ Lo, Hi int }

// IntervalSet is a union of closed intervals, kept sorted, disjoint and
// with adjacent intervals merged, so two sets with the same members are
// equal as slices.
type IntervalSet []Interval

// SetOf builds a normalised set; empty intervals (Lo > Hi) are ignored.
func SetOf(ivs ...Interval) IntervalSet {
	var s IntervalSet
	for _, iv := range ivs {
		if iv.Lo <= iv.Hi {
			s = append(s, iv)
		}
	}
	slices.SortFunc(s, func(a, b Interval) int { return a.Lo - b.Lo })
	out := s[:0]
	for _, iv := range s {
		if n := len(out); n > 0 && iv.Lo <= out[n-1].Hi+1 {
			out[n-1].Hi = max(out[n-1].Hi, iv.Hi)
			continue
		}
		out = append(out, iv)
	}
	return out
}

func (s IntervalSet) Intersect(t IntervalSet) IntervalSet {
	var out IntervalSet
	for _, a := range s {
		for _, b := range t {
			lo, hi := max(a.Lo, b.Lo), min(a.Hi, b.Hi)
			if lo <= hi {
				out = append(out, Interval{lo, hi})
			}
		}
	}
	return SetOf(out...)
}

// Complement returns dom minus s. dom may have gaps; for a single-interval
// dom this is the complement within that interval.
func (s IntervalSet) Complement(dom IntervalSet) IntervalSet {
	if len(dom) == 0 {
		return nil
	}
	span := Interval{dom.Lo(), dom.Hi()}
	var out IntervalSet
	next := span.Lo
	for _, iv := range s.Intersect(IntervalSet{span}) {
		if iv.Lo > next {
			out = append(out, Interval{next, iv.Lo - 1})
		}
		next = iv.Hi + 1
	}
	if next <= span.Hi {
		out = append(out, Interval{next, span.Hi})
	}
	if len(dom) == 1 {
		return SetOf(out...)
	}
	return SetOf(out...).Intersect(dom)
}

// Values enumerates the members, for list fields (small sets).
func (s IntervalSet) Values() []int {
	out := make([]int, 0, s.Size())
	for _, iv := range s {
		for v := iv.Lo; v <= iv.Hi; v++ {
			out = append(out, v)
		}
	}
	return out
}

func (s IntervalSet) Equal(t IntervalSet) bool { return slices.Equal(s, t) }

// Size is the number of members, computed from the intervals.
func (s IntervalSet) Size() int {
	n := 0
	for _, iv := range s {
		n += iv.Hi - iv.Lo + 1
	}
	return n
}

// Contains reports whether v is a member, by binary search.
func (s IntervalSet) Contains(v int) bool {
	i, _ := slices.BinarySearchFunc(s, v, func(iv Interval, v int) int {
		switch {
		case iv.Hi < v:
			return -1
		case iv.Lo > v:
			return 1
		}
		return 0
	})
	return i < len(s) && s[i].Lo <= v && v <= s[i].Hi
}

// Lo is the smallest member; s must not be empty.
func (s IntervalSet) Lo() int { return s[0].Lo }

// Hi is the largest member; s must not be empty.
func (s IntervalSet) Hi() int { return s[len(s)-1].Hi }
```

- [ ] **Step 6: Run the `internal/lang` tests**

Run: `go test ./internal/lang/ -v`
Expected: PASS for `TestSetOfNormalises`, `TestIntersect`, `TestComplement`, `TestValues`, `TestSizeAndContains`, `TestComplementGappyDomain`, `TestLoHi`.

- [ ] **Step 7: Point `filterc` at the new types**

Replace `filterc/errors.go` with:

```go
package filterc

import "github.com/jfberry/golbat-filterc/internal/lang"

// Position locates an error in the expression source. Line and Column are
// 1-based; Offset is the byte offset.
type Position = lang.Position

// Error is a compile error at a position in the expression.
type Error = lang.Error

func errorf(pos Position, format string, args ...any) *Error {
	return lang.Errorf(pos, format, args...)
}
```

Create `filterc/stages.go`:

```go
package filterc

import "github.com/jfberry/golbat-filterc/internal/lang"

// The compiler's stages live in internal/lang; these names keep filterc's
// code and its white-box tests reading as they did.
type (
	interval    = lang.Interval
	intervalSet = lang.IntervalSet
	warnings    = lang.Warnings
)

func setOf(ivs ...interval) intervalSet { return lang.SetOf(ivs...) }
```

Delete `filterc/intervals.go`, and move its last two functions to the end of `filterc/dispatch.go`, rewritten against the exported names:

```go
// smallSideNegative reports whether the small side of s over f's domain is
// its complement: s holds more than half the domain (a tie keeps s).
func smallSideNegative(f field, s intervalSet) bool {
	d := domains[f]
	n := s.Size()
	return n > d.Hi-d.Lo+1-n
}

// smallSide returns the smaller of s and its complement over f's domain,
// and whether it is the complement.
func smallSide(f field, s intervalSet) (intervalSet, bool) {
	if smallSideNegative(f, s) {
		return s.Complement(intervalSet{domains[f]}), true
	}
	return s, false
}
```

In `filterc/domains.go`, delete the `interval` struct declaration (`type interval struct{ lo, hi int }` and its comment); the `domains` table keeps its literal `{1, 32767}` entries, which now build `lang.Interval`s through the alias.

Then apply these renames in every non-test file still in `filterc` (`parse.go`, `nnf.go`, `dnf.go`, `dispatch.go`, `compile.go`, `domains.go`) — the compiler tells you every site:

| old | new |
|---|---|
| `x.lo`, `x.hi` on an `interval` | `x.Lo`, `x.Hi` |
| `.intersect(` | `.Intersect(` |
| `.complement(domains[f])` (an interval) | `.Complement(intervalSet{domains[f]})` |
| `.equal(` / `.size()` / `.contains(` / `.values()` | `.Equal(` / `.Size()` / `.Contains(` / `.Values()` |
| `w.add(` / `w.list()` | `w.Add(` / `w.List()` |
| `w.pvpNegated = true` (in `nnf.go`) | `if w.Negated == nil { w.Negated = pvpNull }` |
| `if w.pvpNegated {` (in `compile.go`) | `if w.Negated != nil {` |

and add to `filterc/domains.go` (it is used by the two rows above; Task 2 also uses it):

```go
// pvpNull describes the PvP ranks: absent for a pokemon without PvP data,
// for which Golbat fails every PvP condition.
var pvpNull = &lang.NullGroup{
	Whole:   "holds for every pokemon with PvP data and excludes pokemon without PvP data",
	Negated: "never matches a pokemon without PvP data; a pokemon with no PvP data fails every PvP comparison, negated or not",
	Note:    "note that negated PvP conditions never match pokemon without PvP data",
}
```

with `import "github.com/jfberry/golbat-filterc/internal/lang"` at the top of `domains.go`.

Interval literals written `interval{a, b}` in `parse.go`/`dnf.go` keep compiling (positional literal through the alias).

- [ ] **Step 8: Update the `filterc` tests mechanically**

- `filterc/warnings_test.go` (`TestWarningsDedupeInSourceOrder`): `w.add(` → `w.Add(`, `w.list()` → `w.List()`, `none.add(` → `none.Add(`, `none.list()` → `none.List()`.
- `filterc/property_test.go`: `domains[…].lo` → `.Lo`, `domains[…].hi` → `.Hi` (lines 120, 165, 194, 222, 238, 239).
- `filterc/domains_test.go`: no change needed in this task (its `map[field]interval` builds `lang.Interval`s).

- [ ] **Step 9: Run everything**

```bash
gofmt -l . ; go vet ./... && go test ./... && go test ./filterc/ -run MatchesReference -v 2>&1 | grep checked | diff - /tmp/filterc-base/checked.txt && go run /tmp/litcheck/main.go /tmp/filterc-base/filterc ./filterc ./internal/lang
```

Expected: no gofmt output, vet clean, all packages `ok`, empty diff, and `ok: all N distinct literals of the base tests are still present`.

- [ ] **Step 10: Commit**

```bash
git add internal/lang filterc
git commit -m "refactor: interval sets, errors and warnings move to internal/lang

No behaviour change: no golden, warning or error text changes; the
property test checks the same 4276 expressions. Complement now takes a
set domain (gaps allowed), which pokemon's single-interval domains
reduce to the old behaviour; new tests pin the gappy case.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: the model, lowering, NNF and DNF move to `internal/lang`

**Files:**
- Create: `internal/lang/model.go`, `internal/lang/model_test.go`, `internal/lang/ast.go`, `internal/lang/lower.go`, `internal/lang/lower_test.go`, `internal/lang/nnf.go`, `internal/lang/dnf.go`, `internal/lang/dnf_test.go`, `filterc/validate.go`
- Modify: `internal/lang/errors.go` (drop `NullGroup`, now in `model.go`), `filterc/domains.go`, `filterc/stages.go`, `filterc/dispatch.go`, `filterc/compile.go`, `filterc/domains_test.go`, `filterc/parse_test.go`
- Delete: `filterc/ast.go`, `filterc/parse.go`, `filterc/nnf.go`, `filterc/dnf.go`

**Interfaces:**
- Consumes (Task 1): `lang.Position`, `lang.Error`, `lang.Errorf`, `lang.Warnings` (`Add`, `List`, `Negated`), `lang.NullGroup`, `lang.Interval`, `lang.IntervalSet` and methods; `filterc`'s `interval`, `intervalSet`, `warnings`, `setOf`, `errorf`, `pvpNull`.
- Produces (used by phases 2–4):
  - `lang.FieldID` (`uint8`), `lang.MaxFields = 16`, `lang.Kind` with `lang.Range`, `lang.List`, `lang.Key`
  - `lang.Field{Name string; Kind Kind; Domain IntervalSet; Null *NullGroup; IDs bool; BadID func(v int) string; Below string}`
  - `lang.NewModel(name string, fields []Field) *Model`; `(*Model).Field(FieldID) *Field`, `(*Model).Lookup(string) (FieldID, bool)`, `(*Model).Len() int`; exported `Model.Name`
  - `lang.Node`; `*lang.And{L, R Node}`, `*lang.Or{L, R Node}`, `*lang.Not{X Node}`, `*lang.Lit{F FieldID; Set IntervalSet; Pos Position; Src string; Neg bool}`; `lang.Format(m *Model, n Node) string`
  - `lang.Lower(m *Model, src string, w *Warnings) (Node, error)`
  - `lang.NNF(m *Model, n Node, w *Warnings) Node`
  - `lang.Conjunction{Has [MaxFields]bool; Sets [MaxFields]IntervalSet; At [MaxFields]Position}` (plus an unexported model pointer) with `String() string`; `lang.ToDNF(m *Model, n Node, maxConj int) ([]Conjunction, error)`; `lang.Split(conjs []Conjunction, maxConj int) ([]Conjunction, error)`; `lang.TooMany(limit int) *Error`
  - in `filterc`: `type field = lang.FieldID`; `isSpecies(f field) bool`, `isPvp(f field) bool`; `pokemonModel *lang.Model`; wrappers `parse`, `lower`, `nnf`, `toDNF`, `split`, `format`; `type conjunction = lang.Conjunction`; `validate(conjs []conjunction) error`

- [ ] **Step 1: Write the failing model and stage tests**

Create `internal/lang/model_test.go`:

```go
package lang

import (
	"strings"
	"testing"
)

func testModel() *Model {
	return NewModel("test", []Field{
		{Name: "a", Kind: Range, Domain: IntervalSet{{0, 10}}},
		{Name: "b", Kind: List, Domain: IntervalSet{{1, 3}}},
	})
}

func TestModelLookup(t *testing.T) {
	m := testModel()
	if f, ok := m.Lookup("b"); !ok || f != 1 || m.Field(f).Name != "b" {
		t.Errorf("Lookup(b) = %d, %v", f, ok)
	}
	if _, ok := m.Lookup("c"); ok {
		t.Error("Lookup(c) must fail")
	}
	if m.Len() != 2 || m.Name != "test" {
		t.Errorf("Len, Name = %d, %q", m.Len(), m.Name)
	}
}

func TestNewModelRejects(t *testing.T) {
	many := make([]Field, MaxFields+1)
	for i := range many {
		many[i] = Field{Name: strings.Repeat("x", i+1), Domain: IntervalSet{{0, 1}}}
	}
	cases := map[string][]Field{
		"duplicate":    {{Name: "a", Domain: IntervalSet{{0, 1}}}, {Name: "a", Domain: IntervalSet{{0, 1}}}},
		"empty domain": {{Name: "a"}},
		"too many":     many,
	}
	for name, fields := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: NewModel did not panic", name)
				}
			}()
			NewModel("bad", fields)
		}()
	}
}
```

Create `internal/lang/dnf_test.go`:

```go
package lang

import "testing"

// Conjunctions are copied by value but hold slice headers; merging and
// splitting must build new sets, never write into an input's.
func TestMergeAndSplitLeaveInputsAlone(t *testing.T) {
	m := testModel()
	n, err := Lower(m, "(a in 0..5 || a == 9) && b in [1, 2]", nil)
	if err != nil {
		t.Fatal(err)
	}
	conjs, err := ToDNF(m, NNF(m, n, nil), 16)
	if err != nil {
		t.Fatal(err)
	}
	before := make([]string, len(conjs))
	for i, c := range conjs {
		before[i] = c.String()
	}
	a, _ := fromLit(m, &Lit{F: 0, Set: IntervalSet{{0, 10}}})
	b, _ := fromLit(m, &Lit{F: 0, Set: IntervalSet{{3, 4}}})
	if c, ok := merge(a, b); !ok || c.String() != "a[3,4]" || a.String() != "a[0,10]" || b.String() != "a[3,4]" {
		t.Errorf("merge: %s from %s and %s", c, a, b)
	}
	wide, _ := fromLit(m, &Lit{F: 0, Set: IntervalSet{{0, 1}, {5, 6}}})
	parts, err := Split([]Conjunction{wide}, 16)
	if err != nil || len(parts) != 2 || wide.String() != "a[0,1][5,6]" {
		t.Errorf("split: %v %v, input now %s", parts, err, wide)
	}
	for i, c := range conjs {
		if c.String() != before[i] {
			t.Errorf("conjunction %d changed: %s, was %s", i, c, before[i])
		}
	}
	if len(conjs) != 2 || conjs[0].String() != "a[0,5] b[1,2]" || conjs[1].String() != "a[9,9] b[1,2]" {
		t.Errorf("DNF = %v", conjs)
	}
}
```

Create `internal/lang/lower_test.go` by moving `TestPosTableMatchesBind` verbatim from `filterc/parse_test.go` (package `lang`; imports `testing` and `github.com/expr-lang/expr/file`). Remove it from `filterc/parse_test.go` and drop that file's `file` import if nothing else uses it.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/lang/`
Expected: FAIL — `undefined: NewModel`, `undefined: Lower`, `undefined: newPosTable`, …

- [ ] **Step 3: Write `internal/lang/model.go`** (and delete the `NullGroup` declaration from `errors.go`)

```go
package lang

import "fmt"

// FieldID identifies a field of a Model: an index into its field table.
type FieldID uint8

// MaxFields bounds a model's field table, so a Conjunction is a value
// (fixed arrays) and copying one never shares state.
const MaxFields = 16

// Kind says how a field's set reaches the wire, which decides whether
// split must break it into one interval per conjunction.
type Kind uint8

const (
	Range Kind = iota // one {min, max} per clause: split into one conjunction per interval
	List              // a list of values: not split
	Key               // selects a scan group (pokemon, form): not split
)

// NullGroup describes fields that are NULL (absent) for some rows: every
// condition on such a field is UNKNOWN for them, negated or not.
type NullGroup struct {
	// Whole is the verdict for a literal covering the whole domain, which
	// is not a no-op: it excludes the rows where the field is NULL.
	Whole string
	// Negated is appended to the warning on a complemented literal.
	Negated string
	// Note is appended to "can never hold" once such a literal was warned.
	Note string
}

// Field is one comparable field of a model.
type Field struct {
	Name   string
	Kind   Kind
	Domain IntervalSet // the values the field can hold; complements are taken within it
	Null   *NullGroup  // nil when the field always has a value
	// IDs makes explicit values (==, !=, list members) outside the domain
	// an error at the value instead of a warning; ranges and ordered
	// comparisons are still clipped.
	IDs bool
	// BadID, with IDs, may give the message for a value outside the
	// domain; "" falls back to "<name> <v> is out of range <lo>..<hi>".
	BadID func(v int) string
	// Below, when set, is how a range that starts below the domain and
	// reaches into it is reported ("species ids start at 1"), instead of
	// as a clipped range.
	Below string
}

// Model is one filter language's field table.
type Model struct {
	Name   string
	fields []Field
	byName map[string]FieldID
}

// NewModel builds a model; it panics on a table that cannot be right
// (more than MaxFields fields, a repeated name, an empty domain), since
// models are package-level tables checked by their tests.
func NewModel(name string, fields []Field) *Model {
	if len(fields) > MaxFields {
		panic(fmt.Sprintf("lang: model %s has %d fields, more than %d", name, len(fields), MaxFields))
	}
	m := &Model{Name: name, fields: fields, byName: make(map[string]FieldID, len(fields))}
	for i, f := range fields {
		if _, dup := m.byName[f.Name]; dup {
			panic(fmt.Sprintf("lang: model %s repeats field %q", name, f.Name))
		}
		if len(f.Domain) == 0 {
			panic(fmt.Sprintf("lang: model %s field %q has an empty domain", name, f.Name))
		}
		m.byName[f.Name] = FieldID(i)
	}
	return m
}

// Field returns the field with id f.
func (m *Model) Field(f FieldID) *Field { return &m.fields[f] }

// Lookup finds a field by name.
func (m *Model) Lookup(name string) (FieldID, bool) {
	f, ok := m.byName[name]
	return f, ok
}

// Len is the number of fields.
func (m *Model) Len() int { return len(m.fields) }
```

- [ ] **Step 4: Write `internal/lang/ast.go`**

```go
package lang

import (
	"fmt"
	"strings"
)

// Node is the boolean tree an expression lowers to: and/or/not over
// literals. The only literal is a field with an interval set over the
// field's domain.
type Node interface{ isNode() }

type And struct{ L, R Node }
type Or struct{ L, R Node }
type Not struct{ X Node }

// Lit constrains a field to a set of values within its domain.
type Lit struct {
	F   FieldID
	Set IntervalSet
	Pos Position
	// Src is the literal as written (field op value, value op field,
	// field [not] in [...] or lo..hi), for warnings; Neg is true when the
	// author wrote it in a negative form (!= or not in), so Set is already
	// a complement.
	Src string
	Neg bool
}

func (*And) isNode() {}
func (*Or) isNode()  {}
func (*Not) isNode() {}
func (*Lit) isNode() {}

// Format renders a node for tests and debugging: and(a,b), or(a,b),
// not(a), iv{[90,100]}, pokemon{[1,1],[4,4]}.
func Format(m *Model, n Node) string {
	switch v := n.(type) {
	case *And:
		return "and(" + Format(m, v.L) + "," + Format(m, v.R) + ")"
	case *Or:
		return "or(" + Format(m, v.L) + "," + Format(m, v.R) + ")"
	case *Not:
		return "not(" + Format(m, v.X) + ")"
	case *Lit:
		parts := make([]string, len(v.Set))
		for i, iv := range v.Set {
			parts[i] = fmt.Sprintf("[%d,%d]", iv.Lo, iv.Hi)
		}
		return m.Field(v.F).Name + "{" + strings.Join(parts, ",") + "}"
	}
	return fmt.Sprintf("<%T>", n)
}
```

- [ ] **Step 5: Write `internal/lang/lower.go`**

This is `filterc/parse.go` with the model in place of the package tables. Messages are unchanged character for character; bounds still compare with `<`/`>` against the domain's `Lo()`/`Hi()` (not `Contains`), so behaviour on single-interval domains is identical — phase 3 revisits gappy domains deliberately.

```go
package lang

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/file"
	"github.com/expr-lang/expr/parser"
)

const (
	maxExpressionBytes = 64 << 10
	maxLiteral         = 1 << 20 // keeps v±1 arithmetic far from overflow
)

// Lower turns expression source into the boolean tree over m's fields,
// using Expr's parser and accepting only the subset the compiler
// understands. The whitelist walk is the type check: every accepted node
// is a comparison between a field of m and an integer, or and/or/not over
// such comparisons. Warnings go to w, which may be nil.
func Lower(m *Model, src string, w *Warnings) (Node, error) {
	if len(src) > maxExpressionBytes {
		return nil, Errorf(Position{Line: 1, Column: 1}, "expression longer than %d bytes", maxExpressionBytes)
	}
	if strings.TrimSpace(src) == "" {
		return nil, Errorf(Position{Line: 1, Column: 1}, "empty expression")
	}
	t := newPosTable(src)
	tree, err := parser.Parse(src)
	if err != nil {
		if fe, ok := err.(*file.Error); ok {
			return nil, &Error{Msg: fe.Message, Pos: t.at(fe.From)}
		}
		return nil, &Error{Msg: err.Error(), Pos: Position{Line: 1, Column: 1}}
	}
	l := lowerer{m: m, t: t, w: w, src: src}
	return l.boolean(tree.Node)
}

// posTable converts Expr's rune offsets to positions without re-scanning
// the source: built once per parse, each lookup is a binary search over
// the line starts.
type posTable struct {
	runeToByte []int // byte offset of each rune index, plus one past the end
	lineStarts []int // rune index of each line start
}

func newPosTable(src string) posTable {
	t := posTable{runeToByte: make([]int, 0, len(src)+1), lineStarts: []int{0}}
	for i, r := range src {
		t.runeToByte = append(t.runeToByte, i)
		if r == '\n' {
			t.lineStarts = append(t.lineStarts, len(t.runeToByte))
		}
	}
	t.runeToByte = append(t.runeToByte, len(src))
	return t
}

// at is the position of rune offset from, clamped to the source.
func (t posTable) at(from int) Position {
	from = min(max(from, 0), len(t.runeToByte)-1)
	line, found := slices.BinarySearch(t.lineStarts, from)
	if !found {
		line--
	}
	return Position{Line: line + 1, Column: from - t.lineStarts[line] + 1, Offset: t.runeToByte[from]}
}

type lowerer struct {
	m   *Model
	t   posTable
	w   *Warnings
	src string
}

func (l *lowerer) pos(n ast.Node) Position { return l.t.at(n.Location().From) }

func (l *lowerer) boolean(n ast.Node) (Node, error) {
	switch v := n.(type) {
	case *ast.BinaryNode:
		switch v.Operator {
		case "and", "&&":
			return l.pair(v, func(a, b Node) Node { return &And{a, b} })
		case "or", "||":
			return l.pair(v, func(a, b Node) Node { return &Or{a, b} })
		case "==", "!=", "<", "<=", ">", ">=":
			return l.comparison(v)
		case "in":
			return l.membership(v, false)
		}
		return nil, Errorf(l.pos(n), "unsupported operator %q", v.Operator)
	case *ast.UnaryNode:
		if v.Operator != "not" && v.Operator != "!" {
			return nil, Errorf(l.pos(n), "unsupported operator %q", v.Operator)
		}
		if !isBoolean(v.Node) {
			return nil, Errorf(l.pos(n), "%s applies to a comparison; write %s (…)", v.Operator, v.Operator)
		}
		// `x not in y` parses as not(x in y) with the not after x;
		// `not (x in y)` has it before
		if in, ok := v.Node.(*ast.BinaryNode); ok && in.Operator == "in" && v.Location().From > in.Left.Location().From {
			return l.membership(in, true)
		}
		x, err := l.boolean(v.Node)
		if err != nil {
			return nil, err
		}
		return &Not{x}, nil
	case *ast.IdentifierNode, *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode, *ast.StringNode:
		return nil, Errorf(l.pos(n), "expected a comparison")
	}
	return nil, Errorf(l.pos(n), "unsupported construct %s", n.String())
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

func (l *lowerer) pair(v *ast.BinaryNode, mk func(a, b Node) Node) (Node, error) {
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
func (l *lowerer) comparison(v *ast.BinaryNode) (Node, error) {
	fieldSide, valueSide, op := v.Left, v.Right, v.Operator
	if _, ok := v.Left.(*ast.IdentifierNode); !ok {
		if _, ok := v.Right.(*ast.IdentifierNode); ok {
			fieldSide, valueSide, op = v.Right, v.Left, flip(op)
		} else if u, ok := v.Left.(*ast.UnaryNode); ok && (u.Operator == "not" || u.Operator == "!") {
			return nil, Errorf(l.pos(v.Left), "%s binds tighter than %s; write %s (…)", u.Operator, v.Operator, u.Operator)
		} else {
			return nil, Errorf(l.pos(v.Left), "expected a field on one side of %s", v.Operator)
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
	fd := l.m.Field(f)
	if fd.IDs && (op == "==" || op == "!=") {
		if err := checkIds(fd, []int{val}, func(int) Position { return l.pos(valueSide) }); err != nil {
			return nil, err
		}
	}
	src := fmt.Sprintf("%s %s %d", fd.Name, v.Operator, val)
	if fieldSide != v.Left {
		src = fmt.Sprintf("%d %s %s", val, v.Operator, fd.Name)
	}
	lit := &Lit{F: f, Set: rangeFromOp(fd, op, val), Pos: l.pos(v.Left), Src: src, Neg: op == "!="}
	if verdict, ok := holds(fd, lit.Set); ok {
		if d := fd.Domain; val < d.Lo() || val > d.Hi() {
			l.w.Add(lit.Pos, "%s: %d is outside %s's range %d..%d, so this condition %s", src, val, fd.Name, d.Lo(), d.Hi(), verdict)
		} else {
			l.w.Add(lit.Pos, "%s: %s's range is %d..%d, so this condition %s", src, fd.Name, d.Lo(), d.Hi(), verdict)
		}
	}
	return lit, nil
}

// holds says whether a literal's set is the whole domain or empty. On a
// NULL-able field a literal over the whole domain is not a no-op: it fails
// for every row where the field is NULL, and the group's verdict says so.
func holds(fd *Field, set IntervalSet) (verdict string, ok bool) {
	switch {
	case len(set) == 0:
		return "can never hold", true
	case !set.Equal(fd.Domain):
		return "", false
	case fd.Null != nil:
		return fd.Null.Whole, true
	}
	return "always holds", true
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

// membership lowers `field in [a, b]` and `field in a..b`, and their
// `not in` forms when neg is set.
func (l *lowerer) membership(v *ast.BinaryNode, neg bool) (Node, error) {
	if u, ok := v.Left.(*ast.UnaryNode); ok && (u.Operator == "not" || u.Operator == "!") {
		return nil, Errorf(l.pos(u), "%s binds tighter than in; write %s (%s in %s)", u.Operator, u.Operator, u.Node.String(), v.Right.String())
	}
	f, err := l.fieldOf(v.Left)
	if err != nil {
		return nil, err
	}
	fd := l.m.Field(f)
	d := fd.Domain
	pos := l.pos(v.Left)
	in := "in"
	if neg {
		in = "not in"
	}
	lit := func(set IntervalSet, src string) *Lit {
		set = set.Intersect(d)
		if neg {
			set = set.Complement(d)
		}
		return &Lit{F: f, Set: set, Pos: pos, Src: fmt.Sprintf("%s %s %s", fd.Name, in, src), Neg: neg}
	}
	switch r := v.Right.(type) {
	case *ast.ArrayNode:
		vals := make([]int, 0, len(r.Nodes))
		for _, e := range r.Nodes {
			val, err := l.intLit(e)
			if err != nil {
				return nil, err
			}
			vals = append(vals, val)
		}
		if fd.IDs {
			if err := checkIds(fd, vals, func(i int) Position { return l.pos(r.Nodes[i]) }); err != nil {
				return nil, err
			}
		}
		ivs := make([]Interval, len(vals))
		strs := make([]string, len(vals))
		for i, val := range vals {
			ivs[i] = Interval{val, val}
			strs[i] = fmt.Sprint(val)
		}
		out := lit(SetOf(ivs...), "["+strings.Join(strs, ", ")+"]")
		inDomain := 0
		for i, val := range vals {
			if val < d.Lo() || val > d.Hi() {
				l.w.Add(l.pos(r.Nodes[i]), "%s: %d is outside %s's range %d..%d and is ignored", out.Src, val, fd.Name, d.Lo(), d.Hi())
			} else {
				inDomain++
			}
		}
		if verdict, ok := holds(fd, out.Set); ok {
			switch {
			case len(vals) == 0:
				l.w.Add(pos, "%s: the list is empty, so this condition %s", out.Src, verdict)
			case inDomain == 0:
				l.w.Add(pos, "%s: no listed value is in %s's range %d..%d, so this condition %s", out.Src, fd.Name, d.Lo(), d.Hi(), verdict)
			default:
				l.w.Add(pos, "%s: %s's range is %d..%d, so this condition %s", out.Src, fd.Name, d.Lo(), d.Hi(), verdict)
			}
		}
		return out, nil
	case *ast.BinaryNode:
		if r.Operator == ".." {
			lo, err := l.intLit(r.Left)
			if err != nil {
				return nil, err
			}
			hi, err := l.intLit(r.Right)
			if err != nil {
				return nil, err
			}
			out := lit(SetOf(Interval{lo, hi}), fmt.Sprintf("%d..%d", lo, hi))
			l.warnRange(out, lo, hi)
			return out, nil
		}
	}
	return nil, Errorf(l.pos(v.Right), "expected a list or a range after in")
}

// warnRange warns about a range literal lo..hi whose bounds reach outside
// the field's domain, or which covers the whole domain.
func (l *lowerer) warnRange(lit *Lit, lo, hi int) {
	fd := l.m.Field(lit.F)
	d := fd.Domain
	clipped := SetOf(Interval{lo, hi}).Intersect(d)
	out, outside := lo, lo < d.Lo() || lo > d.Hi()
	if !outside && (hi < d.Lo() || hi > d.Hi()) {
		out, outside = hi, true
	}
	verdict, ok := holds(fd, lit.Set)
	switch {
	case lo > hi: // empty whatever the domain; `not in` makes it whole
		l.w.Add(lit.Pos, "%s: the range %d..%d is empty (%d > %d), so this condition %s", lit.Src, lo, hi, lo, hi, verdict)
	case fd.Below != "" && lo < d.Lo() && hi >= d.Lo() && !ok:
		ignored := fmt.Sprint(lo)
		if lo < d.Lo()-1 {
			ignored = fmt.Sprintf("%d..%d", lo, d.Lo()-1)
		}
		l.w.Add(lit.Pos, "%s: %s; %s is ignored", lit.Src, fd.Below, ignored)
	case ok && outside:
		l.w.Add(lit.Pos, "%s: %d is outside %s's range %d..%d, so this condition %s", lit.Src, out, fd.Name, d.Lo(), d.Hi(), verdict)
	case ok && lo <= hi:
		l.w.Add(lit.Pos, "%s: %s's range is %d..%d, so this condition %s", lit.Src, fd.Name, d.Lo(), d.Hi(), verdict)
	case outside:
		c := clipped[0]
		l.w.Add(lit.Pos, "%s: %d is outside %s's range %d..%d; the range is clipped to %d..%d", lit.Src, out, fd.Name, d.Lo(), d.Hi(), c.Lo, c.Hi)
	}
}

func (l *lowerer) fieldOf(n ast.Node) (FieldID, error) {
	id, ok := n.(*ast.IdentifierNode)
	if !ok {
		return 0, Errorf(l.pos(n), "expected a field name, got %s", n.String())
	}
	f, ok := l.m.Lookup(id.Value)
	if !ok {
		return 0, Errorf(l.pos(n), "unknown field %q", id.Value)
	}
	return f, nil
}

func (l *lowerer) intLit(n ast.Node) (int, error) {
	val, ok := 0, false
	switch v := n.(type) {
	case *ast.IntegerNode:
		val, ok = v.Value, true
	case *ast.FloatNode:
		return 0, l.floatError(n, v, "")
	case *ast.UnaryNode:
		if inner, isInt := v.Node.(*ast.IntegerNode); isInt && v.Operator == "-" {
			val, ok = -inner.Value, true
		}
		if inner, isFloat := v.Node.(*ast.FloatNode); isFloat && v.Operator == "-" {
			return 0, l.floatError(n, inner, "-")
		}
	}
	if !ok {
		return 0, Errorf(l.pos(n), "expected an integer, got %s", n.String())
	}
	if val < -maxLiteral || val > maxLiteral {
		return 0, Errorf(l.pos(n), "integer %d is out of range", val)
	}
	return val, nil
}

// floatError names a float literal as written and, when Expr renders it
// differently (1e2, 100.0), the value it stands for.
func (l *lowerer) floatError(at ast.Node, f *ast.FloatNode, sign string) error {
	rendered := sign + strconv.FormatFloat(f.Value, 'f', -1, 64)
	loc := f.Location()
	if loc.From >= 0 && loc.From < loc.To && loc.To < len(l.t.runeToByte) {
		if text := sign + l.src[l.t.runeToByte[loc.From]:l.t.runeToByte[loc.To]]; text != rendered {
			return Errorf(l.pos(at), "expected an integer, got a float (%s is %s)", text, rendered)
		}
	}
	return Errorf(l.pos(at), "expected an integer, got a float (%s)", rendered)
}

// checkIds rejects explicit ids outside the field's domain, each at its
// own position, with the field's own message when it has one. Ranges and
// ordered comparisons are clipped to the domain instead. It returns a
// plain nil error when every id is in range.
func checkIds(fd *Field, vals []int, posOf func(i int) Position) error {
	d := fd.Domain
	for i, v := range vals {
		if v >= d.Lo() && v <= d.Hi() {
			continue
		}
		if fd.BadID != nil {
			if msg := fd.BadID(v); msg != "" {
				return &Error{Msg: msg, Pos: posOf(i)}
			}
		}
		return Errorf(posOf(i), "%s %d is out of range %d..%d", fd.Name, v, d.Lo(), d.Hi())
	}
	return nil
}

// rangeFromOp is the set of domain values v' with v' op v.
func rangeFromOp(fd *Field, op string, v int) IntervalSet {
	d := fd.Domain
	var raw Interval
	switch op {
	case "==":
		raw = Interval{v, v}
	case "!=":
		return SetOf(Interval{v, v}).Complement(d)
	case "<":
		raw = Interval{d.Lo(), v - 1}
	case "<=":
		raw = Interval{d.Lo(), v}
	case ">":
		raw = Interval{v + 1, d.Hi()}
	case ">=":
		raw = Interval{v, d.Hi()}
	}
	return SetOf(raw).Intersect(d)
}
```

- [ ] **Step 6: Write `internal/lang/nnf.go`**

```go
package lang

// NNF pushes every negation down to the literals, where it becomes the
// complement within the field's domain. The result has no Not. A literal
// on a NULL-able field that ends up complemented is warned about (w may be
// nil): a row where the field is NULL fails every condition on it, so the
// complement does not include it.
func NNF(m *Model, n Node, w *Warnings) Node { return pushNot(m, n, false, w) }

func pushNot(m *Model, n Node, neg bool, w *Warnings) Node {
	switch v := n.(type) {
	case *Not:
		return pushNot(m, v.X, !neg, w)
	case *And:
		if neg {
			return &Or{pushNot(m, v.L, true, w), pushNot(m, v.R, true, w)}
		}
		return &And{pushNot(m, v.L, false, w), pushNot(m, v.R, false, w)}
	case *Or:
		if neg {
			return &And{pushNot(m, v.L, true, w), pushNot(m, v.R, true, w)}
		}
		return &Or{pushNot(m, v.L, false, w), pushNot(m, v.R, false, w)}
	case *Lit:
		fd := m.Field(v.F)
		warnNegatedNull(fd, v, neg, w)
		if !neg {
			return v
		}
		return &Lit{F: v.F, Set: v.Set.Complement(fd.Domain), Pos: v.Pos, Src: v.Src, Neg: !v.Neg}
	}
	return n
}

// warnNegatedNull warns about a literal on a NULL-able field that ends up
// complemented: by an enclosing not (and not written negative), or written
// negative (!=, not in) and not negated again. A literal over the whole
// domain already has its own warning saying it excludes the NULL rows, so
// it gets no second one.
func warnNegatedNull(fd *Field, v *Lit, neg bool, w *Warnings) {
	if w == nil || fd.Null == nil || neg == v.Neg {
		return
	}
	if w.Negated == nil {
		w.Negated = fd.Null
	}
	if v.Set.Equal(fd.Domain) {
		return
	}
	if neg {
		w.Add(v.Pos, "negating %s %s", v.Src, fd.Null.Negated)
	} else {
		w.Add(v.Pos, "%s %s", v.Src, fd.Null.Negated)
	}
}
```

- [ ] **Step 7: Write `internal/lang/dnf.go`**

```go
package lang

import (
	"fmt"
	"strings"
)

// Conjunction is one AND of literals over a model's fields. It is a value:
// copying one copies its arrays, and no stage writes into a set in place.
type Conjunction struct {
	m    *Model
	Has  [MaxFields]bool        // Sets[f] is a constraint (possibly empty = unsatisfiable)
	Sets [MaxFields]IntervalSet // the allowed values per constrained field
	At   [MaxFields]Position    // the first literal on each constrained field, for errors
}

func (c Conjunction) String() string {
	var parts []string
	for f := range c.m.Len() {
		if !c.Has[f] {
			continue
		}
		s := c.m.Field(FieldID(f)).Name
		for _, iv := range c.Sets[f] {
			s += fmt.Sprintf("[%d,%d]", iv.Lo, iv.Hi)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

// TooMany is the conjunction cap's error.
func TooMany(limit int) *Error {
	return Errorf(Position{Line: 1, Column: 1}, "expression expands to more than %d conjunctions; simplify it", limit)
}

// fromLit makes a conjunction of one literal; ok is false when it can never
// hold (an empty set).
func fromLit(m *Model, n Node) (c Conjunction, ok bool) {
	c.m = m
	v, isLit := n.(*Lit)
	if !isLit {
		return c, false
	}
	c.Has[v.F], c.Sets[v.F], c.At[v.F] = true, v.Set, v.Pos
	return c, len(v.Set) > 0
}

// merge ANDs two conjunctions; ok is false when the result is unsatisfiable.
// A field constrained on both sides keeps a's position.
func merge(a, b Conjunction) (Conjunction, bool) {
	c := a
	for f := range a.m.Len() {
		if !b.Has[f] {
			continue
		}
		if !c.Has[f] {
			c.Has[f], c.Sets[f], c.At[f] = true, b.Sets[f], b.At[f]
			continue
		}
		c.Sets[f] = c.Sets[f].Intersect(b.Sets[f])
		if len(c.Sets[f]) == 0 {
			return c, false
		}
	}
	return c, true
}

// ToDNF distributes AND over OR, folding literals into conjunctions as they
// form and dropping unsatisfiable ones, so the intermediate never exceeds
// the output. n must be in negation normal form.
func ToDNF(m *Model, n Node, maxConj int) ([]Conjunction, error) {
	switch v := n.(type) {
	case *And:
		l, err := ToDNF(m, v.L, maxConj)
		if err != nil {
			return nil, err
		}
		r, err := ToDNF(m, v.R, maxConj)
		if err != nil {
			return nil, err
		}
		var out []Conjunction
		for _, a := range l {
			for _, b := range r {
				if c, ok := merge(a, b); ok {
					out = append(out, c)
					if len(out) > maxConj {
						return nil, TooMany(maxConj)
					}
				}
			}
		}
		return out, nil
	case *Or:
		l, err := ToDNF(m, v.L, maxConj)
		if err != nil {
			return nil, err
		}
		r, err := ToDNF(m, v.R, maxConj)
		if err != nil {
			return nil, err
		}
		if len(l)+len(r) > maxConj {
			return nil, TooMany(maxConj)
		}
		return append(l, r...), nil
	}
	if c, ok := fromLit(m, n); ok {
		return []Conjunction{c}, nil
	}
	return nil, nil
}

// Split turns a conjunction whose interval set for a Range field has
// several intervals into one conjunction per interval (a clause holds one
// range per field). List and Key fields are not split.
func Split(conjs []Conjunction, maxConj int) ([]Conjunction, error) {
	var out []Conjunction
	for _, c := range conjs {
		parts := []Conjunction{c}
		for f := range c.m.Len() {
			if !c.Has[f] || len(c.Sets[f]) <= 1 || c.m.Field(FieldID(f)).Kind != Range {
				continue
			}
			// refuse the product before building it
			if len(out)+len(parts)*len(c.Sets[f]) > maxConj {
				return nil, TooMany(maxConj)
			}
			next := make([]Conjunction, 0, len(parts)*len(c.Sets[f]))
			for _, p := range parts {
				for _, iv := range c.Sets[f] {
					q := p
					q.Sets[f] = IntervalSet{iv}
					next = append(next, q)
				}
			}
			parts = next
		}
		out = append(out, parts...)
		if len(out) > maxConj {
			return nil, TooMany(maxConj)
		}
	}
	return out, nil
}
```

- [ ] **Step 8: Run the `internal/lang` tests**

Run: `go test ./internal/lang/ -v`
Expected: PASS, including `TestModelLookup`, `TestNewModelRejects`, `TestMergeAndSplitLeaveInputsAlone`, `TestPosTableMatchesBind`.

- [ ] **Step 9: Move `filterc` onto the model**

Delete `filterc/ast.go`, `filterc/parse.go`, `filterc/nnf.go`, `filterc/dnf.go`.

Replace the top of `filterc/domains.go` (package comment through `isPvp`) with the block below; keep the `domains` comment and table exactly as they are, and keep `pvpNull` from Task 1; then append `pokemonModel`:

```go
// Package filterc compiles boolean filter expressions into Golbat
// pokemon scan (v3) requests. See docs/superpowers/specs for the design.
package filterc

import (
	"fmt"

	"github.com/jfberry/golbat-filterc/internal/lang"
)

// field identifies one of the integer fields an expression may compare: a
// field of pokemonModel.
type field = lang.FieldID

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

// isSpecies reports whether f selects a scan group key (pokemon or form)
// rather than a condition inside a clause.
func isSpecies(f field) bool { return f == fPokemon || f == fForm }

// isPvp reports whether f is a PvP rank, absent for a pokemon without PvP
// data.
func isPvp(f field) bool { return f == fLittle || f == fGreat || f == fUltra }
```

```go
// pokemonModel is the pokemon language as internal/lang sees it: the
// table above, with pokemon and form as scan keys whose explicit ids are
// checked, gender as a list, the PvP ranks NULL-able, and every other
// field a range.
var pokemonModel = func() *lang.Model {
	fields := make([]lang.Field, nFields)
	for f := range nFields {
		fd := lang.Field{Name: fieldNames[f], Kind: lang.Range, Domain: lang.IntervalSet{domains[f]}}
		switch {
		case isSpecies(f):
			fd.Kind, fd.IDs = lang.Key, true
		case f == fGender:
			fd.Kind = lang.List
		case isPvp(f):
			fd.Null = pvpNull
		}
		fields[f] = fd
	}
	fields[fPokemon].BadID = func(v int) string {
		if v < 1 {
			return fmt.Sprintf(`pokemon %d is not a species id; leave pokemon unconstrained for "everything else"`, v)
		}
		return ""
	}
	fields[fPokemon].Below = "species ids start at 1"
	return lang.NewModel("pokemon", fields)
}()
```

Replace `filterc/stages.go` with:

```go
package filterc

import "github.com/jfberry/golbat-filterc/internal/lang"

// The compiler's stages live in internal/lang; these names keep filterc's
// code and its white-box tests reading as they did.
type (
	interval    = lang.Interval
	intervalSet = lang.IntervalSet
	warnings    = lang.Warnings
	conjunction = lang.Conjunction
)

func setOf(ivs ...interval) intervalSet { return lang.SetOf(ivs...) }

// parse lowers src with the pokemon model, discarding warnings.
func parse(src string) (lang.Node, error) { return lang.Lower(pokemonModel, src, nil) }

// lower is parse with warnings collected into w (which may be nil).
func lower(src string, w *warnings) (lang.Node, error) { return lang.Lower(pokemonModel, src, w) }

func nnf(n lang.Node, w *warnings) lang.Node { return lang.NNF(pokemonModel, n, w) }

func toDNF(n lang.Node, maxConj int) ([]conjunction, error) {
	return lang.ToDNF(pokemonModel, n, maxConj)
}

func split(conjs []conjunction, maxConj int) ([]conjunction, error) {
	return lang.Split(conjs, maxConj)
}

func format(n lang.Node) string { return lang.Format(pokemonModel, n) }
```

Create `filterc/validate.go`:

```go
package filterc

// validate rejects a form constraint without a species constraint whose
// small side is positive: forms belong to a species, and the v3 model has
// no "any species, this form" key. A form set covering the whole domain
// constrains nothing.
func validate(conjs []conjunction) error {
	for _, c := range conjs {
		if !c.Has[fForm] || c.Sets[fForm].Equal(intervalSet{domains[fForm]}) {
			continue
		}
		if !c.Has[fPokemon] || smallSideNegative(fPokemon, c.Sets[fPokemon]) {
			return errorf(c.At[fForm], "form needs a pokemon id in the same conjunction (after a negation, write the species explicitly: pokemon != X || (pokemon == X && form != F))")
		}
	}
	return nil
}
```

In `filterc/dispatch.go` rename: `c.has[` / `p.c.has[` → `.Has[`; `c.ranges[` / `p.c.ranges[` → `.Sets[`; `field(f).isSpecies()` → `isSpecies(field(f))`. `clauseFor` becomes:

```go
func clauseFor(c conjunction, ids []PokemonId) Clause {
	cl := Clause{Pokemon: ids}
	for f := range nFields {
		// pokemon and form are the clause's keys, not conditions
		if !c.Has[f] || isSpecies(f) {
			continue
		}
		if f == fGender {
			cl.Gender = c.Sets[f].Values()
			continue
		}
		iv := c.Sets[f][0] // a range field has exactly one interval after split
		cl.setRange(f, &MinMax{Min: iv.Lo, Max: iv.Hi})
	}
	return cl
}
```

`filterc/compile.go`'s stage calls already go through the wrappers (`lower(expression, w)`, `toDNF(nnf(n, w), …)`, `split(…)`, `validate(…)`, `w.List()`). Change only the note, so the text comes from the NULL group instead of a literal:

```go
		if w.Negated != nil {
			msg += "; " + w.Negated.Note
		}
```

It produces the same text as before (`"; note that negated PvP conditions never match pokemon without PvP data"`); `TestWarnings` pins it.

- [ ] **Step 10: Update the `filterc` tests mechanically**

- `filterc/domains_test.go`: `f.String()` → `fieldNames[f]` (both places in `TestFieldNamesRoundTrip`, keeping the `"field %d: String() = %q, want %q"` literal); `fPokemon.isSpecies()` → `isSpecies(fPokemon)` and likewise for `fForm`, `fIv`. Add:

```go
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
```

(add `"github.com/jfberry/golbat-filterc/internal/lang"` to that file's imports).
- `filterc/parse_test.go`: `TestPosTableMatchesBind` already moved (Step 1). Nothing else should need changing — `parse` and `format` are the wrappers. If the compiler reports a leftover internal name, rename it per the Task 1 table.
- `filterc/dnf_test.go`, `filterc/golden_test.go`, `filterc/reference_test.go`, `filterc/row_test.go`: should compile unchanged (`conjunction`, `parse`, `nnf`, `toDNF`, `split`, `validate`, `dispatch`, `planFor`, `distinguishedKeys`, `field`, `fieldByName` all still exist). Fix only what the compiler reports, by the renames above.

- [ ] **Step 11: Run everything**

```bash
gofmt -l . ; go vet ./... && go test ./... && go test ./filterc/ -run MatchesReference -v 2>&1 | grep checked | diff - /tmp/filterc-base/checked.txt && go run /tmp/litcheck/main.go /tmp/filterc-base/filterc ./filterc ./internal/lang
```

Expected: no gofmt output, vet clean, all `ok`, empty diff, `ok: all N distinct literals …`.

- [ ] **Step 12: Commit**

```bash
git add -A internal/lang filterc
git commit -m "refactor: lowering, NNF and DNF move to internal/lang behind a Model

filterc's pokemon table now builds a lang.Model (domains, kinds, the PvP
NULL group, species id checks); validate and dispatch stay in filterc.
No behaviour change: no golden, warning or error text changes; the
property test checks the same 4276 expressions.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: prove it, document it, open the PR

**Files:**
- Modify: `CLAUDE.md` (Pipeline section), `docs/handover.md` ("Where things are" table, state line), `docs/superpowers/specs/2026-09-25-filter-expression-compiler-design.md` ("Repository layout" closing paragraph)

**Interfaces:**
- Consumes: everything from Tasks 1–2.
- Produces: the phase 1 PR; phase 2's plan is written against this code.

- [ ] **Step 1: Run the full proof explicitly**

```bash
go test ./... -count=1
go test ./filterc/ -run MatchesReference -v -count=1 2>&1 | grep checked
go test ./filterc/ -run 'Cap|Alloc|Refuse|Allocations|Before' -v -count=1 2>&1 | grep -E '^(=== RUN|--- |ok|FAIL)'
go test ./filterc/ -run xxx -fuzz FuzzCompile -fuzztime 20s
go run /tmp/litcheck/main.go /tmp/filterc-base/filterc ./filterc ./internal/lang
gofmt -l . ; go vet ./...
```

Expected: all `ok`; `checked 4276 expressions (724 skipped; 1785 on the species×form grid)`; every listed allocation/cap test `--- PASS`; fuzz runs 20 s with no failure; litcheck `ok`; no gofmt output; vet clean.

Also confirm the golden files did not change: `git diff main --stat -- filterc/golden_test.go filterc/warnings_test.go` shows at most the `w.Add`/`w.List` renames in `warnings_test.go` and nothing in `golden_test.go`.

- [ ] **Step 2: Update `CLAUDE.md`'s Pipeline section**

Replace the section from `## Pipeline (package \`filterc\`)` down to (not including) the line starting `` `golbat/` is a minimal scan client`` with:

````markdown
## Pipeline (`internal/lang` + package `filterc`)

```
internal/lang/  the model-driven front end, shared by every language
  lower.go      Expr parser → whitelist lowering → boolean tree of literals.
                Every literal is an interval set over its field's domain
                (species and form included). Errors and most warnings start here.
  nnf.go        negation normal form: `!` becomes complement within the domain.
  dnf.go        DNF with early pruning (AND = intersection, empty → dropped),
                interval splitting for Range fields.
  model.go      Model and Field: name, kind (Range/List/Key), domain, NULL
                group, id checks — what the front end reads instead of
                package tables. intervals.go, errors.go (positioned errors,
                the warnings collector), ast.go.
filterc/
  domains.go    The pokemon field domains and their sentinels — every range
                decision lives in this table and its comments, which cite
                Golbat's lookup helpers. Builds pokemonModel.
  validate.go   a form constraint needs a species constraint whose small side
                is positive.
  dispatch.go   THE CORE. Per conjunction, the smaller side of its species set and
                of its form set names the distinguished (species, form) keys; a
                key's bucket is the conjunctions that hold for a representative
                of that key; emission writes a generic clause, a keyed clause
                listing every key whose bucket contains the conjunction, and a
                block clause for every empty bucket. Four caps are checked
                before the allocation they bound. Identical clauses are deduped.
  types.go      v3 wire shapes. compile.go  public API + options. stages.go
                unexported wrappers over internal/lang for compile.go and tests.
```

````

- [ ] **Step 3: Update `docs/handover.md` and the old spec's layout note**

In `docs/handover.md`, change the "State on …" line to `State on 2026-10-12: phase 1 of the fort/names spec (internal/lang refactor) in review; go test ./... green.` and replace the first three rows of the "Where things are" table with:

```markdown
| `internal/lang/` | the shared front end: `lower.go` (Expr parser + whitelist lowering; all errors and most warnings originate here), `nnf.go`, `dnf.go` (DNF, early pruning, interval splitting), `model.go` (the field-table type every language fills in) |
| `filterc/validate.go`, `dispatch.go` | the form-needs-species check; the core: small-side key naming, buckets via `applies`, caps, emission, clause dedupe |
| `filterc/domains.go` | the pokemon field table and domains — every semantic decision about ranges lives in this table and its comments; builds `pokemonModel` |
```

In `docs/superpowers/specs/2026-09-25-filter-expression-compiler-design.md`, append to the paragraph after the "Repository layout" code block:

```markdown
Since 2026-10-12 (fort/names spec, phase 1) lowering, NNF, DNF and split
live in `internal/lang` behind a `Model`; `filterc/domains.go` builds the
pokemon model and `filterc/validate.go` holds the species check.
```

- [ ] **Step 4: Commit and open the PR**

```bash
git add CLAUDE.md docs/handover.md docs/superpowers/specs/2026-09-25-filter-expression-compiler-design.md
git commit -m "docs: internal/lang in the agent guide, handover and layout note

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push -u origin HEAD
gh pr create --title "refactor: model-driven front end in internal/lang (fort/names phase 1)" --body "$(cat <<'EOF'
Phase 1 of docs/superpowers/specs/2026-10-12-fort-filters-and-game-names-design.md
(spec and plan included in this branch).

Lowering, interval sets, NNF, DNF/split, errors and warnings move from
filterc into internal/lang, driven by a Model (field table). The pokemon
table in filterc/domains.go builds that model; validate and dispatch stay
in filterc. Public API unchanged (Error and Position become aliases).

Proof of no behaviour change:
- no golden changes; every string literal of the base tests is still present (checked mechanically)
- the seeded property test checks the same 4276 expressions (724 skipped; 1785 on the grid)
- allocation-ceiling and cap tests unchanged and passing; fuzz 20 s clean

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Then register the PR with the thread (`link_pull_request` with the URL `gh` printed).
