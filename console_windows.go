//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableANSI włącza obsługę sekwencji ANSI w konsoli Windows.
// Zwraca false, gdy wyjście nie jest konsolą (np. jest przekierowane do pliku).
func enableANSI(f *os.File) bool {
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}
