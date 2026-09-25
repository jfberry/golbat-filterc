package filterc

import (
	"testing"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// tv is a three-valued truth value (SQL style): comparisons on a missing
// PvP rank are unknown, not unknown is unknown, and/or follow Kleene.
type tv int8

const (
	tFalse tv = iota
	tTrue
	tUnknown
)

func kleeneAnd(a, b tv) tv {
	if a == tFalse || b == tFalse {
		return tFalse
	}
	if a == tTrue && b == tTrue {
		return tTrue
	}
	return tUnknown
}

func kleeneOr(a, b tv) tv {
	if a == tTrue || b == tTrue {
		return tTrue
	}
	if a == tFalse && b == tFalse {
		return tFalse
	}
	return tUnknown
}

func kleeneNot(a tv) tv {
	switch a {
	case tTrue:
		return tFalse
	case tFalse:
		return tTrue
	}
	return tUnknown
}

func boolTV(b bool) tv {
	if b {
		return tTrue
	}
	return tFalse
}

// reference evaluates src for r straight from Expr's AST. It is the
// definition of the language the compiler is tested against.
func reference(t *testing.T, src string, r row) tv {
	t.Helper()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("reference: parse %q: %v", src, err)
	}
	return refNode(t, tree.Node, r)
}

func refNode(t *testing.T, n ast.Node, r row) tv {
	t.Helper()
	switch v := n.(type) {
	case *ast.BinaryNode:
		switch v.Operator {
		case "and", "&&":
			return kleeneAnd(refNode(t, v.Left, r), refNode(t, v.Right, r))
		case "or", "||":
			return kleeneOr(refNode(t, v.Left, r), refNode(t, v.Right, r))
		case "==", "!=", "<", "<=", ">", ">=":
			return refCompare(t, v, r)
		case "in":
			return refIn(t, v, r)
		}
	case *ast.UnaryNode:
		if v.Operator == "not" || v.Operator == "!" {
			return kleeneNot(refNode(t, v.Node, r))
		}
	}
	t.Fatalf("reference: unsupported node %s", n.String())
	return tUnknown
}

func refInt(t *testing.T, n ast.Node) int {
	t.Helper()
	switch v := n.(type) {
	case *ast.IntegerNode:
		return v.Value
	case *ast.UnaryNode:
		if inner, ok := v.Node.(*ast.IntegerNode); ok && v.Operator == "-" {
			return -inner.Value
		}
	}
	t.Fatalf("reference: not an integer: %s", n.String())
	return 0
}

func refField(t *testing.T, n ast.Node, r row) (int, bool) {
	t.Helper()
	id, ok := n.(*ast.IdentifierNode)
	if !ok {
		t.Fatalf("reference: not a field: %s", n.String())
	}
	return r.get(fieldByName[id.Value])
}

func refCompare(t *testing.T, v *ast.BinaryNode, r row) tv {
	t.Helper()
	fieldSide, valueSide, op := v.Left, v.Right, v.Operator
	if _, ok := v.Left.(*ast.IdentifierNode); !ok {
		fieldSide, valueSide, op = v.Right, v.Left, flip(op)
	}
	x, known := refField(t, fieldSide, r)
	if !known {
		return tUnknown
	}
	y := refInt(t, valueSide)
	switch op {
	case "==":
		return boolTV(x == y)
	case "!=":
		return boolTV(x != y)
	case "<":
		return boolTV(x < y)
	case "<=":
		return boolTV(x <= y)
	case ">":
		return boolTV(x > y)
	}
	return boolTV(x >= y)
}

func refIn(t *testing.T, v *ast.BinaryNode, r row) tv {
	t.Helper()
	x, known := refField(t, v.Left, r)
	if !known {
		return tUnknown
	}
	switch right := v.Right.(type) {
	case *ast.ArrayNode:
		for _, e := range right.Nodes {
			if refInt(t, e) == x {
				return tTrue
			}
		}
		return tFalse
	case *ast.BinaryNode:
		if right.Operator == ".." {
			return boolTV(refInt(t, right.Left) <= x && x <= refInt(t, right.Right))
		}
	}
	t.Fatalf("reference: bad membership %s", v.String())
	return tUnknown
}

func TestReferenceSemantics(t *testing.T) {
	noPvp := row{pokemonId: 1, iv: 100}
	unranked := row{pokemonId: 1, iv: -1, pvp: &pvpRow{little: 4096, great: 4096, ultra: 4096}}
	cases := []struct {
		src  string
		r    row
		want tv
	}{
		{"great <= 100", noPvp, tUnknown},
		{"!(great <= 100)", noPvp, tUnknown},
		{"great <= 100 || iv == 100", noPvp, tTrue},
		{"great <= 100 && iv == 100", noPvp, tUnknown},
		{"!(great <= 100)", unranked, tTrue},
		{"great == 4096", unranked, tTrue},
		{"!(iv >= 90)", unranked, tTrue},
		{"iv in -1..100", unranked, tTrue},
		{"pokemon not in [1, 4]", noPvp, tFalse},
		{"100 == iv", noPvp, tTrue},
	}
	for _, c := range cases {
		if got := reference(t, c.src, c.r); got != c.want {
			t.Errorf("%q on %+v = %v, want %v", c.src, c.r, got, c.want)
		}
	}
}
