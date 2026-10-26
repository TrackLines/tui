package ui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tracklines/tui/internal/help"
)

// helpView is the help answers (TL-657), from ? then h: type to search them, enter opens the page an
// answer points to on the website. They're the ones staff write in the admin panel, kept up to date
// in the background, so this screen redraws when newer ones arrive.
type helpView struct {
	input  textinput.Model
	cursor int
}

// helpUpdatedMsg: newer answers arrived; the screen showing them redraws.
type helpUpdatedMsg struct{}

func newHelp() *helpView {
	in := textinput.New()
	in.Placeholder = T("e.g. add a board")
	in.Prompt = "? "
	in.Focus()
	return &helpView{input: in}
}

// fetchHelp asks for newer answers, quietly: a 304 or no network keeps the ones held.
func (a *app) fetchHelp() tea.Cmd {
	if a.help == nil {
		return nil
	}
	return func() tea.Msg {
		if changed, _ := a.help.Fetch(context.Background(), a.api.Help); changed {
			return helpUpdatedMsg{}
		}
		return nil
	}
}

func (v *helpView) found(a *app) []help.Answer {
	if a.help == nil {
		return nil
	}
	return help.Search(a.help.Answers(), v.input.Value())
}

func (v *helpView) load(a *app) tea.Cmd {
	a.loading = false
	return textinput.Blink
}

func (v *helpView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	if k, isKey := msg.(tea.KeyMsg); isKey {
		found := v.found(a)
		switch k.String() {
		case "esc":
			return v, msgOf(popMsg{})
		case "up", "ctrl+p":
			v.cursor = max(v.cursor-1, 0)
			return v, nil
		case "down", "ctrl+n":
			v.cursor = min(v.cursor+1, max(len(found)-1, 0))
			return v, nil
		case "enter":
			if v.cursor >= len(found) {
				return v, nil
			}
			project, board := a.here()
			link := help.Link(siteURL(a.api.Base), found[v.cursor], project, board)
			if link == "" {
				a.status = muted.Render(T("That answer has no page to open."))
				return v, nil
			}
			if err := openFile(link); err != nil {
				a.status = warn.Render(err.Error())
				return v, nil
			}
			a.status = ok.Render(F("Opened %s.", link))
			return v, nil
		}
	}
	before := v.input.Value()
	var cmd tea.Cmd
	v.input, cmd = v.input.Update(msg)
	if v.input.Value() != before {
		v.cursor = 0
	}
	return v, cmd
}

func (v *helpView) render(a *app) string {
	var b strings.Builder
	b.WriteString(accent.Render(T("Help")) + "\n\n" + v.input.View() + "\n\n")
	found := v.found(a)
	if len(found) == 0 {
		b.WriteString(muted.Render(T("Nothing about that yet. Try the docs at tracklin.es/docs, or ask us at tracklin.es/contact.")) + "\n")
		return b.String()
	}
	v.cursor = min(v.cursor, len(found)-1)
	// the questions around the one picked, then its answer in full
	rows := max(a.height-14, 5)
	start := min(max(v.cursor-rows/2, 0), max(len(found)-rows, 0))
	for i := start; i < len(found) && i < start+rows; i++ {
		line := "  " + cut(found[i].Question, max(a.width-4, 20))
		if i == v.cursor {
			line = picked.Render("> " + cut(found[i].Question, max(a.width-4, 20)))
		}
		b.WriteString(line + "\n")
	}
	pick := found[v.cursor]
	b.WriteString("\n" + bold.Render(pick.Question) + "\n" + wrap(pick.Answer, a.width) + "\n")
	if pick.Href != "" {
		b.WriteString(muted.Render(F("enter opens %s", pick.Href)) + "\n")
	}
	return b.String()
}

func (v *helpView) help() string {
	return T("type to search · ↑/↓ pick · enter open the page · esc back")
}
func (v *helpView) typing() bool { return true }

// here is the project and board you came from, for an answer's [project] / [board] link.
func (a *app) here() (project, board string) {
	for i := len(a.stack) - 1; i >= 0; i-- {
		switch v := a.stack[i].(type) {
		case *boardView:
			if board == "" {
				board = v.id
			}
			if project == "" && v.board != nil {
				project = v.board.ProjectID
			}
		case *projectView:
			if project == "" {
				project = v.id
			}
		}
	}
	return project, board
}

// siteURL is the website for the API: api.tracklin.es → tracklin.es.
func siteURL(apiBase string) string {
	return strings.Replace(strings.TrimRight(apiBase, "/"), "://api.", "://", 1)
}
