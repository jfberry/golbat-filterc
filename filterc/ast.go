package filterc

import (
	"fmt"
	"strings"
)

// node is the boolean tree an expression lowers to: and/or/not over
// literals. The only literal is a field with an interval set over the
// field's domain; pokemon and form are fields like any other.
type node interface{ isNode() }

type andNode struct{ l, r node }
type orNode struct{ l, r node }
type notNode struct{ x node }

// rangeLit constrains a field to a set of values within its domain.
type rangeLit struct {
	f   field
	set intervalSet
	pos Position
	// src is the literal as written (field op value, value op field,
	// field [not] in [...] or lo..hi), for warnings; neg is true when the
	// author wrote it in a negative form (!= or not in), so set is already
	// a complement.
	src string
	neg bool
}

func (*andNode) isNode()  {}
func (*orNode) isNode()   {}
func (*notNode) isNode()  {}
func (*rangeLit) isNode() {}

// format renders a node for tests and debugging: and(a,b), or(a,b),
// not(a), iv{[90,100]}, pokemon{[1,1],[4,4]}.
func format(n node) string {
	switch v := n.(type) {
	case *andNode:
		return "and(" + format(v.l) + "," + format(v.r) + ")"
	case *orNode:
		return "or(" + format(v.l) + "," + format(v.r) + ")"
	case *notNode:
		return "not(" + format(v.x) + ")"
	case *rangeLit:
		parts := make([]string, len(v.set))
		for i, iv := range v.set {
			parts[i] = fmt.Sprintf("[%d,%d]", iv.lo, iv.hi)
		}
		return v.f.String() + "{" + strings.Join(parts, ",") + "}"
	}
	return fmt.Sprintf("<%T>", n)
}
