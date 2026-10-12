//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
)

// replaceExecutable podmienia plik programu na nową wersję.
// Działającego pliku .exe nie można nadpisać ani usunąć, ale można zmienić jego nazwę,
// więc stara wersja zostaje jako .old i jest usuwana przy następnym uruchomieniu.
func replaceExecutable(exe, newPath string) error {
	oldPath := exe + ".old"
	os.Remove(oldPath)

	if err := os.Rename(exe, oldPath); err != nil {
		return err
	}
	if err := os.Rename(newPath, exe); err != nil {
		os.Rename(oldPath, exe)
		return err
	}
	return nil
}

// cleanupOldExecutable usuwa starą wersję programu pozostawioną po aktualizacji.
// Jeśli stara wersja wciąż działa (czeka na nową), usunięcie się nie uda i nastąpi przy kolejnym starcie.
func cleanupOldExecutable() {
	exe, err := executablePath()
	if err != nil {
		return
	}
	os.Remove(exe + ".old")
}

// restartSelf uruchamia nową wersję programu z tymi samymi argumentami w tej samej konsoli,
// czeka na jej zakończenie i kończy się z tym samym kodem wyjścia.
func restartSelf(exe string) error {
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), restartedEnv+"=1")

	// Ctrl+C trafia do obu procesów; stary ma tylko poczekać, aż nowy się zakończy.
	signal.Ignore(os.Interrupt)

	if err := cmd.Start(); err != nil {
		return err
	}

	err := cmd.Wait()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.ExitCode())
	}
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
	return nil
}
