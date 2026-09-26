# golbat-filterc

Compiles filter expressions into [Golbat](https://github.com/UnownHash/Golbat)
pokemon scan requests.

Golbat's `POST /api/pokemon/v3/scan` takes a typed filter: clauses grouped by
the pokemon/form keys they list, where a pokemon is matched against the most
specific group that exists for it and within that group a clause matches if
all its conditions hold. That model is fast and can say "everything else",
but it has no negation and shared rules must be repeated under each species
that has rules of its own. `filterc` lets you write

```
size == 5 && pokemon != 710
iv == 100 || (pokemon == 1 && gender == 2)
```

and compiles it, once, into the typed request. Golbat is not changed and its
scan path is not touched.

It is a Go library first, with a CLI (`filterc compile`, `filterc scan`) and
a small server (`filterc serve`) that can also run the compiled scan against
a configured Golbat instance.

## Usage

```
go install github.com/jfberry/golbat-filterc/cmd/filterc@latest

filterc compile 'size == 5 && pokemon != 710'
filterc compile --min-lat 51.4 --min-lon -0.2 --max-lat 51.6 --max-lon 0.1 --limit 300 'iv == 100'
filterc scan --golbat http://127.0.0.1:9001 --secret … 'iv == 100 || (pokemon == 1 && gender == 2)'
filterc serve --listen 127.0.0.1:8080
```

`filterc.toml` (in the working directory or `$XDG_CONFIG_HOME/filterc/`)
holds the defaults; flags override it:

```toml
listen = "127.0.0.1:8080"

[golbat]
url    = "http://127.0.0.1:9001"
secret = ""

[bounds]
min = { lat = 51.4, lon = -0.2 }
max = { lat = 51.6, lon = 0.1 }
limit = 300
```

Unknown keys in the file are an error.

The server has no authentication of its own; `/scan` runs scans with the
configured secret, so keep it on a private interface. It listens on
`127.0.0.1:8080` by default; pass `--listen` (e.g. `--listen :8080`) only if
you deliberately want it reachable from other hosts.

- `POST /compile` `{"expression": "…", "bounds"?: {"min", "max"}, "limit"?: n}` → `{"request": {…}, "warnings": […]}`
- `POST /scan` — the same, plus `"response"`: Golbat's reply
- `GET /healthz`

## The language

Fields: `pokemon`, `form`, `iv`, `atk`, `def`, `sta`, `level`, `cp`,
`gender`, `size`, `little`, `great`, `ultra`. Operators: `== != < <= > >=`,
`in [..]`, `not in`, `a..b` ranges, `&& || !` (or `and or not`), parentheses.
Integers only. A form literal needs a `pokemon` id in the same conjunction.
Ranges and comparisons work on `pokemon` and `form` too (`pokemon in 1..151`,
`pokemon > 5`, `pokemon > 20000 && pokemon < 20010`, `form > 0`); each set
is keyed from its smaller side (its members or the species it excludes), and
a set too large either way hits the key or id cap. Compiles are bounded by
four caps, each checked before the allocation it guards: 512 conjunctions,
10,000 species/form keys, 100,000 pokemon entries and 10,000 clauses.

Semantics follow Golbat's matcher exactly, which the property test in
`filterc/property_test.go` checks: `-1` is a real value ("no encounter
data") for the encounter fields, so `!(iv >= 90)` includes un-encountered
pokemon; PvP ranks are absent for a pokemon with no PvP data (any comparison
on them is unknown, never true), ranks run 1..4096 and `4096` means
"unranked in that league". `iv` tops out at 100 and `atk`/`def`/`sta` at 15;
legacy rows Golbat holds with larger values are never returned.

The same holds inside a compound: `!(great <= 100 && iv == 100)` excludes a
perfect pokemon that has no PvP data. `iv != 100 || great > 100` has the same
limitation for its PvP half: "no PvP data" is not expressible in the v3
model.

Warnings (on stderr from the CLI, with a caret; in `"warnings"` from the
server) point at literals that compile but probably not as meant:

- a negated PvP condition (`!(great <= 100)`, `great != 4096`, `great not in 1..10`) never matches a pokemon without PvP data;
- a value outside the field's range makes a condition always or never hold (`iv < 200`, `iv == 200`), or clips a range (`iv in 50..200`);
- a comparison that covers the whole range or none of it, with the value in range, says so (`iv > 100`, `iv >= -1`); on PvP fields a whole-range condition (`great <= 4096`) means "has PvP data" and the warning says that instead;
- a reversed range is empty (`iv in 5..1`);
- a list member outside the field's range is ignored (`gender in [1, 7]`), and a list that leaves nothing or everything (`gender in []`, `gender in [7]`) says so;
- a species range below 1 ignores the ids under 1 (`pokemon in 0..3`);
- an expression that can never hold matches nothing.

Design: [docs/superpowers/specs/2026-09-25-filter-expression-compiler-design.md](docs/superpowers/specs/2026-09-25-filter-expression-compiler-design.md).
Background: [UnownHash/Golbat#417](https://github.com/UnownHash/Golbat/issues/417).
