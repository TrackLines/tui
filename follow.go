package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// followView follows a standup (a board's) or refinement (a project's) someone is hosting on the
// web (TL-465): who's there, whose turn it is, and the ticket the host has open. The TUI only ever
// watches: it never starts, hosts or ends one. ponytail: it checks every 2s rather than reading the
// live stream; read /live if 2s ever feels slow.
type followView struct {
	kind, id, name string // standup + board, or refinement + project
	session        *Session
	turn           *StandupTurn
	ticket         *Ticket // what the host has open
	ticks          int
}

const followEvery = 2 * time.Second

type (
	followTick struct{ tag string }
	followData struct {
		session Session
		turn    *StandupTurn
		ticket  *Ticket
	}
)

func (v *followView) tag() string { return "follow:" + v.kind + ":" + v.id }

func (v *followView) load(a *app) tea.Cmd { return v.check(a, true) }

// check reads the session (joining, every 30s, to stay in it), whose turn it is, and the host's ticket.
func (v *followView) check(a *app, join bool) tea.Cmd {
	known := v.ticket
	return fetch(v.tag(), func(ctx context.Context) (any, error) {
		var s Session
		var err error
		if join {
			s, err = a.api.Join(ctx, v.kind, v.id)
		} else {
			s, err = a.api.Session(ctx, v.kind, v.id)
		}
		if e, isAPI := err.(*APIError); isAPI && e.Status == http.StatusNotFound {
			return nil, fmt.Errorf("The %s has ended.", v.kind)
		}
		if err != nil {
			return nil, err
		}
		d := followData{session: s, ticket: known}
		if v.kind == "standup" {
			if t, err := a.api.Turn(ctx, v.id); err == nil {
				d.turn = &t
			}
		}
		if id := s.Focus.TicketID; id == nil {
			d.ticket = nil
		} else if known == nil || known.ID != *id {
			t, err := a.api.Ticket(ctx, *id)
			if err != nil {
				return nil, err
			}
			d.ticket = &t
		}
		return d, nil
	})
}

func (v *followView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		if msg.tag != v.tag() {
			return v, nil
		}
		if d, isData := msg.data.(followData); isData {
			v.session, v.turn, v.ticket = &d.session, d.turn, d.ticket
		}
		tag := v.tag()
		return v, tea.Tick(followEvery, func(time.Time) tea.Msg { return followTick{tag} })
	case followTick:
		if msg.tag != v.tag() {
			return v, nil
		}
		v.ticks++
		return v, v.check(a, v.ticks%15 == 0) // join again every 30s
	}
	return v, nil
}

// close leaves the session, whichever way the screen is left.
func (v *followView) close(a *app) tea.Cmd {
	kind, id := v.kind, v.id
	return func() tea.Msg { _ = a.api.Leave(context.Background(), kind, id); return nil }
}

func (v *followView) render(a *app) string {
	head := accent.Render(fmt.Sprintf("Following the %s on %s", v.kind, v.name))
	if v.session == nil {
		return head + "\n"
	}
	var b strings.Builder
	b.WriteString(head + muted.Render("  hosted by "+a.name(v.session.Host)) + "\n")
	var here []string
	for _, p := range v.session.People {
		here = append(here, a.name(p))
	}
	b.WriteString(muted.Render("Here: "+strings.Join(here, ", ")) + "\n")
	if t := v.turn; t != nil && t.Index >= 0 && t.Index < len(t.People) {
		turn := "Now: " + bold.Render(t.People[t.Index].Label)
		if t.RunningSince == nil {
			turn += muted.Render(" (paused)")
		}
		if t.Index+1 < len(t.People) {
			turn += muted.Render(" · next: " + t.People[t.Index+1].Label)
		}
		b.WriteString(turn + "\n")
	}
	b.WriteString("\n")
	if v.ticket == nil {
		b.WriteString(muted.Render("The host hasn’t opened a ticket.") + "\n")
	} else {
		b.WriteString((&ticketView{ticket: v.ticket}).detail(a))
	}
	return b.String()
}

func (v *followView) help() string {
	return "following: it moves with the host · esc/q leave · Q quit"
}
func (v *followView) typing() bool { return false }

// running asks whether a session is on, for the board's and project's banner: "f to follow it".
func running(a *app, kind, id string) tea.Cmd {
	return fetch("running:"+kind+":"+id, func(ctx context.Context) (any, error) {
		s, err := a.api.Session(ctx, kind, id)
		if err != nil {
			return (*Session)(nil), nil // none running, or live sessions are off: no banner
		}
		return &s, nil
	})
}

// sessionBanner is the line a board or project shows while its standup or refinement is running.
func sessionBanner(a *app, kind string, s *Session) string {
	if s == nil {
		return ""
	}
	return ok.Render(fmt.Sprintf("● %s running, hosted by %s", strings.ToUpper(kind[:1])+kind[1:], a.name(s.Host))) + muted.Render(" · f to follow it") + "\n"
}
