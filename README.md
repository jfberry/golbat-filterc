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
