package filterc

import (
	"fmt"
	"slices"
	"strings"

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
func parse(src string) (node, error) { return lower(src, nil) }

// lower is parse with warnings collected into w (which may be nil).
func lower(src string, w *warnings) (node, error) {
	if len(src) > maxExpressionBytes {
		return nil, errorf(Position{Line: 1, Column: 1}, "expression longer than %d bytes", maxExpressionBytes)
	}
	t := newPosTable(src)
	tree, err := parser.Parse(src)
	if err != nil {
		if fe, ok := err.(*file.Error); ok {
			return nil, &Error{Msg: fe.Message, Pos: t.at(fe.From)}
		}
		return nil, &Error{Msg: err.Error(), Pos: Position{Line: 1, Column: 1}}
	}
	l := lowerer{t: t, w: w}
	return l.boolean(tree.Node)
}

// posTable converts Expr's rune offsets to positions without re-scanning
// the source: built once per parse, each lookup is a binary search over
// the line starts.
type posTable struct {
	runeToByte []int // byte offset of each rune index, plus one past the end
	lineStarts []int // rune index of each line start
}

func newPosTable(src string) posTable {
	t := posTable{runeToByte: make([]int, 0, len(src)+1), lineStarts: []int{0}}
	for i, r := range src {
		t.runeToByte = append(t.runeToByte, i)
		if r == '\n' {
			t.lineStarts = append(t.lineStarts, len(t.runeToByte))
		}
	}
	t.runeToByte = append(t.runeToByte, len(src))
	return t
}

// at is the position of rune offset from, clamped to the source.
func (t posTable) at(from int) Position {
	from = min(max(from, 0), len(t.runeToByte)-1)
	line, found := slices.BinarySearch(t.lineStarts, from)
	if !found {
		line--
	}
	return Position{Line: line + 1, Column: from - t.lineStarts[line] + 1, Offset: t.runeToByte[from]}
}

type lowerer struct {
	t posTable
	w *warnings
}

func (l *lowerer) pos(n ast.Node) Position { return l.t.at(n.Location().From) }

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
			return l.membership(v, false)
		}
		return nil, errorf(l.pos(n), "unsupported operator %q", v.Operator)
	case *ast.UnaryNode:
		if v.Operator != "not" && v.Operator != "!" {
			return nil, errorf(l.pos(n), "unsupported operator %q", v.Operator)
		}
		if !isBoolean(v.Node) {
			return nil, errorf(l.pos(n), "%s applies to a comparison; write %s (…)", v.Operator, v.Operator)
		}
		// `x not in y` parses as not(x in y) with the not after x;
		// `not (x in y)` has it before
		if in, ok := v.Node.(*ast.BinaryNode); ok && in.Operator == "in" && v.Location().From > in.Left.Location().From {
			return l.membership(in, true)
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
	if f.isSpecies() && (op == "==" || op == "!=") {
		if err := checkIds(f, []int{val}, func(int) Position { return l.pos(valueSide) }); err != nil {
			return nil, err
		}
	}
	src := fmt.Sprintf("%s %s %d", f, v.Operator, val)
	if fieldSide != v.Left {
		src = fmt.Sprintf("%d %s %s", val, v.Operator, f)
	}
	lit := &rangeLit{f: f, set: rangeFromOp(f, op, val), pos: l.pos(v.Left), src: src, neg: op == "!="}
	if verdict, ok := holds(f, lit.set); ok {
		if d := domains[f]; val < d.lo || val > d.hi {
			l.w.add(lit.pos, "%s: %d is outside %s's range %d..%d, so this condition %s", src, val, f, d.lo, d.hi, verdict)
		} else {
			l.w.add(lit.pos, "%s: %s's range is %d..%d, so this condition %s", src, f, d.lo, d.hi, verdict)
		}
	}
	return lit, nil
}

// holds says whether a literal's set is the whole domain or empty.
func holds(f field, set intervalSet) (verdict string, ok bool) {
	switch {
	case len(set) == 0:
		return "can never hold", true
	case set.equal(intervalSet{domains[f]}):
		return "always holds", true
	}
	return "", false
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

// membership lowers `field in [a, b]` and `field in a..b`, and their
// `not in` forms when neg is set.
func (l *lowerer) membership(v *ast.BinaryNode, neg bool) (node, error) {
	f, err := l.fieldOf(v.Left)
	if err != nil {
		return nil, err
	}
	pos := l.pos(v.Left)
	in := "in"
	if neg {
		in = "not in"
	}
	lit := func(set intervalSet, src string) *rangeLit {
		set = set.intersect(intervalSet{domains[f]})
		if neg {
			set = set.complement(domains[f])
		}
		return &rangeLit{f: f, set: set, pos: pos, src: fmt.Sprintf("%s %s %s", f, in, src), neg: neg}
	}
	switch r := v.Right.(type) {
	case *ast.ArrayNode:
		vals := make([]int, 0, len(r.Nodes))
		for _, e := range r.Nodes {
			val, err := l.intLit(e)
			if err != nil {
				return nil, err
			}
			vals = append(vals, val)
		}
		if f.isSpecies() {
			if err := checkIds(f, vals, func(i int) Position { return l.pos(r.Nodes[i]) }); err != nil {
				return nil, err
			}
		}
		ivs := make([]interval, len(vals))
		strs := make([]string, len(vals))
		for i, val := range vals {
			ivs[i] = interval{val, val}
			strs[i] = fmt.Sprint(val)
		}
		out := lit(setOf(ivs...), "["+strings.Join(strs, ", ")+"]")
		d := domains[f]
		for i, val := range vals {
			if val < d.lo || val > d.hi {
				l.w.add(l.pos(r.Nodes[i]), "%s: %d is outside %s's range %d..%d and is ignored", out.src, val, f, d.lo, d.hi)
			}
		}
		return out, nil
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
			out := lit(setOf(interval{lo, hi}), fmt.Sprintf("%d..%d", lo, hi))
			l.warnRange(out, lo, hi)
			return out, nil
		}
	}
	return nil, errorf(l.pos(v.Right), "expected a list or a range after in")
}

// warnRange warns about a range literal lo..hi whose bounds reach outside
// the field's domain, or which covers the whole domain.
func (l *lowerer) warnRange(lit *rangeLit, lo, hi int) {
	f, d := lit.f, domains[lit.f]
	clipped := setOf(interval{lo, hi}).intersect(intervalSet{d})
	out, outside := lo, lo < d.lo || lo > d.hi
	if !outside && (hi < d.lo || hi > d.hi) {
		out, outside = hi, true
	}
	verdict, ok := holds(f, lit.set)
	switch {
	case f == fPokemon && lo < d.lo && hi >= d.lo && !ok:
		ignored := fmt.Sprint(lo)
		if lo < 0 {
			ignored = fmt.Sprintf("%d..0", lo)
		}
		l.w.add(lit.pos, "%s: species ids start at 1; %s is ignored", lit.src, ignored)
	case ok && outside:
		l.w.add(lit.pos, "%s: %d is outside %s's range %d..%d, so this condition %s", lit.src, out, f, d.lo, d.hi, verdict)
	case ok && lo <= hi:
		l.w.add(lit.pos, "%s: %s's range is %d..%d, so this condition %s", lit.src, f, d.lo, d.hi, verdict)
	case outside:
		c := clipped[0]
		l.w.add(lit.pos, "%s: %d is outside %s's range %d..%d; the range is clipped to %d..%d", lit.src, out, f, d.lo, d.hi, c.lo, c.hi)
	}
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

// checkIds rejects explicit species or form ids outside the field's domain
// (pokemon 0 with its own message), each at its own position. Ranges and
// ordered comparisons are clipped to the domain instead.
func checkIds(f field, vals []int, posOf func(i int) Position) error {
	d := domains[f]
	for i, v := range vals {
		if f == fPokemon && v < 1 {
			return errorf(posOf(i), `pokemon %d is not a species id; leave pokemon unconstrained for "everything else"`, v)
		}
		if v < d.lo || v > d.hi {
			return errorf(posOf(i), "%s %d is out of range %d..%d", f, v, d.lo, d.hi)
		}
	}
	return nil
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
