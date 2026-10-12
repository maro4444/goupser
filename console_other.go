//go:build !windows

package main

import (
	"os"

	"github.com/mattn/go-isatty"
)

// enableANSI zwraca true, gdy wyjście jest terminalem obsługującym sekwencje ANSI.
func enableANSI(f *os.File) bool {
	return isatty.IsTerminal(f.Fd())
}
