// Command tracklines is a terminal UI for TrackLin.es: your work, projects, boards and tickets,
// keyboard first (TL-463). main only wires it together; the parts are in internal/.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tracklines/tui/internal/api"
	"github.com/tracklines/tui/internal/config"
	"github.com/tracklines/tui/internal/i18n"
	"github.com/tracklines/tui/internal/reporting"
	"github.com/tracklines/tui/internal/ui"
)

// version is set at build time (-ldflags "-X main.version=v1.2.3"); "dev" never checks for updates.
var version = "dev"

// noKey is what to do without an API key.
func noKey() string {
	return i18n.T(`tracklines needs a personal API key (tl_p_…).

Make one at https://tracklin.es/account/api-keys, then either:
  mkdir -p ~/.config/tracklines && echo 'tl_p_…' > ~/.config/tracklines/key && chmod 600 ~/.config/tracklines/key
or set TRACKLINES_API_KEY.`)
}

// noTerminalText is what to do when there's no terminal to draw in.
func noTerminalText() string {
	return i18n.T("tracklines needs a terminal to draw in: run it from one, not from a script, a pipe or CI.")
}

// noTerminal: Bubble Tea couldn't open a terminal (stdin isn't one and /dev/tty can't be opened: CI, cron,
// an agent's shell). That's how it was started, not a crash, so it isn't reported to Bugfixes (TL-798).
// ponytail: Run's only file open is the TTY, so any open error is that.
func noTerminal(err error) bool {
	var pe *fs.PathError
	return errors.As(err, &pe) && pe.Op == "open"
}

func main() {
	settings := config.Load(config.Path())
	i18n.Set(settings.Language)
	key, err := config.APIKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, noKey())
		os.Exit(1)
	}
	base := os.Getenv("TRACKLINES_API_URL")
	if base == "" {
		base = "https://api.tracklin.es"
	}
	client := &api.Client{Base: base, Key: key, HTTP: http.DefaultClient, OnFailure: reporting.APIFailure}
	restore := reporting.Setup(version, settings.Reports)
	_, err = tea.NewProgram(ui.New(client, ui.Options{Version: version, VimKeys: settings.VimKeys, BacklogSort: settings.BacklogSort, UpdateCheck: settings.UpdateCheck, Language: settings.Language}), tea.WithAltScreen(), tea.WithReportFocus()).Run()
	if noTerminal(err) {
		restore()
		fmt.Fprintln(os.Stderr, noTerminalText())
		os.Exit(1)
	}
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
