package filterc

import "github.com/jfberry/golbat-filterc/internal/lang"

// The compiler's stages live in internal/lang; these names keep filterc's
// code and its white-box tests reading as they did.
type (
	interval    = lang.Interval
	intervalSet = lang.IntervalSet
	warnings    = lang.Warnings
	conjunction = lang.Conjunction
	node        = lang.Node
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
