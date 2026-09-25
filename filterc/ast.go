package filterc

import (
	"fmt"
	"strings"
)

// node is the boolean tree an expression lowers to: and/or/not over
// literals. The only literal kinds are a range field with an interval set
// and a species/form id set with a negated flag.
type node interface{ isNode() }

type andNode struct{ l, r node }
type orNode struct{ l, r node }
type notNode struct{ x node }

// rangeLit constrains a non-species field to a set of values.
type rangeLit struct {
	f   field
	set intervalSet
	pos Position
}

// idLit constrains pokemon or form to an id set (or its complement).
type idLit struct {
	f   field
	ids idSet
	neg bool
	pos Position
}

func (*andNode) isNode()  {}
func (*orNode) isNode()   {}
func (*notNode) isNode()  {}
func (*rangeLit) isNode() {}
func (*idLit) isNode()    {}

// format renders a node for tests and debugging: and(a,b), or(a,b),
// not(a), iv{[90,100]}, pokemon{1,4}, !pokemon{710}.
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
	case *idLit:
		parts := make([]string, len(v.ids))
		for i, id := range v.ids {
			parts[i] = fmt.Sprint(id)
		}
		neg := ""
		if v.neg {
			neg = "!"
		}
		return neg + v.f.String() + "{" + strings.Join(parts, ",") + "}"
	}
	return fmt.Sprintf("<%T>", n)
}
