package lang

import (
	"fmt"
	"strings"
)

// Node is the boolean tree an expression lowers to: and/or/not over
// literals. The only literal is a field with an interval set over the
// field's domain.
type Node interface{ isNode() }

type And struct{ L, R Node }
type Or struct{ L, R Node }
type Not struct{ X Node }

// Lit constrains a field to a set of values within its domain.
type Lit struct {
	F   FieldID
	Set IntervalSet
	Pos Position
	// Src is the literal as written (field op value, value op field,
	// field [not] in [...] or lo..hi), for warnings; Neg is true when the
	// author wrote it in a negative form (!= or not in), so Set is already
	// a complement.
	Src string
	Neg bool
}

func (*And) isNode() {}
func (*Or) isNode()  {}
func (*Not) isNode() {}
func (*Lit) isNode() {}

// Format renders a node for tests and debugging: and(a,b), or(a,b),
// not(a), iv{[90,100]}, pokemon{[1,1],[4,4]}.
func Format(m *Model, n Node) string {
	switch v := n.(type) {
	case *And:
		return "and(" + Format(m, v.L) + "," + Format(m, v.R) + ")"
	case *Or:
		return "or(" + Format(m, v.L) + "," + Format(m, v.R) + ")"
	case *Not:
		return "not(" + Format(m, v.X) + ")"
	case *Lit:
		parts := make([]string, len(v.Set))
		for i, iv := range v.Set {
			parts[i] = fmt.Sprintf("[%d,%d]", iv.Lo, iv.Hi)
		}
		return m.Field(v.F).Name + "{" + strings.Join(parts, ",") + "}"
	}
	return fmt.Sprintf("<%T>", n)
}
