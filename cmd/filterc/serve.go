// Stub, replaced in Task 9.

package main

import (
	"fmt"
	"io"
)

func runServe(s settings, stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "serve: not implemented yet")
	return 2
}
