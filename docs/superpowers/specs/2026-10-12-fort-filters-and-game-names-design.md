# Fort filters and game names — design

Status: draft for review. Written 2026-10-12.

Extends the pokemon compiler
([2026-09-25-filter-expression-compiler-design.md](2026-09-25-filter-expression-compiler-design.md),
"the pokemon spec") with three new languages — gym, pokestop and station —
compiled to Golbat's single-type fort scans, and with game names (enum
identifiers from the game protos) in every language, pokemon included.
Everything in the pokemon spec's "Decisions recorded" still holds; this
document only adds to it.

Golbat sources are cited at `origin/main` `5cd3ed7`:
`decoder/api_fort.go` (`ApiFortScan`, `ApiFortDnfFilter`, `isFortDnfMatch`,
`matchDnfIdPair`, `matchContestFocus`, `stationActiveAt`, `internalGetForts`),
`decoder/fortRtree.go` (`FortLookup`, `updateGymLookup`,
`updatePokestopLookup`, `updateStationLookupWithBattles`),
`decoder/station_battle.go` (`FortLookupIncident`, `FortLookupStationBattle`,
`applyTopStationBattleToFortLookup`), `decoder/pokestop_showcase.go`
(`focusBuddy = "buddy"`) and `pogo/vbase.pb.go` (the game enums).

## Purpose

Golbat's fort scans take OR'd `ApiFortDnfFilter` clauses. Writing them by
hand has the same problems the pokemon model had — no negation, and
"everything except" has to be spelled out — plus fort-specific traps: an
omitted `filters` list matches every fort, an empty inner list matches
nothing, `is_ar_scan_eligible: false` behaves like `true`, conditions on a
raid, lure, showcase or battle silently require it to be live, and incident
and battle conditions in one clause must hold for the same incident or
battle. The compiler lets a person write

```
raid_level in [5, MEGA] && team != VALOR
lure in [GLACIAL, MOSSY] || quest(item == RARE_CANDY)
station_active && battle(level >= 3 && pokemon == ZAPDOS)
```

and emits the typed request, with every one of those traps either handled or
reported. Game names come from the same protos that give the fort enums their
domains, so they arrive together and apply to the pokemon language too
(`pokemon == PIKACHU && size == XXL`).

## Scope

Delivers:

- a model-driven front end shared by every language (`internal/lang`), with
  the pokemon compiler moved onto it unchanged;
- generated game data (`internal/gamedata`): value sets and names per enum,
  from Golbat's vendored protos, with a hand-maintained alias table;
- names in every language, pokemon included;
- three fort languages compiled to `POST /api/gym/scan`,
  `/api/pokestop/scan` and `/api/station/scan` (package `fortc`);
- the proof for forts: a vendored oracle, a reference evaluator with `now`,
  row generators, a property test, goldens, warnings tests, fuzz;
- client, CLI and server support (`--type`, `filterc names`).

Out of scope, with the extension kept:

- the combined `/api/fort/scan` (decided: one language per type, because the
  result sets differ). The emitter's output is exactly a combined scan
  group's `filters`, so a later combined mode only adds the request wrapper;
- species-agnostic form names (`form == ALOLA` for any species) and per-species
  form sets — these need the masterfile's species → forms map;
- enum domains for the pokemon model (it keeps its numeric domains; see
  "Names in the pokemon language");
- `updated_after` on fort requests (a request field the CLI can grow later).

## Shared front end

### `internal/lang`

Lowering, interval sets, NNF, DNF, split, positions, errors and the warnings
collector move from `filterc` into `internal/lang`, driven by a `Model`:

```go
type Model struct {
    Name   string        // "pokemon", "gym", …: used in messages
    Fields []Field       // plain fields
    Scopes []Scope       // incident, battle, quest (empty for pokemon/gym)
}

type Field struct {
    Name     string
    Kind     Kind          // Range | List | Bool | PairId | PairForm
    Domain   intervalSet   // values the field can meaningfully hold
    Enum     *gamedata.Enum // names, when the field is enum-valued
    Null     *NullGroup    // nil, or the group's messages (pvp, raid, lure, contest)
    Positive bool          // only the positive form compiles (ar_eligible, raid)
    Open     bool          // explicitly written values outside Domain extend it
}

type Scope struct {
    Name   string   // "incident"
    Fields []Field
    Groups [][]int  // fields that may be coupled in one scope literal
}
```

As built in phase 1: `Field` carries `Name`, `Kind` (`Range`/`List`/`Key` so far), `Domain`, `Null *NullGroup` (the group's three message texts: whole-domain verdict, negated-literal warning, never-holds note), `IDs`, `BadID` and `Below`; later phases add `Enum`, `Positive`, `Open`, the Bool and Pair kinds and scopes. `Conjunction` holds fixed `[MaxFields]` arrays (`MaxFields = 16`) rather than slices, so a copy never shares state; a model with more fields is rejected at construction — pokestop's plain fields plus a scope's fields must fit, which phase 3 checks (raise the constant if needed). The pokemon table stayed in `filterc/domains.go` (which builds `pokemonModel`); `validate` is `filterc/validate.go`.

`conjunction` becomes indexed by the model's fields (fixed `[MaxFields]` arrays,
not `[nFields]`) plus one slot per scope group. NNF, DNF and split are
unchanged in behaviour; they consult the model where they now consult the
package tables (`domains[f]`, `isPvp`, `isSpecies`). `filterc.Error` and
`filterc.Position` become aliases of the `internal/lang` types, so
`filterc`'s public API is unchanged.

`filterc` keeps the pokemon model (its field table and domains stay in
`filterc/domains.go`), dispatch, the v3 types and `Compile`. The new `fortc`
package holds the three fort models, the fort emitter and the fort wire
types.

**The refactor lands first and alone** (phase 1 below): every existing
golden, warnings and parse test passes byte-identically and the property
test passes with the same seed. No emitted JSON changes, so no golden
changes in that commit.

### Game data: `internal/gamedata`

`internal/cmd/genenums` reads Golbat's `pogo/vbase.pb.go` (parsed with
`go/parser`, never compiled or imported; default `-golbat ../Golbat
-rev HEAD`, read with `git show <rev>:pogo/vbase.pb.go` so the checkout's
working tree is not touched) and writes `internal/gamedata/enums_gen.go`:
for each enum it uses, the sorted `(value, name)` pairs from its `_name`
map, plus the Golbat commit it was generated from. `go generate
./internal/gamedata` runs it. The generated file is committed; nothing at
build time needs a Golbat checkout.

Enums used (sizes at `5cd3ed7`):

| Enum | Values | Used by |
|---|---|---|
| `HoloPokemonId` | 1,026 (0–1025) | pokemon names; fort pair ids |
| `PokemonDisplayProto_Form` | 1,506 (0–3380, gaps) | form names; fort pair forms |
| `PokemonDisplayProto_Gender` | 4 | pokemon `gender` names |
| `HoloPokemonSize` | 6 | pokemon `size` names |
| `Team` | 4 | `team` |
| `RaidLevel` | 30 (0–29) | `raid_level` |
| `HoloTemporaryEvolutionId` | 5 | `raid_evolution` |
| `Item` restricted to `ITEM_TROY_DISK*` | 6 (501–506) | `lure` |
| `Item` | 292 (0–2306, gaps) | `quest(item)` |
| `QuestRewardProto_Type` | 21 | `quest(type)` |
| `IncidentDisplayType` | 14 | `incident(display_type)` |
| `EnumWrapper_InvasionCharacter` | 123 (0–527, gaps) | `incident(character)` |
| `HoloPokemonType` | 19 | `contest_type` |
| `ContestRankingStandard` | 3 | `contest_ranking` |
| `BreadBattleLevel` | 9 | `battle(level)` |

Every enum's set includes its zero (`*_UNSET`, `MISSINGNO`, `NONE`) because
Golbat's lookup stores nulls as 0 (`ValueOrZero` throughout
`fortRtree.go`), so 0 is a value a fort can have.

### Names

A **name** is a bare identifier on the value side of a comparison or in a
list, resolved in the namespace of the field it is compared with:

```
lure == GLACIAL            lure in [glacial, mossy]       GLACIAL == lure
quest(item == GLACIAL)     team != VALOR                  pokemon == PIKACHU
```

- **Spelling.** A field's namespace holds, for each enum value: the full proto
  name (`ITEM_TROY_DISK_GLACIAL`); the name with the field's prefix stripped
  (`GLACIAL`), unless stripping leaves nothing or something that starts with
  a digit (`RAID_LEVEL_5` → no stripped name; write `5`); and any aliases.
  Matching is case-insensitive.
- **Prefixes** (one per field; in `internal/gamedata/names.go`, hand-maintained):
  `FORM_` (form), `GENDER_`, `POKEMON_SIZE_`, `TEAM_`, `RAID_LEVEL_`,
  `TEMP_EVOLUTION_`, `ITEM_TROY_DISK_` (lure), `ITEM_` (quest item),
  `INCIDENT_DISPLAY_TYPE_`, `CHARACTER_`, `POKEMON_TYPE_`,
  `CONTEST_RANKING_STANDARD_`, `BREAD_BATTLE_LEVEL_`; none for pokemon ids or
  quest reward types.
- **Aliases** (same file) cover names players use that the protos lack:
  `MYSTIC`/`VALOR`/`INSTINCT` and `NEUTRAL` for teams, `NORMAL` for lure 501
  (whose stripped name is empty), `EGG` for `raid_pokemon` 0, and readable
  names for the raid levels and battle levels whose stripped name starts
  with a digit (`SHADOW_5` for `RAID_LEVEL_5_SHADOW`, …). The alias list is
  part of the spec's review surface; the plan enumerates it.
- **Collisions.** A name that maps to two values in one field's namespace is a
  generator error (and a test over the committed data), never a silent
  choice. A name that is also a field of the same model, or an Expr keyword
  in any case (`nil`, `true`, `false`, `in`, `not`, `and`, `or`, `let`,
  `matches`, `contains`, `startsWith`, `endsWith`), is dropped from the
  namespace with a test listing every such drop; the full proto name still
  works. Expr's builtin function names (`type`, `min`, `max`, …) parse as
  plain identifiers outside a call and need no special case (checked
  against the parser while writing this spec).
- **Which side is the field.** In a comparison, an identifier that is a field
  of the current model (or scope) is the field; the other side is a value. If
  both sides are fields, it is an error (`field-to-field comparison is not
  supported`). If neither is, it is an error at the left side (`expected a
  field on one side of ==`), as now.
- **Unknown name**: error at the name, with up to three closest names in that
  field's namespace (edit distance) — `unknown lure "GLACIEL"; did you mean
  GLACIAL?`. A name on a field with no enum (`iv == HIGH`) is an error saying
  the field takes numbers.
- **Messages and warnings name the literal as written** (`lure != GLACIAL`), as
  now; where a range or domain is printed, enum values are printed by name.
- `filterc names [--type t] [field]` lists a field's names and values.

Names resolve to integers during lowering; nothing after lowering knows they
existed. An expression written with numbers compiles to the same output as
before.

#### Names in the pokemon language

`pokemon`, `form`, `gender` and `size` accept names. Their domains stay
numeric (the pokemon spec's table); names only resolve. `gender` −1 and the
encounter −1 sentinels have no name. `form == RATTATA_ALOLA` works without a
masterfile because form ids are global; the existing rule (a form needs a
positive species constraint in the conjunction) is unchanged.

### Enum domains and unknown values (fort languages)

A fort enum field's domain is its enum's value set. So `lure != GLACIAL` is
`[501, 503, 504, 505, 506]`, and the largest complement of any enum field is
under 1,600 values.

Fields are **open**: a value written explicitly (`==`, `!=`, or a list
member) that is not in the enum — a lure the protos do not know yet — is kept
and added to that field's domain for this compile, with a warning: `lure ==
507: 507 is not a lure in the game data (Golbat 5cd3ed7); it is kept`.
Ranges and ordered comparisons are clipped to the domain, as now.

The extension is per compile: models are shared package-level tables (and `serve` compiles concurrently), so the compiler applies it as a per-compile domain overlay, never by mutating the model. Every domain check in lowering — explicit-id checks and the "outside the range" warnings — uses membership (`Contains`) so gaps count as outside, and prints enum domains by name rather than as `lo..hi`; phase 1's checks compare against `Lo()`/`Hi()`, which is only right for single-interval domains.

**A fort whose value for a field is outside the field's domain (after that
extension) is NULL for that field**: every condition on it is UNKNOWN. This is
exactly what the emitted lists do (a complement never lists an unknown
value), and it is the same rule as PvP and inactive raids, so it is one line
of the reference evaluator. Regenerating the game data moves such values
into the domain; the resync checklist says so.

## The fort languages

### One rule decides how a condition is written

- **A single value that may be absent is a plain field, NULL while absent.**
  Three-valued logic, exactly as PvP in the pokemon spec. The NULL groups:
  - **raid** (`raid_level`, `raid_pokemon`, `raid_form`, `raid_evolution`,
    `raid`): NULL unless the raid is active, `RaidBattleTimestamp > now ||
    RaidEndTimestamp > now` — so eggs count as active raids.
  - **lure** (`lure`): NULL unless `LureExpireTimestamp > now`.
  - **contest** (`contest_pokemon`, `contest_form`, `contest_type`,
    `contest_ranking`, `contest_buddy_level`): NULL unless `ShowcaseExpiry >
    now`; `contest_buddy_level` is also NULL when `ShowcaseBuddyMinLevel <=
    0` (`matchContestFocus`).

  So `raid_level != 5` is "an active raid that is not level 5", and
  `!(raid_level == 5)` never matches a gym with no active raid — warned, as
  negated PvP is.
- **Several values are a scope: "some item such that …".** `incident(…)`,
  `battle(…)`, `quest(…)`. Inside the parentheses the scope's own fields are
  used with `&& || !` freely; `||` inside a scope is lifted out
  (`incident(a || b)` ≡ `incident(a) || incident(b)`). A scope literal holds
  for a fort when some live item satisfies its inside:
  - **incident**: some incident with `ExpireTimestamp > now`.
  - **battle**: some battle with `BattleEndTimestamp > now`, the battles
    being `StationBattles`, or — when that slice is empty — the single
    legacy battle in the lookup's own `Battle*` fields.
  - **quest**: the AR quest or the no-AR quest. Quests are not time-gated
    and have no absent state (a stop without a quest has type 0, amount 0);
    only the out-of-domain rule below makes a quest value NULL. A consequence a map operator may not expect: `quest(item != RARE_CANDY)` holds for a stop with no quest (item 0); the compiler warns when a negated quest condition's complement contains 0 (the no-quest value), suggesting `quest(type == ITEM) && quest(item != …)`.

  Conditions inside one scope literal are coupled to the same item only
  where Golbat couples them: incident `display_type` with `character`;
  battle `level` with `pokemon`/`form`; quest `pokemon` with `form` only
  (Golbat checks each quest condition against either quest independently).

  Rules, each an error at the offending position with the rewrite in the
  message:
  - a scope cannot be negated (`!incident(…)`): "no incident is a grunt"
    cannot be expressed as clauses;
  - one conjunction may hold at most one scope literal per coupling group —
    one `incident(…)`, one `battle(…)`, and one `quest(…)` each for type,
    amount, item and pokemon/form. A second one would mean "some other
    item", which no clause can say, and intersecting them would be wrong
    (`quest(type in [2,3]) && quest(type in [3,4])` holds for a stop whose
    AR quest is type 2 and no-AR quest is type 4, but the intersection `{3}`
    does not);
  - inside `quest(…)`, conditions on more than one group are an error:
    `quest(type == STARDUST && amount >= 1000)` → write
    `quest(type == STARDUST) && quest(amount >= 1000)`, and the message says
    the two may be satisfied by different quests.

  Negation *inside* a scope is ordinary complement within the domain:
  `incident(character != CLIFF)` is "some incident whose character is not
  Cliff".

### Fields

Language name → wire field. Kinds: **R** range (`ApiFortDnfMinMax`), **L**
list, **B** boolean (`*bool`), **P** pokemon/form pair (`ApiDnfId` list).

**gym**

| Field | Kind | Domain | Wire | Notes |
|---|---|---|---|---|
| `team` | L | `Team` | `team_id` | |
| `slots` | R | 0..6 | `available_slots` | |
| `ar_eligible` | B, positive only | | `is_ar_scan_eligible: true` | `!ar_eligible` is an error: Golbat only tests that the field is present |
| `raid` | B, positive only | | `raid_level`: whole domain | "has an active raid"; `!raid` is an error (no clause says "no active raid") |
| `raid_level` | L, raid | `RaidLevel` | `raid_level` | |
| `raid_pokemon`, `raid_form` | P, raid | `HoloPokemonId`, `PokemonDisplayProto_Form` | `raid_pokemon_id` | 0 = egg / unknown boss (`EGG`) |
| `raid_evolution` | L, raid | `HoloTemporaryEvolutionId` | `raid_temp_evolution_id` | 0 = base form |

**pokestop**

| Field | Kind | Domain | Wire | Notes |
|---|---|---|---|---|
| `ar_eligible` | B, positive only | | `is_ar_scan_eligible: true` | as gym |
| `lure` | L, lure | lure items | `lure_id` | |
| `contest_pokemon`, `contest_form` | P, contest | as raid | `contest_pokemon` | |
| `contest_type` | L, contest | `HoloPokemonType` | `contest_pokemon_type` | |
| `contest_ranking` | L, contest | `ContestRankingStandard` | `contest_ranking_standard` | 0 = unknown standard |
| `contest_buddy_level` | L, contest | 1..127 | `contest_focus: [{"type": "buddy", "min_level": n}, …]` | |
| `quest(type)` | L | `QuestRewardProto_Type` | `quest_reward_type` | |
| `quest(amount)` | R | 0..32767 | `quest_reward_amount` | |
| `quest(item)` | L | `Item` | `quest_reward_item_id` | |
| `quest(pokemon, form)` | P | as raid | `quest_reward_pokemon` | |
| `incident(display_type)` | L | `IncidentDisplayType` | `incident_display_type` | |
| `incident(character)` | L | `EnumWrapper_InvasionCharacter` | `incident_character` | |

**station**

| Field | Kind | Domain | Wire | Notes |
|---|---|---|---|---|
| `station_active` | B | | `station_active` | `!IsInactive && start < now && end > now` (strict); both polarities compile |
| `battle_available` | B | | `battle_available` | the decoded flag; says nothing about liveness |
| `stationed_gmax` | B | | `stationed_gmax` | `TotalStationedGmax > 0` |
| `battle(level)` | L | `BreadBattleLevel` | `battle_level` | |
| `battle(pokemon, form)` | P | as raid | `battle_pokemon` | |

`ar_eligible` is not offered on stations: `updateStationLookupWithBattles`
never sets `IsArScanEligible`, so it would never hold.

A field of another type (`team` in a pokestop expression) is `unknown field
"team" for pokestop; it is a gym field`.

### Literals

- Comparisons, `in`, `not in` and ranges work on list and range fields as on
  pokemon fields; names work wherever a number does.
- Booleans are written bare or negated: `station_active`, `!stationed_gmax`.
  `station_active == true` is an error suggesting the bare form. A boolean
  field elsewhere in a comparison (`station_active > 1`) is an error.
- Pairs: `raid_pokemon` and `raid_form` are separate fields, like `pokemon`
  and `form`; a form condition needs a pokemon condition in the same
  conjunction (or scope literal). There is no small-side rule — the emitter
  enumerates `S × F` — so `raid_pokemon != MEWTWO && raid_form == 0` is fine
  (it lists 1,025 ids, under the cap).
- A scope is written `name(expression)` (an Expr call with one argument);
  any other call, or a scope name used as a field, is an error.

## Compiling

### Pipeline (`fortc.Compile`)

1. **Lower** with the type's model. Names resolve here. A pre-pass collects
   explicitly written values per open field so complements are taken within
   the extended domain. Leaves are plain literals (interval sets) or scope
   literals (a lowered subtree over the scope's fields). A positive-only
   field written `!=` or `not in` is an error here; one under an enclosing
   `!` is caught in NNF.
2. **NNF.** `!` on a plain literal is complement within its domain; on a
   scope literal or a positive-only field it is an error at its position.
   Each scope literal's inside is put into NNF independently.
3. **DNF.** Each scope literal's inside is put into DNF and each of its
   conjunctions becomes one scope literal; then the outer expression is
   distributed as now, a conjunction holding plain field sets plus at most
   one scope literal per coupling group. A second literal for an occupied
   group is an error naming both positions. Empty sets drop the
   conjunction (or the scope literal, and with it its conjunction) at once.
   The conjunction cap applies across the outer expansion.
4. **Split.** `slots` and `quest(amount)` hold one `{min, max}` each, so a
   set of *n* intervals becomes *n* conjunctions (cartesian product, under
   the cap). Lists and pairs are not split.
5. **Validate.** A form condition without a pokemon condition in its
   conjunction or scope literal is an error at the first form literal.
6. **Emit** one `ApiFortDnfFilter` per conjunction, in source order;
   identical clauses are skipped.
   - list field: its set, enumerated in ascending order;
   - range field: `{min, max}` with both bounds;
   - pair: for each id in `S`, `{pokemon_id: id}` if form is unconstrained,
     else `{pokemon_id: id, form: f}` for each `f` in `F`;
   - boolean: `true`/`false`; `ar_eligible` → `is_ar_scan_eligible: true`;
     `raid` → `raid_level` as the whole `RaidLevel` domain (intersected with
     any `raid_level` literal in the conjunction);
   - `contest_buddy_level` → `contest_focus` entries.

The compiled request does not depend on `now`; only the oracle and the
reference do.

### Edge outputs

- **Never holds** (no conjunction survives): `filters` is one clause that can
  never match, using Golbat's rule that an explicitly empty list matches
  nothing — `[{"team_id": []}]` (gym), `[{"lure_id": []}]` (pokestop),
  `[{"battle_level": []}]` (station) — and the warning "the expression can
  never hold; the request matches nothing". An omitted or empty `filters`
  would match every fort, so it is never emitted.
- **Always holds** (a conjunction with no constraints): `filters` is
  `[{}]`, explicitly, and other clauses are dropped (they are subsumed).
- The wire types distinguish an omitted list from an empty one (`nil` →
  omitted; non-nil empty → `[]`); the emitter produces an empty list only in
  the never-clause.

### Limits

| Cap | Default | Option | Checked |
|---|---|---|---|
| conjunctions after splitting | 512 | `WithMaxConjunctions` | as now |
| emitted clauses | 10,000 | `WithMaxClauses` | before each non-duplicate clause is appended |
| entries in one list or pair field of one clause | 2,000 | `WithMaxListEntries` | from interval sizes (`|S|·|F|` for pairs) before enumeration |
| entries across all clauses | 100,000 | `WithMaxEntries` | summed from sizes before any enumeration, then before each append |

Golbat checks fort lists with `slices.Contains` and `matchDnfIdPair`, a
linear scan per fort per clause, so these caps protect its scan path as well
as the compiler. With enum domains the largest realistic lists are
`raid_pokemon != MEWTWO` (1,025 ids) and `raid_pokemon == MEWTWO && raid_form
!= 0` (1,505 entries — every form in the game, not just Mewtwo's; per-species
form sets need the masterfile). Both pre-checks are exact lower bounds from
interval sizes, as in the pokemon compiler. Exceeding one is an error naming
the field: `raid_form != 0 lists 1505 forms of MEWTWO; list the forms you
want`.

### Warnings

New, each with a row in `fortc/warnings_test.go` pinning text and position
and a line in the README:

- **negated NULL-able condition**, one wording per group:
  `raid_level != 5: never matches a gym without an active raid`, likewise
  "without an active lure" and "without an active showcase";
- **value not in the game data** (open fields), at the value;
- **quest conditions in one conjunction** (`quest(type == X) && quest(amount
  >= n)`): "these may be satisfied by different quests (AR and no-AR)";
- **never holds**, as above;
- **negated quest condition including the no-quest value** (`quest(item != RARE_CANDY)`): matches stops without a quest.

The pokemon warnings (outside the domain, whole/empty, clipped, reversed
range, list member outside the domain, list verdicts) apply per field, with
enum values printed by name. On a NULL-able field a whole-domain literal is
not a no-op and says so ("holds for every gym with an active raid"), as on
PvP fields.

## The proof

The pokemon rule carries over: **if the property test fails, the compiler is
wrong.** Never adjust the oracle, the reference, the generator or the
invariants to make it pass.

- **Oracle — `fortc/matcher_test.go`.** `isFortDnfMatch`, `matchDnfIdPair`,
  `matchContestFocus` and `stationActiveAt` transcribed verbatim from Golbat
  at the commit in the file header, keeping Golbat's boolean structure; the
  single-type loop from `internalGetForts` (empty `filters` → match the type,
  else OR); `FortLookup`, `FortLookupIncident`, `FortLookupStationBattle`
  copied as plain structs; `contestFocusType` as the string `"buddy"`.
  `TestMatcherRecipes` pins against the copy: empty `filters` matches every
  fort of the type; an empty inner list matches nothing;
  `is_ar_scan_eligible: false` behaves like `true`; raid active is `battle >
  now || end > now`; lure, showcase, incident and battle expiry fail at `<=
  now`; `station_active` has strict bounds; the legacy single-battle path
  applies when `StationBattles` is empty.
- **Reference — `fortc/reference_test.go`.** A three-valued evaluator over
  the Expr AST, taking `now`, sharing no code with `internal/lang` or the
  emitter. It may read `internal/gamedata` (the domains and names are part of
  the language's definition, not the compiler). NULL groups and
  out-of-domain values are UNKNOWN; scopes are an existential (Kleene OR)
  over the live items. It evaluates only expressions that compile; the
  scope rules are errors, pinned by parse tests.
- **Rows — `fortc/row_test.go`.** One generator per type. Every timestamp is
  drawn from `{now−1, now, now+1}` plus far past and far future, so every
  strict and non-strict boundary is exercised; values cover each domain's
  edges, 0, the values written in the expression, and out-of-domain values
  the lookup can hold (raid level 99, team 7, an unknown lure); stops get
  0–3 incidents and stations 0–3 battles, or the legacy battle only, so
  same-item coupling is tested.
- **Property — `fortc/property_test.go`.** Random expressions per type over
  every field, operator, name, scope, boolean and nested `!`; expressions
  rejected with an expected scope, polarity or cap error are counted and
  skipped (the count is logged, as now). For each expression and row:
  `match(compile(e), row, now) == (reference(e, row, now) == TRUE)`.
  Invariants on every output: deterministic; both bounds on every range;
  no empty list outside the never-clause; never an omitted or empty
  `filters`; no form without an id; list lengths within the caps; only the
  type's own fields.
- **Goldens — `fortc/golden_test.go`.** Exact JSON for every example in this
  document, the never and always clauses, scope lifting, pair emission,
  `raid`, `contest_buddy_level`, and named vs numbered inputs (same output).
- **Names.** `internal/gamedata` tests: no collisions in any namespace; every
  alias resolves to a value in its enum; the generator is deterministic and
  reproduces the committed file. Pokemon goldens gain named inputs; existing
  goldens are unchanged.
- **Fuzz** `FuzzCompile` per type for panics and the invariants.

## API, client, CLI, server

```go
package fortc

type Type uint8 // Gym, Pokestop, Station
func Compile(t Type, expression string, opts ...Option) (*Compiled, error)
func WithMaxConjunctions(n int) Option
func WithMaxClauses(n int) Option
func WithMaxListEntries(n int) Option
func WithMaxEntries(n int) Option

type Compiled struct {
    Type     Type
    Filters  []Clause // ApiFortDnfFilter wire shape
    Warnings []string
}
func (c *Compiled) Request(b filterc.Bounds, limit int, withIncidents bool) ScanRequest
// {min, max, limit, filters, with_incidents (pokestop only)}
```

`golbat.Client` gains `ScanGyms`, `ScanPokestops`, `ScanStations`, each
returning the results as `[]json.RawMessage` (passed through) with
`examined`, `skipped`, `total`, `limit_reached`.

CLI: `--type pokemon|gym|pokestop|station` (default `pokemon`) on `compile`
and `scan`; `--with-incidents` (pokestop only; an error with another type);
`filterc names [--type t] [field]`. Server: `"type"` and `"with_incidents"`
in the `/compile` and `/scan` bodies, defaulting to pokemon and false; the
response shape is unchanged. The server keeps its loopback default and no
auth.

## Order of work

Four phases, each with its own plan (written against the code the previous phase produced) and its own PR, green on its own:

1. **Refactor** to `internal/lang` and a pokemon `Model`. No golden changes;
   every test byte-identical.
2. **Game data and names**: `genenums`, the committed `enums_gen.go`,
   `names.go` (prefixes and aliases), the resolver in lowering, names in the
   pokemon language, `filterc names` for pokemon. New goldens for named
   inputs only. `docs/resync.md` gains "Game data".
3. **Forts**: the three models, scope literals, the emitter, never/always
   clauses, caps, warnings, the oracle, the reference with `now`, row
   generators, property test, goldens, fuzz. `docs/resync.md` "Forts" is
   written.
4. **Plumbing and docs**: client, CLI and server `--type`, `--with-incidents`,
   `names` for fort types; README sections per type; CLAUDE.md and
   `docs/handover.md` updated.

## Resync additions

- **Game data.** `go generate ./internal/gamedata` against the new Golbat
  commit; review the diff of `enums_gen.go` (new values widen domains and
  stop being NULL; a renamed or removed name breaks expressions that used
  it — call that out in the release notes); run the gamedata tests and the
  fort property tests.
- **Forts.** As for pokemon: diff `decoder/api_fort.go`,
  `decoder/fortRtree.go` and `decoder/station_battle.go` since the oracle's
  header commit; re-transcribe changed lines verbatim; confirm which
  timestamps are compared and whether strictly; re-read the `doc:` tags on
  `ApiFortDnfFilter` and `FortDnfFilter` in `grpc/api.proto`; check the wire
  types against `ApiFortDnfFilter`, `ApiFortDnfMinMax`, `ApiDnfId`,
  `ApiFortDnfContestFocus`.

## Decisions recorded

- One language per fort type, targeting the single-type scans, because the
  result sets differ; the combined scan is a later wrapper.
- A single value that may be absent is a NULL-able plain field (three-valued,
  as PvP); several values are a scope, coupled exactly where Golbat couples
  them; scopes cannot be negated.
- Quests are a scope with one coupling group per condition, because Golbat
  checks each quest condition against either quest independently.
- Fort enum domains are the game's enum sets, generated from Golbat's
  vendored protos; fields are open to explicitly written unknown values;
  out-of-domain row values are NULL.
- Names are a front-end feature in every language: proto names, prefix
  stripped, case-insensitive, plus a reviewed alias table.
- `ar_eligible` and `raid` are positive-only because their negations cannot
  be expressed in Golbat's filter.
- A never-holding expression emits an explicit never-clause, an always-holding
  one emits `[{}]`; `filters` is never omitted or empty.
- List caps exist to protect Golbat's linear list checks as well as the
  compiler.
