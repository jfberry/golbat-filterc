package lang

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/file"
	"github.com/expr-lang/expr/parser"
)

const (
	maxExpressionBytes = 64 << 10
	maxLiteral         = 1 << 20 // keeps v±1 arithmetic far from overflow
)

// Lower turns expression source into the boolean tree over m's fields,
// using Expr's parser and accepting only the subset the compiler
// understands. The whitelist walk is the type check: every accepted node
// is a comparison between a field of m and an integer, or and/or/not over
// such comparisons. Warnings go to w, which may be nil.
func Lower(m *Model, src string, w *Warnings) (Node, error) {
	if len(src) > maxExpressionBytes {
		return nil, Errorf(Position{Line: 1, Column: 1}, "expression longer than %d bytes", maxExpressionBytes)
	}
	if strings.TrimSpace(src) == "" {
		return nil, Errorf(Position{Line: 1, Column: 1}, "empty expression")
	}
	t := newPosTable(src)
	tree, err := parser.Parse(src)
	if err != nil {
		if fe, ok := err.(*file.Error); ok {
			return nil, &Error{Msg: fe.Message, Pos: t.at(fe.From)}
		}
		return nil, &Error{Msg: err.Error(), Pos: Position{Line: 1, Column: 1}}
	}
	l := lowerer{m: m, t: t, w: w, src: src}
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
	m   *Model
	t   posTable
	w   *Warnings
	src string
}

func (l *lowerer) pos(n ast.Node) Position { return l.t.at(n.Location().From) }

func (l *lowerer) boolean(n ast.Node) (Node, error) {
	switch v := n.(type) {
	case *ast.BinaryNode:
		switch v.Operator {
		case "and", "&&":
			return l.pair(v, func(a, b Node) Node { return &And{a, b} })
		case "or", "||":
			return l.pair(v, func(a, b Node) Node { return &Or{a, b} })
		case "==", "!=", "<", "<=", ">", ">=":
			return l.comparison(v)
		case "in":
			return l.membership(v, false)
		}
		return nil, Errorf(l.pos(n), "unsupported operator %q", v.Operator)
	case *ast.UnaryNode:
		if v.Operator != "not" && v.Operator != "!" {
			return nil, Errorf(l.pos(n), "unsupported operator %q", v.Operator)
		}
		if !isBoolean(v.Node) {
			return nil, Errorf(l.pos(n), "%s applies to a comparison; write %s (…)", v.Operator, v.Operator)
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
		return &Not{x}, nil
	case *ast.IdentifierNode, *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode, *ast.StringNode:
		return nil, Errorf(l.pos(n), "expected a comparison")
	}
	return nil, Errorf(l.pos(n), "unsupported construct %s", n.String())
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

func (l *lowerer) pair(v *ast.BinaryNode, mk func(a, b Node) Node) (Node, error) {
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
func (l *lowerer) comparison(v *ast.BinaryNode) (Node, error) {
	fieldSide, valueSide, op := v.Left, v.Right, v.Operator
	if _, ok := v.Left.(*ast.IdentifierNode); !ok {
		if _, ok := v.Right.(*ast.IdentifierNode); ok {
			fieldSide, valueSide, op = v.Right, v.Left, flip(op)
		} else if u, ok := v.Left.(*ast.UnaryNode); ok && (u.Operator == "not" || u.Operator == "!") {
			return nil, Errorf(l.pos(v.Left), "%s binds tighter than %s; write %s (…)", u.Operator, v.Operator, u.Operator)
		} else {
			return nil, Errorf(l.pos(v.Left), "expected a field on one side of %s", v.Operator)
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
	fd := l.m.Field(f)
	if fd.IDs && (op == "==" || op == "!=") {
		if err := checkIds(fd, []int{val}, func(int) Position { return l.pos(valueSide) }); err != nil {
			return nil, err
		}
	}
	src := fmt.Sprintf("%s %s %d", fd.Name, v.Operator, val)
	if fieldSide != v.Left {
		src = fmt.Sprintf("%d %s %s", val, v.Operator, fd.Name)
	}
	lit := &Lit{F: f, Set: rangeFromOp(fd, op, val), Pos: l.pos(v.Left), Src: src, Neg: op == "!="}
	if verdict, ok := holds(fd, lit.Set); ok {
		if d := fd.Domain; val < d.Lo() || val > d.Hi() {
			l.w.Add(lit.Pos, "%s: %d is outside %s's range %d..%d, so this condition %s", src, val, fd.Name, d.Lo(), d.Hi(), verdict)
		} else {
			l.w.Add(lit.Pos, "%s: %s's range is %d..%d, so this condition %s", src, fd.Name, d.Lo(), d.Hi(), verdict)
		}
	}
	return lit, nil
}

// holds says whether a literal's set is the whole domain or empty. On a
// NULL-able field a literal over the whole domain is not a no-op: it fails
// for every row where the field is NULL, and the group's verdict says so.
func holds(fd *Field, set IntervalSet) (verdict string, ok bool) {
	switch {
	case len(set) == 0:
		return "can never hold", true
	case !set.Equal(fd.Domain):
		return "", false
	case fd.Null != nil:
		return fd.Null.Whole, true
	}
	return "always holds", true
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
func (l *lowerer) membership(v *ast.BinaryNode, neg bool) (Node, error) {
	if u, ok := v.Left.(*ast.UnaryNode); ok && (u.Operator == "not" || u.Operator == "!") {
		return nil, Errorf(l.pos(u), "%s binds tighter than in; write %s (%s in %s)", u.Operator, u.Operator, u.Node.String(), v.Right.String())
	}
	f, err := l.fieldOf(v.Left)
	if err != nil {
		return nil, err
	}
	fd := l.m.Field(f)
	d := fd.Domain
	pos := l.pos(v.Left)
	in := "in"
	if neg {
		in = "not in"
	}
	lit := func(set IntervalSet, src string) *Lit {
		set = set.Intersect(d)
		if neg {
			set = set.Complement(d)
		}
		return &Lit{F: f, Set: set, Pos: pos, Src: fmt.Sprintf("%s %s %s", fd.Name, in, src), Neg: neg}
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
		if fd.IDs {
			if err := checkIds(fd, vals, func(i int) Position { return l.pos(r.Nodes[i]) }); err != nil {
				return nil, err
			}
		}
		ivs := make([]Interval, len(vals))
		strs := make([]string, len(vals))
		for i, val := range vals {
			ivs[i] = Interval{val, val}
			strs[i] = fmt.Sprint(val)
		}
		out := lit(SetOf(ivs...), "["+strings.Join(strs, ", ")+"]")
		inDomain := 0
		for i, val := range vals {
			if val < d.Lo() || val > d.Hi() {
				l.w.Add(l.pos(r.Nodes[i]), "%s: %d is outside %s's range %d..%d and is ignored", out.Src, val, fd.Name, d.Lo(), d.Hi())
			} else {
				inDomain++
			}
		}
		if verdict, ok := holds(fd, out.Set); ok {
			switch {
			case len(vals) == 0:
				l.w.Add(pos, "%s: the list is empty, so this condition %s", out.Src, verdict)
			case inDomain == 0:
				l.w.Add(pos, "%s: no listed value is in %s's range %d..%d, so this condition %s", out.Src, fd.Name, d.Lo(), d.Hi(), verdict)
			default:
				l.w.Add(pos, "%s: %s's range is %d..%d, so this condition %s", out.Src, fd.Name, d.Lo(), d.Hi(), verdict)
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
			out := lit(SetOf(Interval{lo, hi}), fmt.Sprintf("%d..%d", lo, hi))
			l.warnRange(out, lo, hi)
			return out, nil
		}
	}
	return nil, Errorf(l.pos(v.Right), "expected a list or a range after in")
}

// warnRange warns about a range literal lo..hi whose bounds reach outside
// the field's domain, or which covers the whole domain.
func (l *lowerer) warnRange(lit *Lit, lo, hi int) {
	fd := l.m.Field(lit.F)
	d := fd.Domain
	clipped := SetOf(Interval{lo, hi}).Intersect(d)
	out, outside := lo, lo < d.Lo() || lo > d.Hi()
	if !outside && (hi < d.Lo() || hi > d.Hi()) {
		out, outside = hi, true
	}
	verdict, ok := holds(fd, lit.Set)
	switch {
	case lo > hi: // empty whatever the domain; `not in` makes it whole
		l.w.Add(lit.Pos, "%s: the range %d..%d is empty (%d > %d), so this condition %s", lit.Src, lo, hi, lo, hi, verdict)
	case fd.Below != "" && lo < d.Lo() && hi >= d.Lo() && !ok:
		ignored := fmt.Sprint(lo)
		if lo < d.Lo()-1 {
			ignored = fmt.Sprintf("%d..%d", lo, d.Lo()-1)
		}
		l.w.Add(lit.Pos, "%s: %s; %s is ignored", lit.Src, fd.Below, ignored)
	case ok && outside:
		l.w.Add(lit.Pos, "%s: %d is outside %s's range %d..%d, so this condition %s", lit.Src, out, fd.Name, d.Lo(), d.Hi(), verdict)
	case ok && lo <= hi:
		l.w.Add(lit.Pos, "%s: %s's range is %d..%d, so this condition %s", lit.Src, fd.Name, d.Lo(), d.Hi(), verdict)
	case outside:
		c := clipped[0]
		l.w.Add(lit.Pos, "%s: %d is outside %s's range %d..%d; the range is clipped to %d..%d", lit.Src, out, fd.Name, d.Lo(), d.Hi(), c.Lo, c.Hi)
	}
}

func (l *lowerer) fieldOf(n ast.Node) (FieldID, error) {
	id, ok := n.(*ast.IdentifierNode)
	if !ok {
		return 0, Errorf(l.pos(n), "expected a field name, got %s", n.String())
	}
	f, ok := l.m.Lookup(id.Value)
	if !ok {
		return 0, Errorf(l.pos(n), "unknown field %q", id.Value)
	}
	return f, nil
}

func (l *lowerer) intLit(n ast.Node) (int, error) {
	val, ok := 0, false
	switch v := n.(type) {
	case *ast.IntegerNode:
		val, ok = v.Value, true
	case *ast.FloatNode:
		return 0, l.floatError(n, v, "")
	case *ast.UnaryNode:
		if inner, isInt := v.Node.(*ast.IntegerNode); isInt && v.Operator == "-" {
			val, ok = -inner.Value, true
		}
		if inner, isFloat := v.Node.(*ast.FloatNode); isFloat && v.Operator == "-" {
			return 0, l.floatError(n, inner, "-")
		}
	}
	if !ok {
		return 0, Errorf(l.pos(n), "expected an integer, got %s", n.String())
	}
	if val < -maxLiteral || val > maxLiteral {
		return 0, Errorf(l.pos(n), "integer %d is out of range", val)
	}
	return val, nil
}

// floatError names a float literal as written and, when Expr renders it
// differently (1e2, 100.0), the value it stands for.
func (l *lowerer) floatError(at ast.Node, f *ast.FloatNode, sign string) error {
	rendered := sign + strconv.FormatFloat(f.Value, 'f', -1, 64)
	loc := f.Location()
	if loc.From >= 0 && loc.From < loc.To && loc.To < len(l.t.runeToByte) {
		if text := sign + l.src[l.t.runeToByte[loc.From]:l.t.runeToByte[loc.To]]; text != rendered {
			return Errorf(l.pos(at), "expected an integer, got a float (%s is %s)", text, rendered)
		}
	}
	return Errorf(l.pos(at), "expected an integer, got a float (%s)", rendered)
}

// checkIds rejects explicit ids outside the field's domain, each at its
// own position, with the field's own message when it has one. Ranges and
// ordered comparisons are clipped to the domain instead. It returns a
// plain nil error when every id is in range.
func checkIds(fd *Field, vals []int, posOf func(i int) Position) error {
	d := fd.Domain
	for i, v := range vals {
		if v >= d.Lo() && v <= d.Hi() {
			continue
		}
		if fd.BadID != nil {
			if msg := fd.BadID(v); msg != "" {
				return &Error{Msg: msg, Pos: posOf(i)}
			}
		}
		return Errorf(posOf(i), "%s %d is out of range %d..%d", fd.Name, v, d.Lo(), d.Hi())
	}
	return nil
}

// rangeFromOp is the set of domain values v' with v' op v.
func rangeFromOp(fd *Field, op string, v int) IntervalSet {
	d := fd.Domain
	var raw Interval
	switch op {
	case "==":
		raw = Interval{v, v}
	case "!=":
		return SetOf(Interval{v, v}).Complement(d)
	case "<":
		raw = Interval{d.Lo(), v - 1}
	case "<=":
		raw = Interval{d.Lo(), v}
	case ">":
		raw = Interval{v + 1, d.Hi()}
	case ">=":
		raw = Interval{v, d.Hi()}
	}
	return SetOf(raw).Intersect(d)
}
