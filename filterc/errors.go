package filterc

import "fmt"

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
