package filterc

import (
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/file"
	"github.com/expr-lang/expr/parser"
)

const (
	maxExpressionBytes = 64 << 10
	maxLiteral         = 1 << 20 // keeps v±1 arithmetic far from overflow
)

// parse turns expression source into the boolean tree, using Expr's parser
// and accepting only the subset the compiler understands. The whitelist walk
// is the type check: every accepted node is a comparison between a known
// field and an integer, or and/or/not over such comparisons.
func parse(src string) (node, error) {
	if len(src) > maxExpressionBytes {
		return nil, errorf(Position{Line: 1, Column: 1}, "expression longer than %d bytes", maxExpressionBytes)
	}
	tree, err := parser.Parse(src)
	if err != nil {
		if fe, ok := err.(*file.Error); ok {
			return nil, &Error{Msg: fe.Message, Pos: Position{Line: fe.Line, Column: fe.Column + 1, Offset: byteOffset(src, fe.From)}}
		}
		return nil, &Error{Msg: err.Error(), Pos: Position{Line: 1, Column: 1}}
	}
	l := lowerer{src: tree.Source, raw: src}
	return l.boolean(tree.Node)
}

type lowerer struct {
	src file.Source
	raw string
}

func (l *lowerer) pos(n ast.Node) Position {
	e := &file.Error{Location: n.Location()}
	e.Bind(l.src)
	return Position{Line: e.Line, Column: e.Column + 1, Offset: byteOffset(l.raw, n.Location().From)}
}

func (l *lowerer) boolean(n ast.Node) (node, error) {
	switch v := n.(type) {
	case *ast.BinaryNode:
		switch v.Operator {
		case "and", "&&":
			return l.pair(v, func(a, b node) node { return &andNode{a, b} })
		case "or", "||":
			return l.pair(v, func(a, b node) node { return &orNode{a, b} })
		case "==", "!=", "<", "<=", ">", ">=":
			return l.comparison(v)
		case "in":
			return l.membership(v)
		}
		return nil, errorf(l.pos(n), "unsupported operator %q", v.Operator)
	case *ast.UnaryNode:
		if v.Operator != "not" && v.Operator != "!" {
			return nil, errorf(l.pos(n), "unsupported operator %q", v.Operator)
		}
		if !isBoolean(v.Node) {
			return nil, errorf(l.pos(n), "%s applies to a comparison; write %s (…)", v.Operator, v.Operator)
		}
		x, err := l.boolean(v.Node)
		if err != nil {
			return nil, err
		}
		return &notNode{x}, nil
	case *ast.IdentifierNode, *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode, *ast.StringNode:
		return nil, errorf(l.pos(n), "expected a comparison")
	}
	return nil, errorf(l.pos(n), "unsupported construct %s", n.String())
}

func isBoolean(n ast.Node) bool {
	switch v := n.(type) {
	case *ast.BinaryNode:
		switch v.Operator {
		case "and", "&&", "or", "||", "==", "!=", "<", "<=", ">", ">=", "in":
			return true
		}
	case *ast.UnaryNode:
		return v.Operator == "not" || v.Operator == "!"
	}
	return false
}

func (l *lowerer) pair(v *ast.BinaryNode, mk func(a, b node) node) (node, error) {
	a, err := l.boolean(v.Left)
	if err != nil {
		return nil, err
	}
	b, err := l.boolean(v.Right)
	if err != nil {
		return nil, err
	}
	return mk(a, b), nil
}

// comparison lowers `field op int` or `int op field`.
func (l *lowerer) comparison(v *ast.BinaryNode) (node, error) {
	fieldSide, valueSide, op := v.Left, v.Right, v.Operator
	if _, ok := v.Left.(*ast.IdentifierNode); !ok {
		if _, ok := v.Right.(*ast.IdentifierNode); ok {
			fieldSide, valueSide, op = v.Right, v.Left, flip(op)
		} else if u, ok := v.Left.(*ast.UnaryNode); ok && (u.Operator == "not" || u.Operator == "!") {
			return nil, errorf(l.pos(v.Left), "%s binds tighter than %s; write %s (…)", u.Operator, v.Operator, u.Operator)
		} else {
			return nil, errorf(l.pos(v.Left), "expected a field on one side of %s", v.Operator)
		}
	}
	f, err := l.fieldOf(fieldSide)
	if err != nil {
		return nil, err
	}
	val, err := l.intLit(valueSide)
	if err != nil {
		return nil, err
	}
	if f.isSpecies() {
		if op != "==" && op != "!=" {
			return speciesRange(f, rangeFromOp(f, op, val), l.pos(fieldSide)), nil
		}
		return l.speciesLit(f, op == "!=", []int{val}, l.pos(fieldSide), []Position{l.pos(valueSide)})
	}
	return &rangeLit{f: f, set: rangeFromOp(f, op, val), pos: l.pos(fieldSide)}, nil
}

func flip(op string) string {
	switch op {
	case "<":
		return ">"
	case "<=":
		return ">="
	case ">":
		return "<"
	case ">=":
		return "<="
	}
	return op
}

// membership lowers `field in [a, b]` and `field in a..b`.
func (l *lowerer) membership(v *ast.BinaryNode) (node, error) {
	f, err := l.fieldOf(v.Left)
	if err != nil {
		return nil, err
	}
	pos := l.pos(v.Left)
	switch r := v.Right.(type) {
	case *ast.ArrayNode:
		vals := make([]int, 0, len(r.Nodes))
		valPos := make([]Position, 0, len(r.Nodes))
		for _, e := range r.Nodes {
			val, err := l.intLit(e)
			if err != nil {
				return nil, err
			}
			vals = append(vals, val)
			valPos = append(valPos, l.pos(e))
		}
		if f.isSpecies() {
			return l.speciesLit(f, false, vals, pos, valPos)
		}
		ivs := make([]interval, len(vals))
		for i, val := range vals {
			ivs[i] = interval{val, val}
		}
		return &rangeLit{f: f, set: setOf(ivs...).intersect(intervalSet{domains[f]}), pos: pos}, nil
	case *ast.BinaryNode:
		if r.Operator == ".." {
			lo, err := l.intLit(r.Left)
			if err != nil {
				return nil, err
			}
			hi, err := l.intLit(r.Right)
			if err != nil {
				return nil, err
			}
			set := setOf(interval{lo, hi}).intersect(intervalSet{domains[f]})
			if f.isSpecies() {
				return speciesRange(f, set, pos), nil
			}
			return &rangeLit{f: f, set: set, pos: pos}, nil
		}
	}
	return nil, errorf(l.pos(v.Right), "expected a list or a range after in")
}

func (l *lowerer) fieldOf(n ast.Node) (field, error) {
	id, ok := n.(*ast.IdentifierNode)
	if !ok {
		return 0, errorf(l.pos(n), "expected a field name, got %s", n.String())
	}
	f, ok := fieldByName[id.Value]
	if !ok {
		return 0, errorf(l.pos(n), "unknown field %q", id.Value)
	}
	return f, nil
}

func (l *lowerer) intLit(n ast.Node) (int, error) {
	val, ok := 0, false
	switch v := n.(type) {
	case *ast.IntegerNode:
		val, ok = v.Value, true
	case *ast.UnaryNode:
		if inner, isInt := v.Node.(*ast.IntegerNode); isInt && v.Operator == "-" {
			val, ok = -inner.Value, true
		}
	}
	if !ok {
		return 0, errorf(l.pos(n), "expected an integer, got %s", n.String())
	}
	if val < -maxLiteral || val > maxLiteral {
		return 0, errorf(l.pos(n), "integer %d is out of range", val)
	}
	return val, nil
}

// speciesLit validates each id at its own position (valPos parallels vals).
func (l *lowerer) speciesLit(f field, neg bool, vals []int, pos Position, valPos []Position) (node, error) {
	d := domains[f]
	for i, v := range vals {
		if f == fPokemon && v < 1 {
			return nil, errorf(valPos[i], `pokemon %d is not a species id; leave pokemon unconstrained for "everything else"`, v)
		}
		if v < d.lo || v > d.hi {
			return nil, errorf(valPos[i], "%s %d is out of range %d..%d", f, v, d.lo, d.hi)
		}
	}
	return &idLit{f: f, ids: idsOf(vals...), neg: neg, pos: pos}, nil
}

// speciesRange lowers an interval set over pokemon or form (already within
// the field's domain) to the equivalent id set: its members when they are
// at most half the domain, otherwise the negation of the complement's
// members, so pokemon > 5 becomes !pokemon{1..5}. An empty set becomes a
// positive empty id set, which can never hold.
func speciesRange(f field, set intervalSet, pos Position) node {
	d := domains[f]
	count := 0
	for _, iv := range set {
		count += iv.hi - iv.lo + 1
	}
	if 2*count <= d.hi-d.lo+1 {
		return &idLit{f: f, ids: idSet(set.values()), pos: pos}
	}
	return &idLit{f: f, ids: idSet(set.complement(d).values()), neg: true, pos: pos}
}

// rangeFromOp is the set of domain values v' with v' op v.
func rangeFromOp(f field, op string, v int) intervalSet {
	d := domains[f]
	var raw interval
	switch op {
	case "==":
		raw = interval{v, v}
	case "!=":
		return setOf(interval{v, v}).complement(d)
	case "<":
		raw = interval{d.lo, v - 1}
	case "<=":
		raw = interval{d.lo, v}
	case ">":
		raw = interval{v + 1, d.hi}
	case ">=":
		raw = interval{v, d.hi}
	}
	return setOf(raw).intersect(intervalSet{d})
}

// byteOffset converts Expr's rune offset into src to a byte offset, clamped
// to the end of the source.
func byteOffset(src string, runeOff int) int {
	n := 0
	for i := range src {
		if n == runeOff {
			return i
		}
		n++
	}
	return len(src)
}
