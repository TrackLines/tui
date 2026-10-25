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
		a.status = ok.Render(F("Updated to %s. Quit and start tracklines again to use it.", msg.tag))
	case tea.KeyMsg:
		if msg.String() != "u" || a.latest == nil || v.updating {
			return v, nil
		}
		v.updating, a.status = true, muted.Render(F("Downloading %s…", a.latest.Tag))
		r := *a.latest
		return v, func() tea.Msg {
			return updatedMsg{r.Tag, update.SelfUpdate(context.Background(), http.DefaultClient, r)}
		}
	}
	return v, nil
}

func (v *aboutView) render(a *app) string {
	var b strings.Builder
	b.WriteString(accent.Render(T("TrackLin.es for the terminal")) + "\n\n")
	b.WriteString(T("Version") + "  " + bold.Render(version) + muted.Render("  ("+runtime.GOOS+"/"+runtime.GOARCH+")") + "\n")
	switch {
	case a.updated != "":
		b.WriteString(ok.Render(F("Updated to %s: restart to use it.", a.updated)) + "\n")
	case a.latest != nil:
		b.WriteString(ok.Render(F("%s is out.", a.latest.Tag)) + "  " + F("%s to update", bold.Render("u")) + "\n")
	case !update.IsRelease(version):
		b.WriteString(muted.Render(T("A development build: it doesn’t check for updates.")) + "\n")
	case !updateCheck:
		b.WriteString(muted.Render(F("Update checks are off (update_check in %s).", config.Path())) + "\n")
	default:
		b.WriteString(muted.Render(T("Up to date.")) + "\n")
	}
	keys, other := "vim", "standard"
	if !vimKeys {
		keys, other = "standard", "vim"
	}
	b.WriteString(T("Backlog") + "  " + bold.Render(T(backlogSort)) + muted.Render("  "+F("first (backlog_sort in %s)", config.Path())) + "\n")
	b.WriteString(T("Keys") + "  " + bold.Render(keys) + muted.Render("  "+F("(for %s: keys = %s in %s)", other, other, config.Path())) + "\n")
	b.WriteString("\n" + bold.Render(T("Everywhere")) + "\n")
	for _, k := range [][2]string{
		{"1 / 2", T("My work (Created by me, Mentions) / Projects")},
		{"/", T("search everything")},
		{"t", T("go to a ticket by its key (TL-308)")},
		{"D", T("unsent drafts: comments and edits that didn't reach the server")},
		{"esc / q", T("back (q quits on My work and Projects)")},
		{"Q", T("quit from anywhere")},
		{"?", T("this screen")},
	} {
		b.WriteString(fmt.Sprintf("  %-8s %s\n", k[0], k[1]))
	}
	b.WriteString("\n" + muted.Render(T("Each screen lists its own keys at the bottom. More: github.com/TrackLines/tui")) + "\n")
	return b.String()
}

func (v *aboutView) help() string {
	if v.updating {
		return T("updating…")
	}
	return T("u update (when there's one) · esc/q back")
}
func (v *aboutView) typing() bool { return false }
