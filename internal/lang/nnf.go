package lang

// NNF pushes every negation down to the literals, where it becomes the
// complement within the field's domain. The result has no Not. A literal
// on a NULL-able field that ends up complemented is warned about (w may be
// nil): a row where the field is NULL fails every condition on it, so the
// complement does not include it.
func NNF(m *Model, n Node, w *Warnings) Node { return pushNot(m, n, false, w) }

func pushNot(m *Model, n Node, neg bool, w *Warnings) Node {
	switch v := n.(type) {
	case *Not:
		return pushNot(m, v.X, !neg, w)
	case *And:
		if neg {
			return &Or{pushNot(m, v.L, true, w), pushNot(m, v.R, true, w)}
		}
		return &And{pushNot(m, v.L, false, w), pushNot(m, v.R, false, w)}
	case *Or:
		if neg {
			return &And{pushNot(m, v.L, true, w), pushNot(m, v.R, true, w)}
		}
		return &Or{pushNot(m, v.L, false, w), pushNot(m, v.R, false, w)}
	case *Lit:
		fd := m.Field(v.F)
		warnNegatedNull(fd, v, neg, w)
		if !neg {
			return v
		}
		return &Lit{F: v.F, Set: v.Set.Complement(fd.Domain), Pos: v.Pos, Src: v.Src, Neg: !v.Neg}
	}
	return n
}

// warnNegatedNull warns about a literal on a NULL-able field that ends up
// complemented: by an enclosing not (and not written negative), or written
// negative (!=, not in) and not negated again. A literal over the whole
// domain already has its own warning saying it excludes the NULL rows, so
// it gets no second one.
func warnNegatedNull(fd *Field, v *Lit, neg bool, w *Warnings) {
	if w == nil || fd.Null == nil || neg == v.Neg {
		return
	}
	if w.Negated == nil {
		w.Negated = fd.Null
	}
	if v.Set.Equal(fd.Domain) {
		return
	}
	if neg {
		w.Add(v.Pos, "negating %s %s", v.Src, fd.Null.Negated)
	} else {
		w.Add(v.Pos, "%s %s", v.Src, fd.Null.Negated)
	}
}
