// Command tracklines is a terminal UI for TrackLin.es: your open tickets, search, and moving and
// commenting on tickets, keyboard first (TL-463).
package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const usage = `tracklines needs a personal API key (tl_p_…).

Make one at https://tracklin.es/account/api-keys, then either:
  mkdir -p ~/.config/tracklines && echo 'tl_p_…' > ~/.config/tracklines/key && chmod 600 ~/.config/tracklines/key
or set TRACKLINES_API_KEY.`

func main() {
	key, err := apiKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}
	base := os.Getenv("TRACKLINES_API_URL")
	if base == "" {
		base = "https://api.tracklin.es"
	}
	api := &Client{Base: base, Key: key, HTTP: http.DefaultClient}
	restore := setupBugfixes()
	_, err = tea.NewProgram(newApp(api), tea.WithAltScreen()).Run()
	if err != nil {
		// ponytail: Bubble Tea recovers a panic and prints its stack itself; Bugfixes gets that it happened
		report("tui stopped: %v", err)
		time.Sleep(2 * time.Second) // the report goes in the background: let it leave before we exit
	}
	restore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// apiKey is TRACKLINES_API_KEY, or the first line of $XDG_CONFIG_HOME/tracklines/key (~/.config).
func apiKey() (string, error) {
	if k := strings.TrimSpace(os.Getenv("TRACKLINES_API_KEY")); k != "" {
		return k, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, "tracklines", "key"))
	if err != nil {
		return "", err
	}
	k, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	if k == "" {
		return "", fmt.Errorf("empty key file")
	}
	return k, nil
}
