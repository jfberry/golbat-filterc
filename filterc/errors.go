package filterc

import "github.com/jfberry/golbat-filterc/internal/lang"

// Position locates an error in the expression source. Line and Column are
// 1-based; Offset is the byte offset.
type Position = lang.Position

// Error is a compile error at a position in the expression.
type Error = lang.Error

func errorf(pos Position, format string, args ...any) *Error {
	return lang.Errorf(pos, format, args...)
}
