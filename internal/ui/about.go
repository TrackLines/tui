package ui

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tracklines/tui/internal/config"
	"github.com/tracklines/tui/internal/update"
)

// aboutView is ? : the version you're running, whether there's a newer one (u installs it), and the
// keys that work everywhere.
type aboutView struct{ updating bool }

type updatedMsg struct {
	tag string
	err error
}

func (v *aboutView) load(a *app) tea.Cmd {
	a.loading = false
	return nil
}

func (v *aboutView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case updatedMsg:
		v.updating = false
		if msg.err != nil {
			a.status = warn.Render(msg.err.Error())
			return v, nil
		}
		a.updated, a.latest = msg.tag, nil
		a.status = ok.Render("Updated to " + msg.tag + ". Quit and start tracklines again to use it.")
	case tea.KeyMsg:
		if msg.String() != "u" || a.latest == nil || v.updating {
			return v, nil
		}
		v.updating, a.status = true, muted.Render("Downloading "+a.latest.Tag+"…")
		r := *a.latest
		return v, func() tea.Msg {
			return updatedMsg{r.Tag, update.SelfUpdate(context.Background(), http.DefaultClient, r)}
		}
	}
	return v, nil
}

func (v *aboutView) render(a *app) string {
	var b strings.Builder
	b.WriteString(accent.Render("TrackLin.es for the terminal") + "\n\n")
	b.WriteString("Version  " + bold.Render(version) + muted.Render("  ("+runtime.GOOS+"/"+runtime.GOARCH+")") + "\n")
	switch {
	case a.updated != "":
		b.WriteString(ok.Render("Updated to "+a.updated+": restart to use it.") + "\n")
	case a.latest != nil:
		b.WriteString(ok.Render(a.latest.Tag+" is out.") + "  " + bold.Render("u") + " to update\n")
	case !update.IsRelease(version):
		b.WriteString(muted.Render("A development build: it doesn’t check for updates.") + "\n")
	case !updateCheck:
		b.WriteString(muted.Render("Update checks are off (update_check in "+config.Path()+").") + "\n")
	default:
		b.WriteString(muted.Render("Up to date.") + "\n")
	}
	keys, other := "vim", "standard"
	if !vimKeys {
		keys, other = "standard", "vim"
	}
	b.WriteString("Backlog  " + bold.Render(backlogSort) + muted.Render("  first (backlog_sort in "+config.Path()+")") + "\n")
	b.WriteString("Keys     " + bold.Render(keys) + muted.Render("  (for "+other+": keys = "+other+" in "+config.Path()+")") + "\n")
	b.WriteString("\n" + bold.Render("Everywhere") + "\n")
	for _, k := range [][2]string{
		{"1 / 2", "My work (Created by me, Mentions) / Projects"},
		{"/", "search everything"},
		{"t", "go to a ticket by its key (TL-308)"},
		{"esc / q", "back (q quits on My work and Projects)"},
		{"Q", "quit from anywhere"},
		{"?", "this screen"},
	} {
		b.WriteString(fmt.Sprintf("  %-8s %s\n", k[0], k[1]))
	}
	b.WriteString("\n" + muted.Render("Each screen lists its own keys at the bottom. More: github.com/TrackLines/tui") + "\n")
	return b.String()
}

func (v *aboutView) help() string {
	if v.updating {
		return "updating…"
	}
	return "u update (when there's one) · esc/q back"
}
func (v *aboutView) typing() bool { return false }
