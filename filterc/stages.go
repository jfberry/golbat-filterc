package filterc

import "github.com/jfberry/golbat-filterc/internal/lang"

// The compiler's stages live in internal/lang; these names keep filterc's
// code and its white-box tests reading as they did.
type (
	interval    = lang.Interval
	intervalSet = lang.IntervalSet
	warnings    = lang.Warnings
)

func setOf(ivs ...interval) intervalSet { return lang.SetOf(ivs...) }
