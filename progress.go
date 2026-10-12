package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	progressRefresh  = 300 * time.Millisecond
	progressBarWidth = 20
	progressNameMax  = 24
)

// transfer opisuje jedno trwające pobieranie pliku.
type transfer struct {
	client string
	name   string
	start  time.Time
	done   atomic.Int64
	total  atomic.Int64 // -1, dopóki nie znamy Content-Length odpowiedzi
}

// progressBoard pokazuje w terminalu paski postępu trwających pobierań.
// Wszystkie inne komunikaty (np. logi Gina) muszą przechodzić przez Write,
// żeby nie rozbijały rysowanych pasków.
type progressBoard struct {
	mu     sync.Mutex
	out    *os.File
	live   bool // czy terminal obsługuje sekwencje ANSI (rysowanie na żywo)
	active []*transfer
	drawn  int // liczba linii aktualnie narysowanych pod logami
}

func newProgressBoard(out *os.File) *progressBoard {
	b := &progressBoard{out: out, live: enableANSI(out)}
	if b.live {
		go b.loop()
	}
	return b
}

// Write wypisuje komunikat nad paskami postępu i rysuje je ponownie.
func (b *progressBoard) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	var sb strings.Builder
	b.clearTo(&sb)
	sb.Write(p)
	b.drawTo(&sb)
	if _, err := b.out.WriteString(sb.String()); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (b *progressBoard) logf(format string, args ...any) {
	fmt.Fprintf(b, format+"\n", args...)
}

func (b *progressBoard) loop() {
	ticker := time.NewTicker(progressRefresh)
	defer ticker.Stop()
	for range ticker.C {
		b.mu.Lock()
		if len(b.active) > 0 || b.drawn > 0 {
			var sb strings.Builder
			b.clearTo(&sb)
			b.drawTo(&sb)
			b.out.WriteString(sb.String())
		}
		b.mu.Unlock()
	}
}

// clearTo dopisuje sekwencję usuwającą narysowane wcześniej paski.
func (b *progressBoard) clearTo(sb *strings.Builder) {
	if b.drawn > 0 {
		fmt.Fprintf(sb, "\x1b[%dA\x1b[J", b.drawn)
		b.drawn = 0
	}
}

// drawTo dopisuje paski wszystkich trwających pobierań.
func (b *progressBoard) drawTo(sb *strings.Builder) {
	if !b.live || len(b.active) == 0 {
		return
	}
	now := time.Now()
	// Wyłączamy zawijanie linii, żeby zbyt długa linia nie zepsuła liczenia wierszy.
	sb.WriteString("\x1b[?7l")
	fmt.Fprintf(sb, "Trwające pobierania (%d):\n", len(b.active))
	for _, t := range b.active {
		sb.WriteString(t.line(now))
		sb.WriteString("\n")
	}
	sb.WriteString("\x1b[?7h")
	b.drawn = len(b.active) + 1
}

// start rejestruje nowe pobieranie.
func (b *progressBoard) start(client, name string) *transfer {
	t := &transfer{client: client, name: name, start: time.Now()}
	t.total.Store(-1)

	b.mu.Lock()
	b.active = append(b.active, t)
	b.mu.Unlock()

	b.logf("Rozpoczęto pobieranie: %s -> %s", name, client)
	return t
}

// finish usuwa pobieranie z listy i wypisuje jego podsumowanie.
func (b *progressBoard) finish(t *transfer, status int) {
	b.mu.Lock()
	for i, a := range b.active {
		if a == t {
			b.active = append(b.active[:i], b.active[i+1:]...)
			break
		}
	}
	b.mu.Unlock()

	done, total := t.done.Load(), t.total.Load()
	elapsed := time.Since(t.start).Round(100 * time.Millisecond)
	switch {
	case total < 0:
		b.logf("Zakończono żądanie: %s -> %s (status %d, przesłano %s)", t.name, t.client, status, humanSize(done))
	case done >= total:
		b.logf("Zakończono pobieranie: %s -> %s (%s w %s)", t.name, t.client, humanSize(done), elapsed)
	default:
		b.logf("Przerwano pobieranie: %s -> %s (%s z %s)", t.name, t.client, humanSize(done), humanSize(total))
	}
}

// line zwraca opis postępu pobierania w jednej linii.
func (t *transfer) line(now time.Time) string {
	done, total := t.done.Load(), t.total.Load()
	speed := ""
	if secs := now.Sub(t.start).Seconds(); secs > 0 {
		speed = humanSize(int64(float64(done)/secs)) + "/s"
	}
	name := shortenName(t.name, progressNameMax)

	if total <= 0 {
		return fmt.Sprintf("  %-15s %-24s %10s  %s", t.client, name, humanSize(done), speed)
	}

	return fmt.Sprintf("  %-15s %-24s %s  %s", t.client, name, progressBar(done, total), speed)
}

// progressBar zwraca pasek postępu z procentem i rozmiarem, np. "[███░░] 60%  6.0 MB / 10.0 MB".
func progressBar(done, total int64) string {
	ratio := float64(done) / float64(total)
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * progressBarWidth)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", progressBarWidth-filled)
	return fmt.Sprintf("[%s] %3d%%  %s / %s", bar, int(ratio*100), humanSize(done), humanSize(total))
}

// shortenName skraca nazwę do max znaków, dodając wielokropek.
func shortenName(name string, max int) string {
	r := []rune(name)
	if len(r) <= max {
		return name
	}
	return string(r[:max-1]) + "…"
}

// countingWriter liczy bajty wysłane do klienta w ramach pobierania.
type countingWriter struct {
	gin.ResponseWriter
	t *transfer
}

func (w *countingWriter) Write(p []byte) (int, error) {
	if w.t.total.Load() < 0 {
		// Content-Length jest już ustawiony przed pierwszym zapisem treści;
		// przy żądaniach Range jest to rozmiar wysyłanego fragmentu, a nie całego pliku.
		if n, err := strconv.ParseInt(w.Header().Get("Content-Length"), 10, 64); err == nil {
			w.t.total.Store(n)
		}
	}
	n, err := w.ResponseWriter.Write(p)
	w.t.done.Add(int64(n))
	return n, err
}
