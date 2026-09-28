package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

func getHTML(hideList bool) string {
	listContent := ""
	if !hideList {
		files, err := os.ReadDir(".")
		if err == nil {
			listContent += "<hr><h2>Aktualna lista plików w katalogu:</h2><ul>"
			for _, file := range files {
				if !file.IsDir() {
					listContent += fmt.Sprintf("<li>%s</li>", file.Name())
				}
			}
			listContent += "</ul>"
		}
	}

	return `
<!DOCTYPE html>
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
    </style>
</head>
<body>
    <h2>Wybierz pliki i naciśnij Prześlij</h2>
    <form id="uploadForm">
        <!-- Zmieniono przycisk, aby zachęcał do dodawania kolejnych plików -->
        <input type="file" id="fileInput" name="uploadfile" multiple /><br><br>
        <div id="selectedFilesList"></div>
        <input type="submit" value="Prześlij wszystkie wybrane pliki" />
    </form>

    <div id="progressContainer"></div>
    <div id="status"></div>

    ` + listContent + `

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

                xhr.open('POST', '/', true);
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
                            window.location.href = "/";
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

func main() {
	hideList := flag.Bool("brak-listy", false, "Ukrywa domyślną listę plików pod formularzem (tryb Blind Drop)")
	flag.Parse()

	gin.SetMode(gin.ReleaseMode)

	// r zastępuje router/silnik Gin
	r := gin.Default()

	r.GET("/", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(getHTML(*hideList)))
	})

	r.POST("/", func(c *gin.Context) {
		file, err := c.FormFile("uploadfile")
		if err != nil {
			c.String(http.StatusBadRequest, "Błąd pliku")
			return
		}

		targetPath := getUniqueFilename(file.Filename)
		if err := c.SaveUploadedFile(file, targetPath); err != nil {
			c.String(http.StatusInternalServerError, "Błąd zapisu")
			return
		}

		c.String(http.StatusOK, "OK")
	})

	fmt.Println("Serwer gotowy. Port: 8080")
	if *hideList {
		fmt.Println("Tryb podglądu listy plików: WYŁĄCZONY (Blind Drop)")
	} else {
		fmt.Println("Tryb podglądu listy plików: WŁĄCZONY (Domyślny)")
	}
	err := r.Run("0.0.0.0:8080")
	if err != nil {
		return
	}
}
