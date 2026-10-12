package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
)

const (
	latestReleaseURL = "https://api.github.com/repos/maro4444/goupser/releases/latest"

	// Krótki limit, żeby brak internetu nie opóźniał startu serwera.
	updateCheckTimeout = 3 * time.Second
	downloadTimeout    = 10 * time.Minute

	// Ustawiana dla procesu uruchomionego po aktualizacji, żeby nie sprawdzał wersji drugi raz.
	restartedEnv = "GOUPSER_PO_AKTUALIZACJI"
)

// version ustawia GoReleaser podczas budowania (-X main.version=...).
// Wersja "dev" oznacza build lokalny, który się nie aktualizuje.
var version = "dev"

// versionString zwraca wersję programu do wyświetlenia, np. "v0.1.22".
func versionString() string {
	if version == "dev" {
		return "dev (build lokalny, bez automatycznych aktualizacji)"
	}
	return "v" + strings.TrimPrefix(version, "v")
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// checkForUpdate sprawdza, czy na GitHubie jest nowsza wersja programu, i za zgodą
// użytkownika pobiera ją oraz uruchamia ponownie. Gdy nie ma internetu, nic nie robi.
func checkForUpdate() {
	if version == "dev" || os.Getenv(restartedEnv) != "" {
		return
	}

	rel, err := fetchLatestRelease()
	if err != nil {
		return
	}
	if !isNewerVersion(rel.TagName, version) {
		return
	}

	fmt.Printf("Dostępna jest nowa wersja %s (obecna: %s).\n", rel.TagName, versionString())
	if !isInteractive() {
		fmt.Println("Aby zaktualizować, uruchom program w terminalu.")
		return
	}
	if !askYesNo("Zaktualizować teraz? [t/N]: ") {
		return
	}

	exe, err := applyUpdate(rel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Aktualizacja nie powiodła się: %v\nUruchamiam obecną wersję.\n", err)
		return
	}

	fmt.Printf("Zaktualizowano do wersji %s. Uruchamiam ponownie...\n\n", rel.TagName)
	if err := restartSelf(exe); err != nil {
		fmt.Fprintf(os.Stderr, "Nie udało się uruchomić nowej wersji: %v\nUruchom program ponownie ręcznie.\n", err)
		os.Exit(1)
	}
}

// fetchLatestRelease pobiera z API GitHuba opis najnowszego wydania.
func fetchLatestRelease() (*githubRelease, error) {
	req, err := http.NewRequest(http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "goupser/"+version)

	client := &http.Client{Timeout: updateCheckTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub zwrócił status %s", resp.Status)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// isNewerVersion sprawdza, czy wersja latest (np. "v0.1.22") jest nowsza od current.
// Gdy którejś wersji nie da się odczytać, zwraca false.
func isNewerVersion(latest, current string) bool {
	l, ok1 := parseVersion(latest)
	c, ok2 := parseVersion(current)
	if !ok1 || !ok2 {
		return false
	}

	for i := 0; i < len(l) || i < len(c); i++ {
		var lv, cv int
		if i < len(l) {
			lv = l[i]
		}
		if i < len(c) {
			cv = c[i]
		}
		if lv != cv {
			return lv > cv
		}
	}
	return false
}

// parseVersion zamienia tekst w rodzaju "v1.2.3" na listę liczb.
func parseVersion(v string) ([]int, bool) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		nums = append(nums, n)
	}
	return nums, true
}

// isInteractive sprawdza, czy użytkownik może odpowiedzieć na pytanie w terminalu.
func isInteractive() bool {
	fd := os.Stdin.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// askYesNo zadaje pytanie i zwraca true, gdy użytkownik odpowie twierdząco.
func askYesNo(question string) bool {
	fmt.Print(question)
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && answer == "" {
		fmt.Println()
		return false
	}

	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "t", "tak", "y", "yes":
		return true
	default:
		return false
	}
}

// executablePath zwraca rzeczywistą ścieżkę uruchomionego pliku programu.
func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// applyUpdate pobiera nową wersję programu, sprawdza jej sumę kontrolną
// i podmienia nią uruchomiony plik. Zwraca ścieżkę do podmienionego pliku.
func applyUpdate(rel *githubRelease) (string, error) {
	exe, err := executablePath()
	if err != nil {
		return "", fmt.Errorf("nie można ustalić ścieżki programu: %w", err)
	}

	binName := fmt.Sprintf("goupser_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}

	var binAsset, sumAsset *githubAsset
	for i := range rel.Assets {
		a := &rel.Assets[i]
		switch {
		case a.Name == binName:
			binAsset = a
		case strings.HasSuffix(a.Name, "_checksums.txt"):
			sumAsset = a
		}
	}
	if binAsset == nil {
		return "", fmt.Errorf("wydanie %s nie zawiera pliku %s", rel.TagName, binName)
	}
	if sumAsset == nil {
		return "", fmt.Errorf("wydanie %s nie zawiera sum kontrolnych", rel.TagName)
	}

	client := &http.Client{Timeout: downloadTimeout}

	wantSum, err := fetchChecksum(client, sumAsset.URL, binName)
	if err != nil {
		return "", err
	}

	// Nowy plik trafia obok starego, żeby podmiana była zwykłą zmianą nazwy.
	newPath := exe + ".new"
	fmt.Printf("Pobieranie %s...\n", binName)
	if err := downloadVerified(client, binAsset.URL, newPath, wantSum); err != nil {
		os.Remove(newPath)
		return "", err
	}

	if err := replaceExecutable(exe, newPath); err != nil {
		os.Remove(newPath)
		return "", fmt.Errorf("nie można podmienić pliku programu: %w", err)
	}
	return exe, nil
}

// fetchChecksum pobiera plik sum kontrolnych i zwraca sumę SHA-256 podanego pliku.
func fetchChecksum(client *http.Client, url, fileName string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("nie można pobrać sum kontrolnych: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("nie można pobrać sum kontrolnych: status %s", resp.Status)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == fileName {
			return strings.ToLower(fields[0]), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("nie można odczytać sum kontrolnych: %w", err)
	}
	return "", fmt.Errorf("brak sumy kontrolnej dla pliku %s", fileName)
}

// downloadVerified pobiera plik pod ścieżkę path i sprawdza jego sumę SHA-256.
func downloadVerified(client *http.Client, url, path, wantSum string) error {
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("błąd pobierania: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("błąd pobierania: status %s", resp.Status)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("nie można zapisać pliku: %w", err)
	}

	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(f, h), resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("błąd pobierania: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("nie można zapisać pliku: %w", closeErr)
	}

	if got := hex.EncodeToString(h.Sum(nil)); got != wantSum {
		return fmt.Errorf("nieprawidłowa suma kontrolna pobranego pliku")
	}
	return nil
}
