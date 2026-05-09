//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "feedmedaily-tray is currently supported on Windows only")
	os.Exit(1)
}
