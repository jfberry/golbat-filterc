// Package lang is the model-driven front end shared by every filter
// language: Expr parsing and the whitelist walk, interval sets, negation
// normal form, DNF and split, positioned errors and the warnings
// collector. It knows nothing about any one model; a Model describes the
// fields.
package lang

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

// Errorf builds an Error at pos.
func Errorf(pos Position, format string, args ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, args...), Pos: pos}
}

// warning is a diagnostic about one literal: it compiles, but probably not
// to what the author meant.
type warning struct {
	pos  Position
	text string
}

// Warnings collects diagnostics through lowering and NNF. A nil collector
// discards them, for the stage tests that do not care.
type Warnings struct {
	items []warning
	// Negated is the NULL group of the first complemented NULL-able
	// literal warned about, so the "can never hold" warning can point at
	// it; nil when there was none.
	Negated *NullGroup
}

// Add records a warning at pos.
func (w *Warnings) Add(pos Position, format string, args ...any) {
	if w == nil {
		return
	}
	w.items = append(w.items, warning{pos: pos, text: fmt.Sprintf(format, args...)})
}

// List formats the warnings like errors (line:column: message), in source
// order. Repeats of the same formatted text (position included) are
// dropped: a guard against one literal being warned twice, not a feature;
// two literals with the same message keep a caret each.
func (w *Warnings) List() []string {
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
