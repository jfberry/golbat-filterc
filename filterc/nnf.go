package filterc

// nnf pushes every negation down to the literals, where it becomes the
// complement within the field's domain. The result has no notNode.
func nnf(n node) node { return pushNot(n, false) }

func pushNot(n node, neg bool) node {
	switch v := n.(type) {
	case *notNode:
		return pushNot(v.x, !neg)
	case *andNode:
		if neg {
			return &orNode{pushNot(v.l, true), pushNot(v.r, true)}
		}
		return &andNode{pushNot(v.l, false), pushNot(v.r, false)}
	case *orNode:
		if neg {
			return &andNode{pushNot(v.l, true), pushNot(v.r, true)}
		}
		return &orNode{pushNot(v.l, false), pushNot(v.r, false)}
	case *rangeLit:
		if !neg {
			return v
		}
		return &rangeLit{f: v.f, set: v.set.complement(domains[v.f]), pos: v.pos}
	case *idLit:
		if !neg {
			return v
		}
		return &idLit{f: v.f, ids: v.ids, neg: !v.neg, pos: v.pos}
	}
	return n
}
