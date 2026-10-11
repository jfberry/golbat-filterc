# Re-syncing with Golbat

This compiler is correct *relative to Golbat's matcher*. Its proof is a
property test that evaluates the compiled request through a verbatim copy of
that matcher. When Golbat changes the matcher, the lookup sentinels or the
documented semantics, the copy must be brought back in step or the proof is
proving the wrong thing. This file is the checklist. It has one section per
model; add a section when the fort model lands.

Golbat checkout assumed at `../Golbat` (any clone will do). Commands are from
this repo's root.

## Pokemon (v3 scan)

### 1. The matcher — `filterc/matcher_test.go`

Transcribed from two functions, at the Golbat commit named in the file header:

| Golbat source | What it is | Copied as |
|---|---|---|
| `decoder/api_pokemon_scan_v3.go`, the `isPokemonDnfMatch` closure inside `internalGetPokemonInArea3` | the clause match: every range fails on `< min` or `> max`; gender is list membership; every PvP condition fails when `pvpLookup == nil` | `isPokemonDnfMatch(row, *Clause)` |
| `decoder/api_pokemon_common.go`, the lookup chain inside `internalGetPokemonInArea` | the probe order: exact `{id, form}`, then `{id, -1}`, then `{-1, -1}`, stop at the first key that exists; the index build maps `id 0 → -1`, `form nil → -1` | `indexClauses`, `matchV3` |

Procedure:

```
git -C ../Golbat log --oneline -1 -- decoder/api_pokemon_scan_v3.go decoder/api_pokemon_common.go
git -C ../Golbat diff <header commit>..HEAD -- decoder/api_pokemon_scan_v3.go decoder/api_pokemon_common.go
```

If the diff touches either function (not just `doc:` tags), re-transcribe the
changed lines keeping Golbat's boolean structure (do not "clean it up"; the
copy is an oracle, and its shape is how a reader checks it against the
original), update the header commit, and run:

```
go test ./filterc/ -run 'MatcherRecipes|MatchesReference|Golden' -v
```

`TestMatcherRecipes` pins the `api.md` recipes against the copy; the property
test then tells you whether the compiler still agrees with Golbat. A property
failure after a matcher change means Golbat's semantics moved — bring the
compiler to Golbat, not the reverse, and record the change in the spec's
"Semantics" section.

### 2. The domains — `filterc/domains.go`

The table encodes what Golbat's lookup can hold. Cross-check each row against
`decoder/pokemonRtree.go`:

| Field(s) | Golbat helper | What to confirm |
|---|---|---|
| `iv` | `lookupIv` | null → −1; floor; saturates at 127 (we keep the top at 100 on purpose — see the spec's Domains) |
| `atk`, `def`, `sta`, `level`, `gender`, `size` | `lookupInt8` | null → −1; saturates at 127 |
| `cp` | `lookupInt16` | null → −1; saturates at 32767 |
| `form` | `lookupForm` | null → 0; never −1; saturates at 32767 |
| `pokemon` | `PokemonLookup.PokemonId` | 1..32767 (0 is not a species) |
| `little`, `great`, `ultra` | `calculatePokemonPvpLookup`, `HasPvp` | no PvP data → lookup is nil (every PvP condition fails); a league without a rank → 4096; ranks ≥ 1 and never above 4096 |

If a sentinel or ceiling changes, change the table, its comment, the spec's
Domains table, and the README's semantics paragraph; then `go test ./...`
(the warnings tests pin the range texts).

### 3. The documented semantics

Re-read Golbat's `api.md` "Filter semantics" (Golbat#420) and the `doc:` tags
on `ApiPokemonScan3.DnfFilters` / `ApiPokemonDnfFilter3` / `ApiPokemonDnfId`
in `decoder/api_pokemon_scan_v3.go` and `api_pokemon_common.go`, plus the
comments on `PokemonScanRequest.filters` / `DnfId` in `grpc/api.proto`. The
spec's Purpose section and the README's semantics paragraph must still be a
faithful summary. The precedence model (exact → species → everything else; no
server-side merging) is the compiler's entire design assumption: if Golbat
ever changes it, `dispatch.go` is wrong by construction and this is a
redesign, not a resync.

### 4. Wire shape

`filterc/types.go` must marshal exactly the JSON the endpoint accepts
(`pokemon` with `id`/`form`, `iv`, `atk_iv`, `def_iv`, `sta_iv`, `level`,
`cp`, `gender` list, `size`, `pvp_little`, `pvp_great`, `pvp_ultra`; both
bounds always sent). `golbat/client.go` decodes `pokemon`, `examined`,
`skipped`, `total`, `limit_reached`. Compare against `ApiPokemonDnfFilter3`
and `ApiPokemonScanResultV3` after any Golbat API change.

### 5. Golbat-side tests that must keep passing

Golbat's `decoder/api_pokemon_semantics_test.go` pins the precedence model in
Golbat itself (added with #420). If those tests change, the model changed:
stop and reassess before touching this repo.

## Forts (to be written with the fort model)

Will mirror the sections above for `decoder/api_fort.go` (`isFortDnfMatch`,
`combinedFortMatches`, `stationActiveAt`), `decoder/fortRtree.go`
(`FortLookup` and its sentinels), the fort `doc:` tags, and
`grpc/api.proto` (`FortDnfFilter`). Note the `now` argument: the fort oracle
is a function of time, so the resync must also confirm which timestamps are
compared and with which bounds (strict/non-strict).
