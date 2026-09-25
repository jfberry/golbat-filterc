package filterc

// nnf pushes every negation down to the literals, where it becomes the
// complement within the field's domain. The result has no notNode. A PvP
// literal that ends up complemented is warned about (w may be nil): a
// pokemon with no PvP data fails every PvP comparison, so the complement
// does not include it.
func nnf(n node, w *warnings) node { return pushNot(n, false, w) }

func pushNot(n node, neg bool, w *warnings) node {
	switch v := n.(type) {
	case *notNode:
		return pushNot(v.x, !neg, w)
	case *andNode:
		if neg {
			return &orNode{pushNot(v.l, true, w), pushNot(v.r, true, w)}
		}
		return &andNode{pushNot(v.l, false, w), pushNot(v.r, false, w)}
	case *orNode:
		if neg {
			return &andNode{pushNot(v.l, true, w), pushNot(v.r, true, w)}
		}
		return &orNode{pushNot(v.l, false, w), pushNot(v.r, false, w)}
	case *rangeLit:
		warnNegatedPvp(v, neg, w)
		if !neg {
			return v
		}
		return &rangeLit{f: v.f, set: v.set.complement(domains[v.f]), pos: v.pos, src: v.src, neg: !v.neg}
	}
	return n
}

const noPvpData = "never matches a pokemon without PvP data; a pokemon with no PvP data fails every PvP comparison, negated or not"

// warnNegatedPvp warns about a PvP literal that ends up complemented: by an
// enclosing not (and not written negative), or written negative (!=, not
// in) and not negated again.
func warnNegatedPvp(v *rangeLit, neg bool, w *warnings) {
	if w == nil || (v.f != fLittle && v.f != fGreat && v.f != fUltra) || neg == v.neg {
		return
	}
	if neg {
		w.add(v.pos, "negating %s %s", v.src, noPvpData)
	} else {
		w.add(v.pos, "%s %s", v.src, noPvpData)
	}
	w.pvpNegated = true
}
