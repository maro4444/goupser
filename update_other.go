//go:build !windows

package main

import (
	"os"
	"syscall"
)

// replaceExecutable podmienia plik programu na nową wersję.
// Na Linuksie działający plik można zastąpić zwykłą zmianą nazwy.
func replaceExecutable(exe, newPath string) error {
	return os.Rename(newPath, exe)
}

// cleanupOldExecutable nic nie robi: poza Windows po aktualizacji nie zostaje stary plik.
func cleanupOldExecutable() {}

// restartSelf zastępuje bieżący proces nową wersją programu z tymi samymi argumentami.
func restartSelf(exe string) error {
	env := append(os.Environ(), restartedEnv+"=1")
	return syscall.Exec(exe, os.Args, env)
}
