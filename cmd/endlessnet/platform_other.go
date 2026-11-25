//go:build !windows && !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	_, _ = fmt.Fprintln(os.Stderr, "Эта консольная поставка предназначена для Windows и Linux.")
	os.Exit(1)
}
