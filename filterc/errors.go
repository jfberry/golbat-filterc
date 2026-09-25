package filterc

import (
	"fmt"
	"slices"
)

// Position locates an error in the expression source. Line and Column are
// 1-based; Offset is the byte offset.
type Position struct {
	Line, Column, Offset int
}

// Error is a compile error at a position in the expression.
type Error struct {
	Msg string
	Pos Position
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Column, e.Msg)
}

func errorf(pos Position, format string, args ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, args...), Pos: pos}
}

// warning is a diagnostic about one literal: it compiles, but probably not
// to what the author meant.
type warning struct {
	pos  Position
	text string
}

// warnings collects diagnostics through lowering and NNF. A nil collector
// discards them, for the stage tests that do not care.
type warnings struct {
	items []warning
	// pvpNegated is set once a complemented PvP literal has been warned
	// about, so the "can never hold" warning can point at it.
	pvpNegated bool
}

func (w *warnings) add(pos Position, format string, args ...any) {
	if w == nil {
		return
	}
	w.items = append(w.items, warning{pos: pos, text: fmt.Sprintf(format, args...)})
}

// list formats the warnings like errors (line:column: message), in source
// order, dropping repeats of the same text.
func (w *warnings) list() []string {
	if w == nil || len(w.items) == 0 {
		return nil
	}
	items := slices.Clone(w.items)
	slices.SortStableFunc(items, func(a, b warning) int { return a.pos.Offset - b.pos.Offset })
	out := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		s := (&Error{Msg: it.text, Pos: it.pos}).Error()
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
