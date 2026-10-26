// gen bakes the help answers into the TUI (TL-657): `go generate ./internal/help` fetches the backend's
// .po export for each language into baked/. TRACKLINES_API_URL picks another backend (staging).
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var languages = []string{"en", "nl", "de", "es", "pt", "fr", "tlh"}

func main() {
	base := os.Getenv("TRACKLINES_API_URL")
	if base == "" {
		base = "https://api.tracklin.es"
	}
	client := &http.Client{Timeout: 30 * time.Second}
	for _, l := range languages {
		res, err := client.Get(strings.TrimRight(base, "/") + "/api/help/export.po?locale=" + l)
		if err != nil {
			fail(l, err)
		}
		b, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
		res.Body.Close()
		if err != nil {
			fail(l, err)
		}
		if res.StatusCode != http.StatusOK || !strings.Contains(string(b), "msgctxt") {
			fail(l, fmt.Errorf("HTTP %d, not a help .po", res.StatusCode))
		}
		if err := os.WriteFile(filepath.Join("baked", "help."+l+".po"), b, 0o644); err != nil {
			fail(l, err)
		}
		fmt.Printf("help.%s.po: %d bytes\n", l, len(b))
	}
}

// fail stops without touching the rest: the answers baked last time stay.
func fail(lang string, err error) {
	fmt.Fprintf(os.Stderr, "help %s: %v\n", lang, err)
	os.Exit(1)
}
