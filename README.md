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

Status: design stage. The design is in
[docs/superpowers/specs/2026-09-25-filter-expression-compiler-design.md](docs/superpowers/specs/2026-09-25-filter-expression-compiler-design.md).
Background: [UnownHash/Golbat#417](https://github.com/UnownHash/Golbat/issues/417).
