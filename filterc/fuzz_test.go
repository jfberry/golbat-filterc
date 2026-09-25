package filterc

import (
	"errors"
	"testing"
)

func FuzzCompile(f *testing.F) {
	for _, s := range []string{
		"size == 5 && pokemon != 710",
		"iv == 100 || (pokemon == 1 && gender == 2)",
		"!(iv >= 90)",
		"pokemon in [1, 4] && form != 0",
		"great <= 100 || little not in 1..10",
		"not great <= 100",
		"iv >= 90 &&",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		c, err := Compile(src)
		if err != nil {
			var e *Error
			if !errors.As(err, &e) {
				t.Fatalf("%q: error is not *Error: %v", src, err)
			}
			return
		}
		checkInvariants(t, src, c)
	})
}
