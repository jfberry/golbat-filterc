# Handover

For whoever picks this up next — human or agent. Read this, the README and the
spec; the plan under `docs/superpowers/plans/` is the historical build record
and is not kept current (it still shows the PvP domain top as 32767, for
example).

State on 2026-10-11: PR #1 merged to `main` (`ae74ad1`); no open PRs; `go test
./...` green.

## What this is, in one paragraph

A compiler from boolean filter expressions to Golbat `POST
/api/pokemon/v3/scan` requests, plus a CLI and a small server that can run the
scan. Golbat's filter model is **not** a flat OR: clauses are grouped by the
`(species, form)` keys they list and a pokemon is matched against the most
specific group that exists for it (exact id+form → id → "everything else").
That was argued out in UnownHash/Golbat#417 and documented in Golbat#420; the
spec's Purpose section has the links. This compiler exists so that people can
write negation and shared rules naturally while Golbat keeps its fast typed
model untouched. Do not propose union semantics or negation in Golbat's API;
extend this compiler instead.

## Where things are

| Path | Responsibility |
|---|---|
| `filterc/parse.go` | Expr parser + whitelist lowering to the boolean tree; all errors and most warnings originate here |
| `filterc/nnf.go`, `dnf.go` | negation normal form (complement within the domain), DNF with early pruning, interval splitting, the form-needs-species check |
| `filterc/dispatch.go` | the core: small-side key naming, buckets via `applies`, caps, emission, clause dedupe |
| `filterc/domains.go` | the field domains — every semantic decision about ranges lives in this table and its comments |
| `filterc/compile.go`, `types.go`, `errors.go` | public API, v3 wire types, positioned errors and the warnings collector |
| `filterc/matcher_test.go` | **the oracle**: Golbat's matcher transcribed verbatim (see "Re-syncing" below) |
| `filterc/reference_test.go`, `row_test.go`, `property_test.go` | the three-valued reference evaluator and the property test that proves the compiler against it |
| `golbat/` | minimal scan client |
| `cmd/filterc/` | `compile` / `scan` / `serve`, TOML config |
| `docs/superpowers/specs/…design.md` | the design; its "Decisions recorded" and "Limits" sections are authoritative |

## How to know it still works

```
go test ./...                                   # everything, ~5 s
go test ./filterc/ -run MatchesReference -v     # logs "checked N expressions (M skipped; K on the species×form grid)"
go test ./filterc/ -run xxx -fuzz FuzzCompile -fuzztime 20s
go test ./filterc/ -run 'Cap|Alloc|Refuse' -v   # allocation-ceiling tests for the resource bounds
```

If the property test fails, the compiler is wrong, not the test: reduce the
printed expression and row, add the case to `golden_test.go` with the correct
output, then fix the compiler. Never "fix" the reference evaluator, the
matcher, the generator or the invariants to make it pass.

## Re-syncing the vendored matcher

`filterc/matcher_test.go` is a line-for-line transcription of Golbat's
`decoder/api_pokemon_scan_v3.go` (`isPokemonDnfMatch`) and the probe chain in
`decoder/api_pokemon_common.go` (`internalGetPokemonInArea`), at the Golbat
commit named in the file header. Whenever Golbat changes either, re-transcribe,
update the header commit, and run the property test. The domains table in
`domains.go` cites the lookup helpers in Golbat's `decoder/pokemonRtree.go`
(`lookupIv`, `lookupInt8`, `lookupInt16`, `calculatePokemonPvpLookup`) — check
those too; the -1 sentinel, the PvP nil/4096 behaviour and the saturation
ceilings are what the domains encode.

## Decisions that are easy to get wrong

- **Three-valued PvP.** A pokemon with no PvP data has NULL ranks; every PvP
  comparison on it is UNKNOWN, including negated ones. `!(great <= 100 &&
  iv == 100)` therefore excludes a perfect pokemon with no PvP data. This is
  Golbat's behaviour, mirrored on purpose and warned about (W1). "No PvP
  data" is not expressible in v3.
- **`-1` is a value** for `iv/atk/def/sta/level/cp/size` ("no encounter"), so
  `iv < 50` and `!(iv >= 90)` include un-encountered pokemon. Warned where it
  surprises.
- **Domain tops.** `iv` −1..100 and stats −1..15 are narrower than Golbat's
  lookup can hold for legacy rows (saturates at 127); such rows are never
  returned by a compiled filter — deliberate, documented. PvP tops are 4096
  because Golbat never ranks worse.
- **A form constraint needs a species constraint whose small side is
  positive.** Exact per conjunction; only sufficient per expression:
  `!(pokemon == 1 && form == 0)` is expressible but rejected with a rewrite
  hint (`pokemon != 1 || (pokemon == 1 && form != 0)`). A later feature could
  split such a conjunction against the other conjunctions' positive species.
- **Caps** (512 conjunctions, 10k keys, 100k emitted ids, 10k clauses) are each
  checked *before* the allocation they bound; the pre-checks are exact lower
  bounds. Three review rounds got this wrong by summing the wrong thing — read
  `ownEntries()`/`keyCount()` in `dispatch.go` before changing them.
- **`serve` has no auth and carries the Golbat secret**; it listens on
  `127.0.0.1:8080` by default. Keep it that way.

## Backlog (deferred during review, none load-bearing)

Diagnostics
- A whole/empty literal under an enclosing `!` is described as written
  (`!(great >= 1) || iv == 100` reports the PvP text on `great >= 1` while the
  negated literal is dropped silently). Reword to "negating <lit> …" when
  `pushNot` complements a literal that already carries a verdict.
- The ids pre-check counts conjunctions that clause dedupe later removes
  (consistent with the spec's two-check description; could be computed
  post-dedupe).

Tests
- Generator never draws rows above level 50 / cp 1501 except the explicit
  top values; grid fixes only 32760 and 32767 at the top of the species
  domain; no count of how often the block/PvP invariants fire.
- `intervals_test.go` lacks multi×multi intersect and out-of-domain
  complement inputs (the property test covers them indirectly).

Client / CLI / server
- `golbat/client_test.go` indexes `resp.Pokemon[0]` after a non-fatal length
  check; Content-Type and the absent-secret header are untested; error bodies
  are cut at 4096 bytes without a marker.
- CLI: flag errors print twice and `-h` shows the sparse auto-usage (set
  `fs.Usage`); flags must precede the expression; a bad config file exits 2;
  no test for a zero-valued flag (`--limit 0`) overriding the file.
- `serve`: `Shutdown` has 5 s against a 60 s Golbat client timeout; 404/405
  are text/plain while other errors are JSON; unknown body fields, a negative
  `limit` and trailing data are accepted; no Content-Type check (CSRF from a
  browser on the same host — low impact on loopback).

Spec
- Still promises an opt-in end-to-end test against a live Golbat; it was
  never built (`filterc scan` was checked by hand). Build it or drop the
  promise.

## Extension points (spec "Extension points")

Names/masterfile (a resolver hook before type checking; also enables
species-agnostic form predicates by expansion); fort filters (same pipeline
without dispatch — Golbat's fort filters are a plain OR); a JSON-AST input
format for clients without a parser.

## Golbat-side loose ends

- Golbat#420 (docs PR describing the precedence model) — merge it.
- Golbat#419 (the restored #416) and #414 — close with a pointer to #417/#420.
