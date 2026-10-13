package main

import (
	"flag"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	defaultPort = 9000

	author  = "Maro"
	repoURL = "https://github.com/maro4444/goupser"

	// Tryby pracy serwera (wartości flagi -tryb).
	modeUpload = "wysylanie"
	modeServe  = "serwowanie"
	modeBoth   = "oba"

	// Angielskie odpowiedniki trybów, przyjmowane obok polskich nazw.
	modeUploadEN = "up"
	modeServeEN  = "down"
	modeBothEN   = "both"

	// Ścieżki używane w trybach z serwowaniem plików.
	uploadFormPath = "/wyslij"
	servePrefix    = "/pliki/"
)

// fileEntry opisuje plik pokazywany na liście do pobrania.
type fileEntry struct {
	Name string
	Size int64
}

const usageTemplate = `{PROG} - prosty serwer do wysyłania i pobierania plików w sieci lokalnej.
Wersja: {WERSJA}

Sposób użycia:
  {PROG} [flagi]

Tryby pracy (flaga -tryb, nazwa polska albo angielska):
  {WYSYLANIE}, {UP}      Odbieranie plików od użytkowników (domyślny). Formularz pod adresem GET /.
  {SERWOWANIE}, {DOWN}   Udostępnianie plików z katalogu, w którym uruchomiono program.
  {OBA}, {BOTH}          Odbieranie i udostępnianie plików jednocześnie.

Flagi:
  -port <liczba>
        Port nasłuchu, dozwolony zakres 1-65535 (domyślnie {PORT}).
  -tryb <nazwa>
        Tryb pracy: {WYSYLANIE} ({UP}), {SERWOWANIE} ({DOWN}) albo {OBA} ({BOTH})
        (domyślnie {WYSYLANIE}).
  -brak-listy
        Ukrywa listę plików pod formularzem (tryb Blind Drop).
        Ma znaczenie tylko w trybie {WYSYLANIE}.
  -bez-aktualizacji
        Nie sprawdza przy starcie, czy jest nowa wersja programu.
  -bez-stun
        Nie ustala przy starcie adresu zewnętrznego (publicznego) przez serwer STUN.
  -wszystkie-adresy
        Pokazuje przy starcie także adresy pozostałych interfejsów sieciowych
        (np. wirtualnych kart Hyper-V, WSL, VirtualBox).
  -v, -version
        Wyświetla wersję programu i kończy działanie.
  -credits
        Wyświetla informacje o autorze i kończy działanie.
  -h, -help
        Wyświetla ten opis i kończy działanie.

Adresy:
  tryb={WYSYLANIE}     GET  /                  formularz wysyłania plików
                     POST /                  przyjęcie pliku
  tryb={SERWOWANIE}    GET  /                  lista plików do pobrania
                     GET  {PLIKI}<nazwa>    pobranie pliku
  tryb={OBA}           GET  /                  lista plików do pobrania + przycisk "Wyślij pliki"
                     GET  {WYSLIJ}           formularz wysyłania plików
                     POST {WYSLIJ}           przyjęcie pliku
                     GET  {PLIKI}<nazwa>    pobranie pliku

Szczegóły:
  - Serwer nasłuchuje na wszystkich interfejsach (0.0.0.0) i wybranym porcie.
  - Pliki są zapisywane w katalogu, z którego uruchomiono program; jeśli plik o takiej
    nazwie już istnieje, do nazwy dodawany jest numer (np. zadanie_1.pdf).
  - Udostępniane i pokazywane na liście są wyłącznie pliki z katalogu uruchomienia:
    bez podkatalogów i bez plików, których nazwa zaczyna się kropką.
  - Wgrywanie plików o nazwie zaczynającej się kropką jest odrzucane.
  - Kliknięcie pliku na liście prosi o potwierdzenie, a następnie rozpoczyna pobieranie.
  - Postęp każdego pobierania (adres klienta, procent, rozmiar, prędkość) jest na bieżąco
    pokazywany w terminalu serwera.
  - Porty poniżej 1024 mogą wymagać uprawnień administratora.
  - Przy starcie program sprawdza na GitHubie, czy jest nowa wersja (tylko gdy jest
    internet). Po zgodzie użytkownika pobiera ją, podmienia siebie i uruchamia się ponownie.
  - Przy starcie program pokazuje adres w sieci lokalnej (interfejs z trasą domyślną)
    oraz adres zewnętrzny ustalony przez publiczny serwer STUN (Google, zapasowo
    Cloudflare). Adres zewnętrzny działa z internetu tylko po przekierowaniu portu
    na routerze. Adresy pozostałych interfejsów (np. wirtualnych Hyper-V/WSL) są
    pokazywane z flagą -wszystkie-adresy, a także wtedy, gdy nie ma trasy domyślnej.
  - Nazwy flag można podawać z jednym lub dwoma myślnikami (-port lub --port).

Przykłady:
  {PROG}
  {PROG} -port 8080
  {PROG} -tryb {SERWOWANIE} -port 8080
  {PROG} -tryb {OBA} -port 8080
  {PROG} -tryb {DOWN}
  {PROG} -brak-listy
`

// printUsage wypisuje opis programu, flag i trybów pracy.
func printUsage(w io.Writer) {
	r := strings.NewReplacer(
		"{PROG}", filepath.Base(os.Args[0]),
		"{WERSJA}", versionString(),
		"{WYSYLANIE}", modeUpload,
		"{SERWOWANIE}", modeServe,
		"{OBA}", modeBoth,
		"{UP}", modeUploadEN,
		"{DOWN}", modeServeEN,
		"{BOTH}", modeBothEN,
		"{PORT}", fmt.Sprintf("%d", defaultPort),
		"{PLIKI}", servePrefix,
		"{WYSLIJ}", uploadFormPath,
	)
	fmt.Fprint(w, r.Replace(usageTemplate))
}

const uploadPageTemplate = `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Prześlij Pracę</title>
    <style>
        body { font-family: sans-serif; margin: 40px; }
        .file-progress { margin-top: 10px; width: 300px; }
        progress { width: 100%; height: 20px; }
        .filename { font-size: 0.9em; color: #555; }
        #status { font-weight: bold; margin-top: 15px; }
        hr { margin-top: 30px; border: 0; border-top: 1px solid #ccc; }
        #selectedFilesList { margin-top: 10px; color: #333; font-style: italic; }
        .remove-btn { color: red; margin-left: 10px; cursor: pointer; font-size: 0.8em; font-style: normal; }
        .nav { margin-bottom: 20px; }
    </style>
</head>
<body>
    <h2>Wybierz pliki i naciśnij Prześlij</h2>
    {BACK_LINK}
    <form id="uploadForm">
        <!-- Zmieniono przycisk, aby zachęcał do dodawania kolejnych plików -->
        <input type="file" id="fileInput" name="uploadfile" multiple /><br><br>
        <div id="selectedFilesList"></div>
        <input type="submit" value="Prześlij wszystkie wybrane pliki" />
    </form>

    <div id="progressContainer"></div>
    <div id="status"></div>

    {LIST_CONTENT}

    <script>
        var fileInput = document.getElementById('fileInput');
        var selectedFilesList = document.getElementById('selectedFilesList');
        
        // Globalna tablica przechowująca skumulowane pliki do wysłania
        var filesQueue = [];

        // Reagowanie na dodanie nowych plików do pola wyboru
        fileInput.addEventListener('change', function() {
            for (var i = 0; i < fileInput.files.length; i++) {
                var file = fileInput.files[i];
                
                // Unikamy dodawania dokładnie tego samego pliku (o tej samej nazwie i rozmiarze) dwukrotnie
                var isDuplicate = filesQueue.some(function(f) {
                    return f.name === file.name && f.size === file.size;
                });
                
                if (!isDuplicate) {
                    filesQueue.push(file);
                }
            }
            updateSelectedFilesListUI();
            
            // Czyszczenie samego inputa, aby uczeń mógł kliknąć "Wybierz plik" ponownie 
            // i wybrać coś innego bez blokowania przeglądarki
            fileInput.value = '';
        });

        // Odświeżanie wyglądu listy plików na ekranie (przed wysłaniem)
        function updateSelectedFilesListUI() {
            if (filesQueue.length > 0) {
                var listHtml = '<strong>Wybrane pliki do przesłania:</strong><ul>';
                for (var i = 0; i < filesQueue.length; i++) {
                    listHtml += '<li>' + filesQueue[i].name + 
                                ' <span class="remove-btn" onclick="removeFromFileQueue(' + i + ')">[Usuń]</span></li>';
                }
                listHtml += '</ul><br>';
                selectedFilesList.innerHTML = listHtml;
            } else {
                selectedFilesList.innerHTML = '';
            }
        }

        // Możliwość usunięcia pliku z listy, jeśli uczeń się pomylił przed kliknięciem Wyślij
        window.removeFromFileQueue = function(index) {
            filesQueue.splice(index, 1);
            updateSelectedFilesListUI();
        };

        document.getElementById('uploadForm').addEventListener('submit', function(e) {
            e.preventDefault();
            
            if (filesQueue.length === 0) {
                alert('Najpierw wybierz przynajmniej jeden plik!');
                return;
            }

            var container = document.getElementById('progressContainer');
            var statusDiv = document.getElementById('status');
            container.innerHTML = ''; 
            
            statusDiv.innerText = 'Przesyłanie plików...';
            statusDiv.style.color = 'black';

            var activeUploads = filesQueue.length;
            var hasError = false;

            function uploadFile(file) {
                var formData = new FormData();
                formData.append('uploadfile', file);

                var xhr = new XMLHttpRequest();
                
                var wrapper = document.createElement('div');
                wrapper.className = 'file-progress';
                
                var nameDiv = document.createElement('div');
                nameDiv.className = 'filename';
                nameDiv.innerText = file.name;
                
                var progressBar = document.createElement('progress');
                progressBar.value = 0;
                progressBar.max = 100;
                
                var percentDiv = document.createElement('div');
                percentDiv.innerText = '0%';

                wrapper.appendChild(nameDiv);
                wrapper.appendChild(progressBar);
                wrapper.appendChild(percentDiv);
                container.appendChild(wrapper);

                xhr.upload.addEventListener('progress', function(e) {
                    if (e.lengthComputable) {
                        var percent = Math.round((e.loaded / e.total) * 100);
                        progressBar.value = percent;
                        percentDiv.innerText = percent + '%';
                    }
                });

                xhr.onload = function() {
                    activeUploads--;
                    if (xhr.status !== 200) {
                        hasError = true;
                    }
                    checkCompletion();
                };

                xhr.onerror = function() {
                    activeUploads--;
                    hasError = true;
                    checkCompletion();
                };

                xhr.open('POST', '{UPLOAD_URL}', true);
                xhr.send(formData);
            }

            function checkCompletion() {
                if (activeUploads === 0) {
                    if (hasError) {
                        statusDiv.innerText = 'Wystąpił błąd podczas przesyłania niektórych plików.';
                        statusDiv.style.color = 'red';
                    } else {
                        statusDiv.innerText = 'Wszystkie pliki zostały przesłane pomyślnie!';
                        statusDiv.style.color = 'green';
                        
                        setTimeout(function() {
                            document.getElementById('uploadForm').reset();
                            selectedFilesList.innerHTML = '';
                            filesQueue = []; // Reset kolejki plików
                            window.location.href = '{RETURN_URL}';
                        }, 2000);
                    }
                }
            }

            // Wysłanie wszystkich zgromadzonych w tablicy plików
            for (var i = 0; i < filesQueue.length; i++) {
                uploadFile(filesQueue[i]);
            }
        });
    </script>
</body>
</html>`

const listPageTemplate = `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Pliki do pobrania</title>
    <style>
        body { font-family: sans-serif; margin: 40px; }
        h2 { margin-bottom: 5px; }
        .info { color: #555; font-size: 0.9em; margin-bottom: 20px; }
        .przycisk { display: inline-block; margin-bottom: 20px; padding: 10px 18px; background: #0645ad;
                    color: #fff; text-decoration: none; border-radius: 4px; font-weight: bold; }
        .przycisk:hover { background: #053a8a; }
        ul.pliki { list-style: none; padding: 0; max-width: 700px; }
        ul.pliki li { border-bottom: 1px solid #eee; padding: 10px 4px; display: flex;
                      justify-content: space-between; align-items: baseline; }
        ul.pliki a { text-decoration: none; color: #0645ad; font-size: 1.05em; word-break: break-all; }
        ul.pliki a:hover { text-decoration: underline; }
        .rozmiar { color: #777; font-size: 0.85em; white-space: nowrap; padding-left: 15px; }
        .pusto { color: #777; font-style: italic; }
    </style>
</head>
<body>
    <h2>Pliki do pobrania</h2>
    <div class="info">Kliknij nazwę pliku, aby go pobrać (poprosimy o potwierdzenie).</div>
    {UPLOAD_BUTTON}
    {FILES}
    <script>
        function potwierdzPobranie(el) {
            return confirm('Czy pobrać plik "' + el.getAttribute('data-nazwa') + '"?');
        }
    </script>
</body>
</html>`

// renderUploadPage buduje stronę z formularzem wysyłania plików.
// uploadURL to adres, na który trafia POST; returnURL to adres otwierany po udanym wysłaniu.
func renderUploadPage(hideList bool, uploadURL, returnURL string, backToList bool) string {
	backLink := ""
	if backToList {
		backLink = `<p class="nav"><a href="/">&larr; Wróć do listy plików</a></p>`
	}

	r := strings.NewReplacer(
		"{UPLOAD_URL}", uploadURL,
		"{RETURN_URL}", returnURL,
		"{LIST_CONTENT}", renderFormFileList(hideList),
		"{BACK_LINK}", backLink,
	)
	return r.Replace(uploadPageTemplate)
}

// renderFormFileList zwraca listę plików pokazywaną pod formularzem (albo pusty tekst).
func renderFormFileList(hideList bool) string {
	if hideList {
		return ""
	}

	files, err := listServableFiles()
	if err != nil {
		return ""
	}
	if len(files) == 0 {
		return "<hr><h2>Aktualna lista plików w katalogu:</h2><p>Brak plików.</p>"
	}

	var b strings.Builder
	b.WriteString("<hr><h2>Aktualna lista plików w katalogu:</h2><ul>")
	for _, f := range files {
		b.WriteString("<li>" + html.EscapeString(f.Name) + "</li>")
	}
	b.WriteString("</ul>")
	return b.String()
}

// renderListPage buduje stronę z listą plików do pobrania.
// showUploadButton dodaje przycisk prowadzący do formularza wysyłania (tryb oba).
func renderListPage(files []fileEntry, showUploadButton bool) string {
	button := ""
	if showUploadButton {
		button = `<a class="przycisk" href="` + uploadFormPath + `">Wyślij pliki</a>`
	}

	r := strings.NewReplacer(
		"{UPLOAD_BUTTON}", button,
		"{FILES}", renderFilesList(files),
	)
	return r.Replace(listPageTemplate)
}

// renderFilesList zwraca listę linków do pobrania wraz z rozmiarami plików.
func renderFilesList(files []fileEntry) string {
	if len(files) == 0 {
		return `<p class="pusto">Brak plików w katalogu.</p>`
	}

	var b strings.Builder
	b.WriteString(`<ul class="pliki">`)
	for _, f := range files {
		name := html.EscapeString(f.Name)
		href := html.EscapeString(servePrefix + url.PathEscape(f.Name))
		b.WriteString(`<li><a href="` + href + `" download="` + name + `" data-nazwa="` + name +
			`" onclick="return potwierdzPobranie(this)">` + name + `</a>` +
			`<span class="rozmiar">` + humanSize(f.Size) + `</span></li>`)
	}
	b.WriteString(`</ul>`)
	return b.String()
}

// listServableFiles zwraca pliki z katalogu uruchomienia, które mogą być udostępnione:
// bez podkatalogów i bez plików, których nazwa zaczyna się kropką.
func listServableFiles() ([]fileEntry, error) {
	entries, err := os.ReadDir(".")
	if err != nil {
		return nil, err
	}

	files := make([]fileEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, fileEntry{Name: entry.Name(), Size: info.Size()})
	}

	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
	return files, nil
}

// isServableName sprawdza, czy nazwa pliku jest bezpieczna do udostępnienia.
// Odrzuca ścieżki, katalogi oraz pliki ukryte.
func isServableName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	return name == filepath.Base(name)
}

// sanitizeUploadName zwraca bezpieczną nazwę wgrywanego pliku albo pusty tekst,
// jeśli nazwa jest niedozwolona.
func sanitizeUploadName(raw string) string {
	name := filepath.Base(strings.ReplaceAll(raw, `\`, "/"))
	if !isServableName(name) {
		return ""
	}
	return name
}

// humanSize zamienia liczbę bajtów na czytelny opis, np. "1.5 MB".
func humanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}

	div, exp := int64(unit), 0
	for rest := size / unit; rest >= unit; rest /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGT"[exp])
}

// validatePort sprawdza, czy port mieści się w dozwolonym zakresie.
func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("nieprawidłowy port %d: dozwolony zakres to 1-65535", port)
	}
	return nil
}

// normalizeMode zamienia podaną nazwę trybu (polską albo angielską) na polską nazwę,
// której używa reszta programu. Zwraca false, gdy tryb nie jest obsługiwany.
func normalizeMode(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case modeUpload, modeUploadEN:
		return modeUpload, true
	case modeServe, modeServeEN:
		return modeServe, true
	case modeBoth, modeBothEN:
		return modeBoth, true
	default:
		return "", false
	}
}

func getUniqueFilename(filename string) string {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return filename
	}
	ext := filepath.Ext(filename)
	base := filename[:len(filename)-len(ext)]
	counter := 1
	for {
		newName := fmt.Sprintf("%s_%d%s", base, counter, ext)
		if _, err := os.Stat(newName); os.IsNotExist(err) {
			return newName
		}
		counter++
	}
}

// handleUpload zapisuje przesłany plik w katalogu uruchomienia programu.
func handleUpload(c *gin.Context) {
	file, err := c.FormFile("uploadfile")
	if err != nil {
		c.String(http.StatusBadRequest, "Błąd pliku")
		return
	}

	targetName := sanitizeUploadName(file.Filename)
	if targetName == "" {
		c.String(http.StatusBadRequest, "Nieprawidłowa nazwa pliku")
		return
	}

	targetPath := getUniqueFilename(targetName)
	if err := c.SaveUploadedFile(file, targetPath); err != nil {
		c.String(http.StatusInternalServerError, "Błąd zapisu")
		return
	}

	c.String(http.StatusOK, "OK")
}

// handleServeFile wysyła plik z katalogu uruchomienia programu,
// pokazując postęp pobierania w terminalu.
func handleServeFile(board *progressBoard) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		if !isServableName(name) {
			c.String(http.StatusNotFound, "Nie znaleziono pliku")
			return
		}

		workDir, err := os.Getwd()
		if err != nil {
			c.String(http.StatusInternalServerError, "Błąd serwera")
			return
		}

		fullPath := filepath.Join(workDir, name)
		info, err := os.Stat(fullPath)
		if err != nil || info.IsDir() {
			c.String(http.StatusNotFound, "Nie znaleziono pliku")
			return
		}

		// Bez "attachment" przeglądarka wyświetla pliki tekstowe, PDF-y czy obrazki
		// zamiast je pobrać.
		disposition := mime.FormatMediaType("attachment", map[string]string{"filename": name})
		if disposition == "" {
			disposition = "attachment"
		}
		c.Header("Content-Disposition", disposition)
		// Bez tego przeglądarka może pokazać zapamiętaną kopię pliku, nie pytając serwera.
		c.Header("Cache-Control", "no-store")

		t := board.start(c.ClientIP(), name)
		c.Writer = &countingWriter{ResponseWriter: c.Writer, t: t}
		c.File(fullPath)
		board.finish(t, c.Writer.Status())
	}
}

// handleListPage pokazuje listę plików dostępnych do pobrania.
func handleListPage(showUploadButton bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		files, err := listServableFiles()
		if err != nil {
			c.String(http.StatusInternalServerError, "Błąd odczytu katalogu")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(renderListPage(files, showUploadButton)))
	}
}

func main() {
	port := flag.Int("port", defaultPort, "Port nasłuchu serwera (zakres 1-65535)")
	tryb := flag.String("tryb", modeUpload, "Tryb pracy: wysylanie (up), serwowanie (down) albo oba (both)")
	hideList := flag.Bool("brak-listy", false, "Ukrywa listę plików pod formularzem (tryb Blind Drop)")
	noUpdate := flag.Bool("bez-aktualizacji", false, "Nie sprawdza, czy jest nowa wersja programu")
	noSTUN := flag.Bool("bez-stun", false, "Nie ustala adresu zewnętrznego przez serwer STUN")
	allIfaces := flag.Bool("wszystkie-adresy", false, "Pokazuje też adresy pozostałych interfejsów (np. wirtualnych)")

	var showHelp bool
	flag.BoolVar(&showHelp, "help", false, "Wyświetla opis programu i kończy działanie")
	flag.BoolVar(&showHelp, "h", false, "Skrót flagi -help")

	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "Wyświetla wersję programu i kończy działanie")
	flag.BoolVar(&showVersion, "v", false, "Skrót flagi -version")
	showCredits := flag.Bool("credits", false, "Wyświetla informacje o autorze i kończy działanie")

	flag.Usage = func() { printUsage(os.Stderr) }
	flag.Parse()

	if showHelp {
		printUsage(os.Stdout)
		return
	}
	if showVersion {
		fmt.Printf("goupser %s\n", versionString())
		return
	}
	if *showCredits {
		fmt.Printf("goupser %s\n", versionString())
		fmt.Printf("Autor: %s\n", author)
		fmt.Printf("Strona: %s\n", repoURL)
		fmt.Printf("2026\n")
		return
	}

	if err := validatePort(*port); err != nil {
		fmt.Fprintf(os.Stderr, "Błąd: %v\n\n", err)
		fmt.Fprintf(os.Stderr, "Wpisz '%s --help', aby zobaczyć dostępne opcje.\n", filepath.Base(os.Args[0]))
		os.Exit(2)
	}
	mode, ok := normalizeMode(*tryb)
	if !ok {
		fmt.Fprintf(os.Stderr, "Błąd: nieznany tryb pracy %q. Dozwolone wartości: %s (%s), %s (%s), %s (%s).\n\n",
			*tryb, modeUpload, modeUploadEN, modeServe, modeServeEN, modeBoth, modeBothEN)
		fmt.Fprintf(os.Stderr, "Wpisz '%s --help', aby zobaczyć dostępne opcje.\n", filepath.Base(os.Args[0]))
		os.Exit(2)
	}

	cleanupOldExecutable()
	if !*noUpdate {
		checkForUpdate()
	}

	gin.SetMode(gin.ReleaseMode)

	// Logi Gina idą przez tablicę postępu, żeby nie rozbijały pasków pobierania.
	board := newProgressBoard(os.Stdout)
	gin.DefaultWriter = board
	gin.DefaultErrorWriter = board

	// r zastępuje router/silnik Gin
	r := gin.Default()

	r.NoRoute(func(c *gin.Context) {
		c.String(http.StatusNotFound, "Nie znaleziono zasobu (404)")
	})

	switch mode {
	case modeUpload:
		r.GET("/", func(c *gin.Context) {
			page := renderUploadPage(*hideList, "/", "/", false)
			c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
		})
		r.POST("/", handleUpload)

	case modeServe:
		r.GET("/", handleListPage(false))
		r.GET("/pliki/:name", handleServeFile(board))

	case modeBoth:
		r.GET("/", handleListPage(true))
		r.GET(uploadFormPath, func(c *gin.Context) {
			// Lista plików jest na stronie głównej, więc pod formularzem jej nie powtarzamy.
			page := renderUploadPage(true, uploadFormPath, "/", true)
			c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
		})
		r.POST(uploadFormPath, handleUpload)
		r.GET("/pliki/:name", handleServeFile(board))
	}

	workDir, err := os.Getwd()
	if err != nil {
		workDir = "."
	}

	fmt.Printf("goupser %s\n", versionString())
	fmt.Printf("Serwer gotowy. Port: %d (tryb: %s)\n", *port, mode)
	fmt.Printf("Katalog plików: %s\n", workDir)

	printAddr := func(label, host string) {
		switch mode {
		case modeUpload:
			fmt.Printf("%s http://%s:%d/ (formularz wysyłania plików)\n", label, host, *port)
		case modeServe:
			fmt.Printf("%s http://%s:%d/ (lista plików do pobrania)\n", label, host, *port)
		case modeBoth:
			fmt.Printf("%s http://%s:%d/ (lista plików), http://%s:%d%s (wysyłanie plików)\n",
				label, host, *port, host, *port, uploadFormPath)
		}
	}

	printAddr("Adres na tym komputerze:", "localhost")
	primary, hasRoute := primaryLANAddr()
	if hasRoute {
		printAddr("Adres w sieci lokalnej: ", primary)
	}
	// Pozostałe interfejsy to zwykle karty wirtualne (Hyper-V, WSL), więc pokazujemy je
	// tylko na życzenie. Bez trasy domyślnej nie wiadomo, który adres jest właściwy,
	// więc wtedy pokazujemy wszystkie.
	if !hasRoute || *allIfaces {
		for _, ip := range lanIPv4Addrs() {
			if ip == primary {
				continue
			}
			if hasRoute {
				printAddr("Inny interfejs:         ", ip)
			} else {
				printAddr("Adres w sieci lokalnej: ", ip)
			}
		}
	}
	// Bez trasy domyślnej nie ma internetu, więc nie ma też kogo pytać przez STUN.
	if hasRoute && !*noSTUN {
		if ip, err := publicAddrSTUN(); err == nil && ip != primary {
			printAddr("Adres zewnętrzny:       ", ip)
			fmt.Printf("  (działa z internetu tylko po przekierowaniu portu %d na routerze)\n", *port)
		}
	}

	if *hideList {
		if mode == modeUpload {
			fmt.Println("Tryb podglądu listy plików: WYŁĄCZONY (Blind Drop)")
		} else {
			fmt.Println("Uwaga: flaga -brak-listy jest ignorowana w tym trybie (lista plików jest częścią serwowania).")
		}
	} else if mode == modeUpload {
		fmt.Println("Tryb podglądu listy plików: WŁĄCZONY (Domyślny)")
	}

	if *port < 1024 {
		fmt.Println("Uwaga: porty poniżej 1024 mogą wymagać uprawnień administratora.")
	}

	if err := r.Run(fmt.Sprintf("0.0.0.0:%d", *port)); err != nil {
		fmt.Fprintf(os.Stderr, "Nie udało się uruchomić serwera na porcie %d: %v\n", *port, err)
		os.Exit(1)
	}
}
