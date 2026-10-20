// Command tracklines is a terminal UI for TrackLin.es: your work, projects, boards and tickets,
// keyboard first (TL-463). main only wires it together; the parts are in internal/.
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tracklines/tui/internal/api"
	"github.com/tracklines/tui/internal/config"
	"github.com/tracklines/tui/internal/reporting"
	"github.com/tracklines/tui/internal/ui"
)

// version is set at build time (-ldflags "-X main.version=v1.2.3"); "dev" never checks for updates.
var version = "dev"

const noKey = `tracklines needs a personal API key (tl_p_…).

Make one at https://tracklin.es/account/api-keys, then either:
  mkdir -p ~/.config/tracklines && echo 'tl_p_…' > ~/.config/tracklines/key && chmod 600 ~/.config/tracklines/key
or set TRACKLINES_API_KEY.`

func main() {
	key, err := config.APIKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, noKey)
		os.Exit(1)
	}
	base := os.Getenv("TRACKLINES_API_URL")
	if base == "" {
		base = "https://api.tracklin.es"
	}
	client := &api.Client{Base: base, Key: key, HTTP: http.DefaultClient, OnFailure: reporting.APIFailure}
	settings := config.Load(config.Path())
	restore := reporting.Setup(version)
	_, err = tea.NewProgram(ui.New(client, ui.Options{Version: version, VimKeys: settings.VimKeys, BacklogSort: settings.BacklogSort}), tea.WithAltScreen()).Run()
	if err != nil {
		// ponytail: Bubble Tea recovers a panic and prints its stack itself; Bugfixes gets that it happened
		reporting.Report("tui stopped: %v", err)
		time.Sleep(2 * time.Second) // the report goes in the background: let it leave before we exit
	}
	restore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
