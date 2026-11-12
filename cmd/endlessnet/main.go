//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	_, _ = fmt.Fprintln(os.Stderr, "Эта консольная поставка предназначена для Windows 11 x64.")
	os.Exit(1)
}
