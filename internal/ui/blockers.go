package ui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tracklines/tui/internal/api"
)

func dependencyLabel(d api.Dependency) string {
	title := d.Title
	if d.ProjectName != "" {
		title = d.ProjectName + " — " + title
	}
	status := T("open")
	if d.Done {
		status = T("done")
	}
	return title + " · " + status
}

// A separate picker keeps existing sub-ticket and attachment navigation unchanged.
type blockersView struct {
	ticket Ticket
	cursor int
}

func (v *blockersView) load(a *app) tea.Cmd {
	return fetch("blockers:"+v.ticket.ID, func(ctx context.Context) (any, error) { return a.api.Ticket(ctx, v.ticket.ID) })
}
func (v *blockersView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		if msg.tag == "blockers:"+v.ticket.ID {
			if ticket, ok := msg.data.(Ticket); ok {
				v.ticket = ticket
				v.cursor = min(v.cursor, max(0, len(ticket.BlockedBy)-1))
			}
		}
	case tea.KeyMsg:
		if cursorKeys(msg.String(), &v.cursor, len(v.ticket.BlockedBy)) {
			return v, nil
		}
		switch msg.String() {
		case "enter":
			if len(v.ticket.BlockedBy) > 0 {
				return v, msgOf(pushMsg{&ticketView{id: v.ticket.BlockedBy[v.cursor].ID}})
			}
		case "r":
			a.loading = true
			return v, v.load(a)
		}
	}
	return v, nil
}
func (v *blockersView) render(a *app) string {
	var b strings.Builder
	b.WriteString(accent.Render(T("Blocked by")) + " · " + v.ticket.Key + "\n\n")
	if len(v.ticket.BlockedBy) == 0 {
		return b.String() + muted.Render(T("No blocking tickets.")) + "\n"
	}
	var rows []string
	for _, d := range v.ticket.BlockedBy {
		rows = append(rows, cut(dependencyLabel(d), max(10, a.width-2)))
	}
	b.WriteString(lines(rows, v.cursor, max(1, a.rows()-6)))
	b.WriteString("\n\n" + wrap(dependencyLabel(v.ticket.BlockedBy[v.cursor]), a.width))
	return b.String()
}
func (v *blockersView) help() string {
	return T("j/k select blocker · enter open · r reload · esc/q back")
}
func (v *blockersView) typing() bool { return false }
