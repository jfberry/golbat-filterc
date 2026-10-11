# golbat-filterc — guide for agents

A Go library, CLI and small server that compile boolean filter expressions
into [Golbat](https://github.com/UnownHash/Golbat) scan requests. Today it
covers the pokemon scan (`POST /api/pokemon/v3/scan`). **The next piece of
work is a fort model** (gyms, pokestops, stations) on the same pipeline — see
"Next: forts" at the end.

Read in this order: this file; `README.md` (usage, language, semantics);
`docs/superpowers/specs/2026-09-25-filter-expression-compiler-design.md` (the
design — its "Decisions recorded" and "Limits" sections are authoritative);
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

## Pipeline (package `filterc`)

```
parse.go      Expr parser → whitelist lowering → boolean tree of literals.
              Every literal is an interval set over its field's domain
              (species and form included). Errors and most warnings start here.
nnf.go        negation normal form: `!` becomes complement within the domain.
dnf.go        DNF with early pruning (AND = intersection, empty → dropped),
              interval splitting for range fields, validate (a form
              constraint needs a species constraint whose small side is positive).
dispatch.go   THE CORE. Per conjunction, the smaller side of its species set and
              of its form set names the distinguished (species, form) keys; a
              key's bucket is the conjunctions that hold for a representative
              of that key; emission writes a generic clause, a keyed clause
              listing every key whose bucket contains the conjunction, and a
              block clause for every empty bucket. Four caps are checked
              before the allocation they bound. Identical clauses are deduped.
domains.go    The field domains and their sentinels — every range decision
              lives in this table and its comments, which cite Golbat's lookup
              helpers.
types.go      v3 wire shapes. compile.go  public API + options. errors.go
              positioned errors and the warnings collector.
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

Golbat's fort scan (`POST /api/fort/scan` combined, plus the per-type scans;
see Golbat `api.md` and `decoder/api_fort.go`) takes `ApiFortDnfFilter`
clauses. The model differs from pokemon in ways that must be designed for, not
assumed:

1. **Plain OR, no dispatch.** `isFortDnfMatch(fortType, lookup, filter, now)`
   is evaluated for every clause; a fort matches if any clause holds. There are
   no species groups, so the whole dispatch stage disappears — but **an empty
   or omitted `filters` matches every fort of the type**, the opposite of
   pokemon (where it matches nothing). The compiler must emit something
   explicit for "match nothing".
2. **List conditions are tri-state.** Most fields are lists (`team_id`,
   `raid_level`, `lure_id`, `quest_reward_type`, `incident_character`,
   `battle_level`, …): omitted/null = no constraint, an explicitly empty list =
   matches nothing, otherwise membership. Ranges are `ApiFortDnfMinMax`
   (`available_slots`, `quest_reward_amount`); booleans are `*bool`
   (`is_ar_scan_eligible`, `stationed_gmax`, `station_active`,
   `battle_available`). Pokemon-valued lists (`raid_pokemon_id`,
   `quest_reward_pokemon`, `contest_pokemon`, `battle_pokemon`) are `ApiDnfId`
   lists with the same id/form shape as pokemon keys but **no precedence
   semantics** — just membership. `contest_focus` is a structured list.
3. **Time-dependent predicates.** Raid, lure, showcase and station conditions
   are evaluated against `now` (`stationActiveAt`, raid active =
   `battle > now || end > now`, lure expiry, …). The reference evaluator and
   the row model need a `now`, and the property test must draw timestamps on
   both sides of it.
4. **Fort type.** Fields are per type (gym / pokestop / station); the combined
   endpoint groups clauses per type (`ApiFortCombinedScan`, `combinedFortMatches`).
   Decide whether the language has one `fort` namespace with a `type` field or
   one language per type.
5. **Negation.** With a plain OR and list membership, `team != 1` is just the
   complement list over a small domain, and `!(raid_level in [5, 6])` is the
   complement — no dispatch subtleties, but the tri-state (empty list ≠ nil)
   must survive NNF/DNF.

Approach that fits the existing architecture: keep `parse.go`'s front end,
make the field table and domains per model (the spec's "Extension points"
sketches `internal/model`), skip dispatch, add a fort emitter, a vendored
`isFortDnfMatch` + `combinedFortMatches` as the oracle, a reference evaluator
with `now`, and a property test over a row generator with timestamps. Start
with a spec under `docs/superpowers/specs/` and get it reviewed before coding;
the Golbat sources to read first are `decoder/api_fort.go` (`ApiFortDnfFilter`,
`isFortDnfMatch`, `combinedFortMatches`, `stationActiveAt`),
`decoder/fortRtree.go` (`FortLookup`, the sentinels), and `grpc/api.proto`
(`FortDnfFilter`).
