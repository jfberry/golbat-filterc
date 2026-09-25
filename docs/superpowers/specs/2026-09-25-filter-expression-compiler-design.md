# golbat-filterc: filter expression compiler — design

Status: draft for review. Date: 2026-09-25.

## Purpose

Golbat's pokemon scan API (`POST /api/pokemon/v3/scan`) takes a typed
filter: a list of clauses grouped by the pokemon/form keys they list, where
a pokemon is matched against the most specific group that exists for it
(exact id+form, else id, else "everything else") and within that group a
clause matches if all its conditions hold. That model is fast to evaluate
and can express "everything else" without negation, but it is awkward to
write by hand: a shared rule that should also apply to a species with its
own rule has to be repeated under that species, hiding a species is a clause
that can never hold, and there is no negation at all.

`golbat-filterc` lets a person write the filter as a boolean expression —

```
size == 5 && pokemon != 710
iv == 100 || (pokemon == 1 && gender == 2)
```

— and compiles it, once, into the typed v3 request. Golbat's hot path is
untouched: the compiler runs when a filter is edited, not per row. It is a
library first, with a CLI and a small server on top so that a checkout of
the repo builds into a useful tool: compile an expression, or compile it and
run the scan against a Golbat instance.

Background: UnownHash/Golbat#417 (the semantics), #134 (where the model was
introduced), #414/#416/#419 (the misreading of the fallback as a bug and its
reversion), #420 (the documentation of the model). Server-side expression
evaluation was tried in Golbat#114 (Expr per row) and rejected on
performance; a current-version measurement puts Expr at ~120 ns per simple
evaluation against ~2 ns for the typed matcher, and linear in the number of
species disjuncts where the typed model is O(1). Compiling to the typed
model keeps the typed model's cost and adds the expressiveness.

## Scope

v1 delivers:

- the pokemon language and compiler, targeting the v3 request shape;
- a Go library (`filterc`, plus a thin `golbat` client package);
- a CLI: `filterc compile`, `filterc scan`, `filterc serve`;
- a config file for Golbat's address and secret, default scan bounds and
  limit, and the server's listen address;
- the test suite described below, including a property test against a
  reference evaluator.

Out of scope for v1, with the extension point kept:

- names (`PUMPKABOO`, `ALOLAN`) and species-agnostic form predicates, which
  need a masterfile — see "Extension points";
- fort filters (gyms, pokestops, stations) — a second model on the same
  pipeline, see "Extension points";
- v2 output, a JSON-AST input format, and any change to Golbat.

## The language

### Syntax

The syntax is the subset of [Expr](https://expr-lang.org/) below; Expr's
parser and type checker are used, its VM is not.

| Construct | Example |
|---|---|
| comparison | `iv >= 90`, `pokemon == 1`, `gender != 2` |
| membership | `pokemon in [1, 4, 7]`, `gender not in [0, 3]` |
| range membership | `iv in 90..100`, `level not in 1..29` |
| conjunction, disjunction, negation | `&&`, `\|\|`, `!` (also `and`, `or`, `not`) |
| grouping | `( … )` |
| literals | decimal integers, possibly negative |

Fields (all integers): `pokemon`, `form`, `iv`, `atk`, `def`, `sta`,
`level`, `cp`, `gender`, `size`, `little`, `great`, `ultra`. Comparisons
are between a field and an integer literal (either side). Anything else the
Expr parser accepts — strings, floats, arithmetic, function calls, member
access, ternaries, field-to-field comparison — is rejected with a position.

### Domains

Each field has a domain: the set of values Golbat's lookup can hold for it.
Complements are taken within the domain, and both bounds of every emitted
range come from it, so the v3 API's "an omitted bound is 0" behaviour never
applies to compiled output.

| Field | Domain | Notes |
|---|---|---|
| `pokemon` | 1..32767 | 0 and negatives are rejected; forms belong to a species |
| `form` | 0..32767 | 0 is "no form sent", Golbat's default |
| `iv` | −1..100 | −1 = no encounter data |
| `atk`, `def`, `sta` | −1..15 | −1 = no encounter data |
| `level` | −1..127 | −1 = no encounter data; storage maximum |
| `cp` | −1..32767 | −1 = no encounter data; storage maximum |
| `size` | −1..5 | −1 = unknown |
| `gender` | {−1, 0, 1, 2, 3} | −1 = unknown; 0 unset, 1 male, 2 female, 3 genderless |
| `little`, `great`, `ultra` | 1..32767 | 4096 = has PvP data but no rank in that league; NULL when the pokemon has no PvP data at all |

The `−1` values are real members of their domains because Golbat's lookup
stores them as values and its matcher compares them as values: `iv in
-1..100` is the documented way to include un-encountered pokemon. PvP ranks
are different: a pokemon with no PvP data has no rank values at all, and
Golbat's matcher fails every PvP condition for it.

### Semantics

The language has SQL-style three-valued logic. A comparison on a NULL PvP
rank is UNKNOWN; `NOT UNKNOWN` is UNKNOWN; `AND`/`OR` follow Kleene's
tables; a pokemon matches the expression if and only if it evaluates to
TRUE. Every other field is always a value, so for expressions that do not
mention PvP the logic is ordinary two-valued logic.

Consequences a user should know, each of which mirrors what Golbat's
matcher does today:

- `!(great <= 100)` is true for a pokemon ranked 101 or worse, or unranked
  in Great League (4096), and *not* for a pokemon with no PvP data.
- `!(iv >= 90)` is true for un-encountered pokemon (`iv == -1`).
- `form == 0` on its own is a compile error: a form literal must be in a
  conjunction with a positive `pokemon` literal, because forms belong to a
  species and the v3 model has no "any species, this form" key. (With a
  masterfile this becomes an expansion; see "Extension points".)

### Examples

`size == 5 && pokemon != 710` — XXL pokemon except Pumpkaboo:

```json
{"filters": [
  {"size": {"min": 5, "max": 5}},
  {"pokemon": [{"id": 710}], "iv": {"min": 1, "max": 0}}
]}
```

`iv == 100 || (pokemon == 1 && gender == 2)` — every perfect pokemon,
plus female Bulbasaur. Bulbasaur has its own group, so the shared clause is
repeated under its key; that is the merge the client would otherwise have to
write by hand:

```json
{"filters": [
  {"iv": {"min": 100, "max": 100}},
  {"pokemon": [{"id": 1}], "iv": {"min": 100, "max": 100}},
  {"pokemon": [{"id": 1}], "gender": [2]}
]}
```

`pokemon == 1 && form != 0 && iv >= 90` — form 0 gets an exact group that
blocks, every other form of Bulbasaur uses the species group:

```json
{"filters": [
  {"pokemon": [{"id": 1}], "iv": {"min": 90, "max": 100}},
  {"pokemon": [{"id": 1, "form": 0}], "iv": {"min": 1, "max": 0}}
]}
```

`pokemon in [1, 4, 7] && iv == 100` coalesces into one clause with three
keys. `iv != 50` becomes two generic clauses (`-1..49`, `51..100`).
`!(iv >= 90)` becomes `{"iv": {"min": -1, "max": 89}}`.

## The compiler

### Stages

1. **Parse and check.** `expr/parser` then `expr/checker` against an
   environment declaring the 13 fields as `int`. Then a whitelist walk over
   the AST that admits only the constructs above; anything else is an error
   at its position. Unary minus on an integer literal is folded here.
2. **Lower to literals.** Each comparison becomes a canonical literal:
   for range fields an *interval set* over the domain (a sorted list of
   disjoint closed intervals); for `gender` a value set; for `pokemon` and
   `form` an id set with a negated flag. `in`, `not in`, ranges and all six
   comparison operators reduce to these.
3. **Negation normal form.** `!` is pushed to the literals with De Morgan;
   on a literal it is the complement within the field's domain (intervals →
   complementary intervals; species/form → flip the flag).
4. **Disjunctive normal form.** AND is distributed over OR. A conjunction is
   built as it is formed — intersecting interval sets and value sets,
   intersecting positive species and form sets, unioning negative ones — so
   a conjunction that becomes unsatisfiable (an empty interval set, an empty
   positive species or form set, a positive species or form also in the
   corresponding negative set) is dropped immediately and the intermediate
   never holds more than the final conjunctions. The conjunction count is
   capped.
5. **Split.** A v3 clause holds one `{min, max}` per range field, so a
   conjunction whose interval set for a field has *n* intervals becomes *n*
   conjunctions (the cartesian product across fields, under the same cap).
   `gender` is emitted as a list and needs no split.
6. **Validate.** A conjunction with a positive or negative form literal
   must have a positive species set; otherwise it is an error citing the
   form literal's position.
7. **Dispatch.** See below.
8. **Emit** the v3 JSON: keys sorted, conjunctions in source order, ranges
   with both bounds.

### Dispatch

For a conjunction *c* write S⁺(c) for its positive species set (or ANY),
S⁻(c) for its negative species set, F⁺(c) for its positive form set (or
ANY) and F⁻(c) for its negative form set.

**Distinguished keys** D are the `(species, form)` keys the expression names:

- for each *c* and each s ∈ S⁺(c): `(s, f)` for every f ∈ F⁺(c) if F⁺(c) is
  not ANY; `(s, f)` for every f ∈ F⁻(c); and `(s, any)` if F⁺(c) is ANY;
- for each *c* and each s ∈ S⁻(c): `(s, any)`.

**Buckets.** A key's bucket is the set of conjunctions that apply to a
representative pokemon of that key:

- exact key `(s, f)`: *c* with (S⁺(c) = ANY or s ∈ S⁺(c)), s ∉ S⁻(c),
  (F⁺(c) = ANY or f ∈ F⁺(c)), and f ∉ F⁻(c);
- species key `(s, any)`, whose representative is a form of *s* with no
  exact key: *c* with (S⁺(c) = ANY or s ∈ S⁺(c)), s ∉ S⁻(c), and
  F⁺(c) = ANY. Every form named negatively for *s* has an exact key, so the
  representative satisfies f ∉ F⁻(c) by construction;
- the generic bucket ("everything else", a species no key names): *c* with
  S⁺(c) = ANY. S⁻(c) is irrelevant because every negatively named species
  is a distinguished key.

This is exactly Golbat's probe order — exact, then species, then generic —
evaluated at compile time, so no bucket ever needs to inherit from another
at scan time and Golbat never has to merge anything.

**Emission.** For each conjunction in source order: a clause with no
`pokemon` list if it is in the generic bucket; and one clause whose
`pokemon` array lists every distinguished key whose bucket contains it, if
any. Then, for each distinguished key with an empty bucket, the block clause
`{"pokemon": [key], "iv": {"min": 1, "max": 0}}`, which can never hold and
so stops the fallback from applying anything to that key. Output size is at
most 2 × conjunctions + |D| clauses; identical conditions for many keys cost
one clause.

Worked check for `iv == 100 || (pokemon == 1 && gender == 2)`: c₁ has
S⁺ = ANY, c₂ has S⁺ = {1}; D = {(1, any)}; generic = {c₁}; bucket(1, any) =
{c₁, c₂}. Emission: c₁ → generic clause and a `(1)` clause; c₂ → a `(1)`
clause. That is the second example above.

### Limits

Defaults, overridable by option: 512 conjunctions after splitting; 10,000
emitted clauses; 64 KiB of expression text; parse depth as Expr's default.
Exceeding one is an error naming the limit. The server also caps request
body size. An expression with no satisfiable conjunction compiles to an empty
`filters` list and a warning that it matches nothing.

### Errors

One error type with a message and a source position (line, column, byte
offset), whether it comes from Expr (syntax, type) or from the compiler
(unsupported construct, unknown field, non-integer literal, `pokemon` out of
domain, form without species, a limit). The CLI prints the message with a
caret under the position; the server returns it as JSON.

## Library

Module `github.com/jfberry/golbat-filterc`, Go 1.24+, one dependency
(`github.com/expr-lang/expr`) plus the TOML parser for the CLI.

```go
package filterc

func Compile(expression string, opts ...Option) (*Compiled, error)
func WithMaxConjunctions(n int) Option
func WithMaxClauses(n int) Option

type Compiled struct {
    Filters  []Clause // v3 wire shape, json tags as the API expects
    Warnings []string
}
func (c *Compiled) Request(b Bounds, limit int) ScanRequest // {min, max, limit, filters}

type Clause struct {
    Pokemon []PokemonId `json:"pokemon,omitempty"`
    Iv, AtkIv, DefIv, StaIv, Level, Cp, Size *MinMax
    Gender []int8      `json:"gender,omitempty"`
    Little, Great, Ultra *MinMax // json: pvp_little, pvp_great, pvp_ultra
}

type Error struct { Msg string; Pos Position } // Position{Line, Column, Offset}
```

```go
package golbat

type Client struct { URL, Secret string; HTTP *http.Client }
func (c *Client) ScanPokemon(ctx context.Context, req filterc.ScanRequest) (*ScanResponse, error)

type ScanResponse struct {
    Pokemon      []json.RawMessage // passed through untouched
    Examined, Skipped, Total int
    LimitReached bool
}
```

The client sends `X-Golbat-Secret` and treats non-2xx as an error carrying
the status and body.

## CLI and server

```
filterc compile [flags] 'expression'     # print the v3 request body
filterc scan    [flags] 'expression'     # compile, call Golbat, print the response
filterc serve   [flags]                  # HTTP server
```

Flags: `--config path` (default `filterc.toml` in the working directory,
then `$XDG_CONFIG_HOME/filterc/filterc.toml`), `--min-lat --min-lon --max-lat
--max-lon`, `--limit`, `--golbat url`, `--secret`, `--listen addr`, `--json`
(compact output). Flags override the file; the file overrides built-in
defaults. `compile` needs no Golbat; `scan` and `serve`'s `/scan` need the
address and, if Golbat requires one, the secret.

Config file (TOML):

```toml
listen = ":8080"

[golbat]
url    = "http://127.0.0.1:9001"
secret = ""

[bounds]
min = { lat = 0.0, lon = 0.0 }
max = { lat = 0.0, lon = 0.0 }
limit = 3000
```

Server endpoints:

- `POST /compile` — body `{"expression": "…", "bounds"?: {min, max}, "limit"?: n}`;
  response `{"request": {min, max, limit, filters}, "warnings": [...]}`.
  Bounds and limit fall back to the config.
- `POST /scan` — same body; compiles, calls Golbat, responds
  `{"request": …, "response": <Golbat's v3 response>}`.
- `GET /healthz`.
- Errors: 400 `{"error": {"message", "position": {"line", "column"}}}` for
  compile errors; 502 with Golbat's status and body for upstream failures;
  413 over the body cap.

The server has no authentication of its own: it is a local tool, and
exposing it would expose the configured secret's scan capability. The README
says so.

## Testing

- **Golden tests.** The examples in this document and the recipes in
  Golbat's `api.md` compile to exact JSON; error cases produce the expected
  message and position.
- **Property test — the proof.** A generator produces random expressions
  (depth-limited; ids drawn from a small set so species overlap and negation
  bites; all operators and fields) and random rows (every field across its
  domain including the `−1` sentinels; PvP present with ranks including
  4096, or absent). For each pair, `matcher(compile(e), row)` must equal
  `reference(e, row)`. The **reference evaluator** is a hand-written
  three-valued evaluator over the checked AST, sharing no code with the NNF,
  DNF or dispatch stages. The **matcher** is Golbat's v3 `isPokemonDnfMatch`
  and its exact → species → generic probe chain, copied verbatim from
  `decoder/api_pokemon_scan_v3.go` and `api_pokemon_common.go` at a recorded
  commit, with a test that it reproduces the `api.md` recipes; it is
  re-synced by hand when Golbat's matcher changes.
- **Invariants**, checked on every property-test output: deterministic
  (compile twice, compare); both bounds on every range and within the
  domain; no `{id: 0}` and no form without an id; block clauses only for
  keys with empty buckets; clause count within the bound above.
- **Fuzz** the parser and compiler for panics and for the invariants.
- **End to end**, opt-in via the config file: compile a set of expressions,
  run each through `/scan` against a real Golbat, and check every returned
  pokemon against the reference evaluator. This is the check that the
  vendored matcher has not drifted.

## Extension points

- **Names and masterfile.** A resolver hook maps identifiers to ids before
  type checking (`PUMPKABOO` → 710, `FEMALE` → 2, `XXL` → 5). With a
  masterfile it can also expand a species-agnostic form predicate
  (`form_name == "ALOLAN"`) into the explicit `(species, form)` keys that have
  that form — a few thousand at most, coalesced by the `pokemon` array — and
  it must map Golbat's "form 0 = none sent" convention, which differs from
  masterfile default-form ids. Numeric ids always work without it.
- **Forts.** Golbat's fort filters are a plain OR over clauses with their
  own fields (type, raid level, quest reward, lure, incident, contest,
  station battle…). The pipeline is the same through DNF and splitting;
  there is no dispatch stage. The compiler is written with the field table
  and emitter as data (`internal/model`), so a fort model adds a field table,
  domains and an emitter, and the CLI grows `--entity fort`.
- **JSON-AST input** for clients that would rather build the boolean
  structure than a string: the lowering stage accepts it directly.

## Repository layout

```
go.mod
filterc/            compile.go  lower.go  nnf.go  dnf.go  dispatch.go
                    emit.go  domains.go  errors.go  request.go
                    reference_test.go  matcher_test.go (vendored Golbat matcher)
                    property_test.go  golden_test.go  fuzz_test.go
golbat/             client.go  types.go
cmd/filterc/        main.go  config.go  serve.go
docs/superpowers/specs/  this document, then the implementation plan
README.md  LICENSE (Unlicense, as Golbat)
```

## Decisions recorded

- Target the fallback model as documented in Golbat#420; do not propose
  union semantics or negation in Golbat's API. The compiler makes both
  available to users without changing the server.
- Expr for parsing and type checking only; a hand-written reference
  evaluator defines the semantics and the property test proves the compiler
  against it.
- Three-valued semantics for PvP, `−1`-as-value for encounter fields,
  because that is what the matcher does; documented rather than smoothed.
- Numeric ids in v1; names and masterfile as an optional resolver.
- Standalone repository under `jfberry`, library first, CLI and server as
  thin layers; no Golbat-side endpoint.
