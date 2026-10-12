# golbat-filterc — guide for agents

A Go library, CLI and small server that compile boolean filter expressions
into [Golbat](https://github.com/UnownHash/Golbat) scan requests. Today it
covers the pokemon scan (`POST /api/pokemon/v3/scan`). **The next piece of
work — fort models (gyms, pokestops, stations) and game names — is designed in
`docs/superpowers/specs/2026-10-12-fort-filters-and-game-names-design.md`; see
"Next: forts" at the end.**

Read in this order: this file; `README.md` (usage, language, semantics);
`docs/superpowers/specs/2026-09-25-filter-expression-compiler-design.md` (the
design — its "Decisions recorded" and "Limits" sections are authoritative);
`docs/superpowers/specs/2026-10-12-fort-filters-and-game-names-design.md` (fort
languages and game names — the current design for the next work);
`docs/handover.md` (backlog and gotchas); `docs/resync.md` (what to do when
Golbat changes). The plan under `docs/superpowers/plans/` is the historical
build record and is not kept current.

## Why this exists

Golbat's pokemon filter is **not** a flat OR. Clauses are grouped by the
`(species, form)` keys they list; a pokemon is matched against the most
specific group that exists for it — exact id+form, else id, else the clauses
with no pokemon list ("everything else") — and within that group a clause
matches if all its conditions hold. A less specific group never applies to a
pokemon that has a more specific one. That is what makes "everything else"
expressible without negation and keeps Golbat's matcher O(1) per candidate.
It is documented in Golbat's `api.md` ("Filter semantics", Golbat#420) and was
settled in Golbat#417 after #414/#416 misread it as a bug. **Do not propose
union semantics or negation in Golbat's API; extend this compiler instead.**

The compiler lets a person write

```
size == 5 && pokemon != 710
iv == 100 || (pokemon == 1 && gender == 2)
pokemon in 1..151 && iv >= 90
```

and emits the typed request, once, at edit time. Golbat's hot path is untouched.

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

`golbat/` is a minimal scan client. `cmd/filterc/` is `compile` / `scan` /
`serve` with a TOML config (`listen` defaults to `127.0.0.1:8080`; the
server has no auth and carries the Golbat secret — keep it on loopback).

## The proof, and the rule about it

`filterc/property_test.go` compiles random expressions and checks, for random
rows, that `matcher(compile(e), row) == reference(e, row)`:

- `filterc/matcher_test.go` — Golbat's v3 matcher and probe chain transcribed
  verbatim at the commit named in its header. **The oracle.**
- `filterc/reference_test.go` — a three-valued (SQL-style) evaluator over the
  Expr AST that shares no code with lowering, NNF, DNF or dispatch. **The
  definition of the language.**
- `filterc/golden_test.go` — exact JSON for the spec examples and every edge
  found in review; `filterc/warnings_test.go` — exact text and position of
  every diagnostic; `filterc/fuzz_test.go`.

**If the property test fails, the compiler is wrong.** Reduce the printed
expression and row, add a golden with the correct output, fix the compiler.
Never adjust the reference, the matcher, the generator or the invariants to
make it pass. When Golbat changes, follow `docs/resync.md`.

```
go test ./...                                  # ~5 s
go test ./filterc/ -run MatchesReference -v    # logs "checked N expressions (… on the species×form grid)"
go test ./filterc/ -run xxx -fuzz FuzzCompile -fuzztime 20s
go test ./filterc/ -run 'Cap|Alloc|Refuse' -v  # allocation-ceiling tests for the caps
```

## Semantics you must not "fix"

- **Three-valued PvP.** A pokemon with no PvP data has NULL ranks; every PvP
  comparison on it is UNKNOWN, negated or not. `!(great <= 100 && iv == 100)`
  excludes a perfect pokemon with no PvP data. Mirrors Golbat; warned (W1).
  `4096` = has PvP data, unranked in that league; the PvP domain top is 4096.
- **`-1` is a value** ("no encounter data") for `iv/atk/def/sta/level/cp/size`,
  so `iv < 50` and `!(iv >= 90)` include un-encountered pokemon.
- **Domain tops** for `iv` (100) and stats (15) are narrower than Golbat's
  lookup can hold for legacy rows (it saturates at 127); such rows are never
  returned by a compiled filter. Deliberate and documented.
- **A form constraint needs a species constraint whose small side is positive**
  (forms belong to a species; v3 has no "any species, this form" key). Exact
  per conjunction, only sufficient per expression: `!(pokemon == 1 && form == 0)`
  is rejected with a correct rewrite hint.
- **Caps** (512 conjunctions, 10k distinct keys, 100k emitted pokemon entries,
  10k clauses) are each checked *before* the allocation they bound and the
  pre-checks are exact lower bounds. Three review rounds got these wrong by
  summing the wrong quantity; read `keyCount()` / `ownEntries()` in
  `dispatch.go` before touching them.

## Diagnostics conventions

Errors are `*filterc.Error{Msg, Pos}`; `Pos` is 1-based line and column (rune
columns) plus a byte offset; `Error()` renders `line:column: msg`. Warnings are
`[]string`, positioned the same way when they concern a literal, deduplicated
by text+position, in source order; they never change the emitted request. The
CLI prints a caret under the thing each message is about. Every new message
needs a row in `warnings_test.go` or `parse_test.go` pinning text *and*
position, and a line in the README's warnings list. Write messages as a map
operator would read them: name the literal as written, the value, the range,
and what to write instead when there is a fix.

## Working conventions

- Go 1.24; dependencies are `expr-lang/expr` (parser only — never its
  checker, compiler or VM) and `BurntSushi/toml`. `gofmt -l .` empty and
  `go vet ./...` clean on every commit.
- Golden output is the contract: a change to emitted JSON needs a golden
  change in the same commit, named in the commit message.
- Specs and plans live under `docs/superpowers/`; design before code for
  anything architectural (the fort model is architectural).
- Commit trailers: `Co-Authored-By: <model> <noreply@anthropic.com>`.

## Next: forts

The spec `docs/superpowers/specs/2026-10-12-fort-filters-and-game-names-design.md`
is approved and is the authority. The work is four phases, each with its own
plan and PR: phase 1, the `internal/lang` refactor, is this branch; phase 2
(game data and names), phase 3 (forts) and phase 4 (plumbing and docs) follow,
each planned against the code the previous phase produced. There is one
language per fort type, targeting the single-type scans; the combined scan is
out of scope. The Golbat sources to read are the ones listed in the spec's
header, and the semantics to keep in mind are summarised in its "Decisions
recorded".
