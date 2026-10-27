package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/tracklines/tui/internal/api"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tracklines/tui/internal/drafts"
	"github.com/tracklines/tui/internal/i18n"
	"github.com/tracklines/tui/internal/update"
	"github.com/tracklines/tui/internal/usage"
)

// liveRunning: a standup on b1 and a refinement on p1 are running, hosted by Alex (user_2), who has
// liveFocus open. Off unless a test turns it on.
var (
	liveRunning bool
	liveFocus   any
	// apiDown: every change fails (503); replyLost: a comment is saved but its reply never arrives
	// (502); ticketUpdated: TL-1's updated_at (TL-599)
	apiDown, replyLost bool
	historyFails       bool // TL-1's history can't be read (TL-598)
	ticketDue          any  // TL-1's due_date (TL-640); nil is none
	ticketUpdated      = "2026-10-25T10:00:00Z"
	// uploads (TL-660): uploadFails refuses them; uploadHold, when set, keeps one waiting until it's
	// closed or the TUI gives up; removeForbidden: the file isn't yours to remove
	uploadFails, removeForbidden bool
	uploadHold                   chan struct{}
	callsMu                      sync.Mutex // guards fakeAPI's calls while requests are in flight
)

// fakeAPI is TrackLin.es for Sam: project TL with a Web board (To do → In progress → Done, Fibonacci
// estimates), TL-1 on it, TL-2 in the backlog, and a mention.
func fakeAPI(t *testing.T) (*Client, *[]string) {
	var calls []string
	mu := &callsMu
	var posted []map[string]any // comments added (TL-599)
	movedT2 := false
	flaky := 0 // searches for "flaky": the first fails
	ticket := func(id string) map[string]any {
		if id == "t2" {
			return map[string]any{"id": "t2", "key": "TL-2", "title": "Idea", "project_id": "p1"}
		}
		return map[string]any{"id": "t1", "key": "TL-1", "title": "Fix login", "description": "Login fails", "type": "bug", "priority": "urgent", "estimate": "3",
			"project_id": "p1", "board_id": "b1", "board_name": "Web", "column_id": "c1", "column_name": "To do", "updated_at": ticketUpdated, "due_date": ticketDue}
	}
	board := map[string]any{"id": "b1", "name": "Web", "project_id": "p1", "style": "sprints", "estimate_scale": "fibonacci", "columns": []map[string]any{
		{"id": "c1", "name": "To do", "tickets": []any{ticket("t1")}},
		{"id": "c2", "name": "In progress", "tickets": []any{}},
		{"id": "c3", "name": "Done", "tickets": []any{}},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tl_p_test" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/attachments/upload") { // the file itself, not JSON
			n, _ := io.Copy(io.Discard, r.Body)
			mu.Lock()
			calls = append(calls, fmt.Sprintf("POST %s %s %d", r.URL.RequestURI(), r.Header.Get("Content-Type"), n))
			mu.Unlock()
			if uploadHold != nil {
				select {
				case <-uploadHold:
				case <-r.Context().Done():
					return
				}
			}
			if uploadFails {
				http.Error(w, "this ticket already has as many files as it can hold: remove one to add another", http.StatusConflict)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "f2", "name": r.URL.Query().Get("name"), "size": n, "created_by": "user_1"})
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		b, _ := json.Marshal(body)
		mu.Lock() // the TUI asks some things side by side (running sessions)
		calls = append(calls, r.Method+" "+r.URL.RequestURI()+" "+string(b))
		mu.Unlock()
		var out any
		session := map[string]any{"host": "user_2", "people": []string{"user_2", "user_1"}, "focus": map[string]any{"ticketId": liveFocus}}
		switch p := r.URL.Path; {
		case apiDown && r.Method != http.MethodGet:
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		case r.Method == http.MethodDelete && strings.HasPrefix(p, "/api/attachments/"):
			if removeForbidden {
				http.Error(w, "only an organization admin or whoever uploaded it can remove this file", http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/comments"):
			posted = append(posted, map[string]any{"id": fmt.Sprint("p", len(posted)), "body": body["body"], "author": "user_1", "created_at": time.Now().UTC().Format(time.RFC3339)})
			if replyLost {
				http.Error(w, "bad gateway", http.StatusBadGateway)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		case p == "/api/live/sessions":
			out = []any{}
			if liveRunning {
				out = []map[string]any{
					{"kind": "refinement", "id": "p1", "name": "TrackLines", "project_id": "p1", "project_name": "TrackLines", "host": "user_2"},
					{"kind": "standup", "id": "b1", "name": "Web", "project_id": "p1", "project_name": "TrackLines", "host": "user_2"},
				}
			}
		case strings.Contains(p, "/live/"):
			if !liveRunning {
				http.Error(w, "no session running", http.StatusNotFound)
				return
			}
			switch {
			case strings.HasSuffix(p, "/leave"):
				w.WriteHeader(http.StatusNoContent)
				return
			case p == "/api/boards/b1/live/standup":
				out = map[string]any{"people": []map[string]any{{"id": "user_2", "label": "Alex"}, {"id": "user_1", "label": "Sam"}}, "index": 0, "runningSince": 1}
			default: // the session, and joining it
				out = session
			}
		case r.Method == http.MethodPost && p == "/api/tickets/t2/move" && body["estimate"] == "13":
			http.Error(w, "sprint 4 ends today: its scope is locked. Put this in the backlog or the next sprint", http.StatusConflict)
			return
		case r.Method == http.MethodPost && p == "/api/tickets/t2/move":
			mu.Lock()
			movedT2 = true
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		case r.Method == http.MethodPost && p == "/api/tickets/t1/move" && body["column_id"] == "c3":
			http.Error(w, "this api key is read-only", http.StatusForbidden)
			return
		case r.Method == http.MethodPost && (p == "/api/columns/c1/tickets" || p == "/api/projects/p1/backlog"):
			out = map[string]any{"id": "t9", "key": "TL-9", "title": body["title"]}
		case r.Method != http.MethodGet:
			w.WriteHeader(http.StatusNoContent)
			return
		case p == "/api/search" && r.URL.Query().Get("creator") == "me":
			out = []any{map[string]any{"id": "t2", "key": "TL-2", "title": "Idea"}} // Created by me
		case p == "/api/search" && r.URL.Query().Get("q") == "nothing":
			out = []any{}
		case p == "/api/search" && r.URL.Query().Get("q") == "flaky" && flaky == 0:
			flaky++
			http.Error(w, "search is down", http.StatusBadGateway)
			return
		case p == "/api/search":
			out = []any{map[string]any{"id": "t1", "key": "TL-1", "title": "Fix login", "project_name": "TrackLines", "board_name": "Web", "column_name": "To do", "due_date": ticketDue}, map[string]any{"id": "t2", "key": "TL-2", "title": "Idea"}}
		case p == "/api/ticket-keys/TL-1":
			out = ticket("t1")
		case strings.HasPrefix(p, "/api/ticket-keys/"):
			http.Error(w, "not found", http.StatusNotFound)
			return
		case p == "/api/tickets/t1/activity" && historyFails, p == "/api/tickets/t1/history" && historyFails:
			http.Error(w, "history is down", http.StatusBadGateway)
			return
		case p == "/api/tickets/t1/activity": // oldest first (TL-681)
			out = []map[string]any{
				{"id": "e1", "at": "2026-10-20T09:00:00Z", "actor": "user_2", "actor_kind": "person", "action": "ticket.stage", "data": map[string]any{"stage": "backlog", "outcome": "entered"}},
				{"id": "e2", "at": "2026-10-21T09:00:00Z", "actor": "user_1", "actor_kind": "person", "action": "ticket.stage", "data": map[string]any{"stage": "column", "outcome": "entered", "board_name": "Web", "column_name": "To do"}},
				{"id": "e3", "at": "2026-10-21T10:00:00Z", "actor": "user_1", "actor_kind": "person", "action": "ticket.assigned", "data": map[string]any{"from": nil, "to": "user_2"}},
				{"id": "e4", "at": "2026-10-21T11:00:00Z", "actor": "user_1", "actor_kind": "person", "action": "ticket.updated", "data": map[string]any{"fields": map[string]any{"priority": map[string]any{"from": "medium", "to": "urgent"}, "title": true}}},
				{"id": "e5", "at": "2026-10-21T12:00:00Z", "actor": "user_2", "actor_kind": "person", "action": "comment.added", "data": map[string]any{"comment_id": "c1"}},
				{"id": "e6", "at": "2026-10-22T09:00:00Z", "actor": nil, "actor_kind": "system", "action": "ticket.stage", "data": map[string]any{"stage": "resolved", "outcome": "entered", "resolution": "wont_do"}},
			}
		case p == "/api/tickets/t2/activity": // a backend from before activity: the TUI falls back to /history
			http.Error(w, "404 page not found", http.StatusNotFound)
			return
		case p == "/api/tickets/t1/history" && historyFails:
			http.Error(w, "history is down", http.StatusBadGateway)
			return
		case p == "/api/tickets/t1/history": // oldest first, as the API sends it
			out = []map[string]any{
				{"at": "2026-10-20T09:00:00Z", "stage": "backlog", "outcome": "entered", "actor": "user_2"},
				{"at": "2026-10-21T09:00:00Z", "stage": "column", "outcome": "entered", "board_name": "Web", "column_name": "To do", "actor": "user_1"},
				{"at": "2026-10-22T09:00:00Z", "stage": "resolved", "outcome": "entered", "resolution": "wont_do"},
			}
		case p == "/api/tickets/t2/history":
			out = []any{}
		case p == "/api/tickets/t1", p == "/api/tickets/t2":
			out = ticket(strings.TrimPrefix(p, "/api/tickets/"))
		case p == "/api/tickets/t1/attachments":
			out = []map[string]any{{"id": "f1", "name": "shot.png", "url": "https://x.ufs.sh/f/shot", "size": 2048, "content_type": "image/png", "created_by": "user_1"}}
		case strings.HasSuffix(p, "/attachments"):
			out = []any{}
		case strings.HasSuffix(p, "/comments"):
			out = append([]map[string]any{{"id": "m1", "body": "on it", "author": "user_1", "created_at": "2026-10-20T08:00:00Z"}}, posted...)
		case p == "/api/boards/b1":
			out = board
		case p == "/api/help": // newer help answers than the baked ones (TL-657)
			if r.Header.Get("If-None-Match") == `"h2"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"h2"`)
			out = []map[string]any{{"slug": "fresh", "question": "How do I add another board?", "answer": "From the API: Edit project → Boards.",
				"href": "/projects/[project]/edit", "fallback_href": "", "pages": []string{"project"}, "words": "board"}}
		case p == "/api/assignees":
			out = []map[string]any{{"id": "user_1", "label": "You", "name": "Sam Doe"}, {"id": "user_2", "label": "Alex", "name": "Alex Roe"}, {"id": "Claude", "label": "Claude", "kind": "ai"}}
		case p == "/api/projects":
			out = []map[string]any{{"id": "p1", "name": "TrackLines", "key_prefix": "TL", "description": "The tracker"}}
		case p == "/api/projects/p1":
			out = map[string]any{"id": "p1", "name": "TrackLines", "key_prefix": "TL", "boards": []any{board}}
		case p == "/api/projects/p1/backlog/page":
			mu.Lock()
			moved := movedT2
			mu.Unlock()
			tickets := []any{}
			if !moved {
				t2 := ticket("t2")
				t2["labels"] = []string{"ux"}
				tickets = append(tickets, t2)
			}
			out = map[string]any{"tickets": tickets, "labels": []map[string]any{{"label": "api", "count": 2}, {"label": "ux", "count": 1}}}
		case p == "/api/mentions/page":
			out = map[string]any{"mentions": []map[string]any{{"ticket_id": "t1", "key": "TL-1", "title": "Fix login", "project_name": "TrackLines", "actor": "user_1", "mentioned_at": "2026-10-19T10:00:00Z"}}}
		}
		json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return &Client{Base: srv.URL, Key: "tl_p_test", HTTP: srv.Client()}, &calls
}

// pump runs a command and everything it leads to, as Bubble Tea would, skipping timers (cursor blinks).
func pump(a *app, cmd tea.Cmd, depth int) {
	if cmd == nil || depth > 30 {
		return
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-got:
	case <-time.After(100 * time.Millisecond):
		return // a timer
	}
	switch msg := msg.(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			pump(a, c, depth+1)
		}
	default:
		_, next := a.Update(msg)
		pump(a, next, depth+1)
	}
}

func press(a *app, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "right":
			msg = tea.KeyMsg{Type: tea.KeyRight}
		case "ctrl+s":
			msg = tea.KeyMsg{Type: tea.KeyCtrlS}
		case "ctrl+u":
			msg = tea.KeyMsg{Type: tea.KeyCtrlU}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		_, cmd := a.Update(msg)
		pump(a, cmd, 0)
	}
}

func start(t *testing.T) (*app, *[]string) {
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // the most-used projects file, never the real one
	api, calls := fakeAPI(t)
	a := newApp(api)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	pump(a, a.Init(), 0)
	return a, calls
}

func last(calls *[]string) string { return (*calls)[len(*calls)-1] }

// find is the last call starting with prefix, or "".
func find(calls []string, prefix string) string {
	for i := len(calls) - 1; i >= 0; i-- {
		if strings.HasPrefix(calls[i], prefix) {
			return calls[i]
		}
	}
	return ""
}

func expect(t *testing.T, a *app, want ...string) {
	t.Helper()
	v := a.View()
	for _, w := range want {
		if !strings.Contains(v, w) {
			t.Fatalf("screen lacks %q:\n%s", w, v)
		}
	}
}

func TestMyWorkAndMentions(t *testing.T) {
	a, calls := start(t)
	joined := strings.Join(*calls, "\n")
	if !strings.Contains(joined, "/api/search?assignee=me&limit=50&status=open") {
		t.Fatalf("my work isn't my open tickets: %v", *calls)
	}
	if !strings.Contains(joined, "/api/search?creator=me&limit=50&status=open") {
		t.Fatalf("created-by-me isn't fetched: %v", *calls)
	}
	if !strings.Contains(joined, "/api/mentions/page?page=1&per_page=50&hide_done=true") {
		t.Fatalf("mentions: %v", *calls)
	}
	// three stacked sections: My work (assigned), Created by me, Mentions; tab moves between them
	expect(t, a, "1 My work", "2 Projects", "My work", "TL-1", "Web · To do", "TL-2", "Created by me", "Mentions", "Sam Doe · 2026-10-19")
	// My work opens its own cursor
	press(a, "j", "enter") // cursor TL-2
	expect(t, a, "TL-2", "Idea")
	press(a, "esc")
	// tab to Created by me (the one ticket I raised), enter opens it
	press(a, "tab", "enter")
	expect(t, a, "TL-2", "Idea")
	press(a, "esc")
	// tab again to Mentions, enter opens the mentioned ticket's detail (a comment, by name)
	press(a, "tab", "enter")
	expect(t, a, "Fix login", "Login fails", "on it", "Sam Doe")
}

func TestProjectsBoardsAndMovingOnTheBoard(t *testing.T) {
	a, calls := start(t)
	press(a, "2")
	expect(t, a, "Projects", "TrackLines", "The tracker")
	press(a, "enter")
	expect(t, a, "Web", "sprints", "Backlog")
	press(a, "enter")
	expect(t, a, "TrackLines → Web", "To do 1", "In progress 0", "TL-1 Fix login") // which project's board it is
	// L moves the ticket to the next column, at its bottom
	press(a, "L")
	if c := find(*calls, "POST /api/tickets/t1/move"); !strings.HasPrefix(c, "POST /api/tickets/t1/move") || !strings.Contains(c, `"column_id":"c2"`) || !strings.Contains(c, `"position":0`) {
		t.Fatalf("move: %v", *calls)
	}
	expect(t, a, "Moved TL-1 to In progress.")
	// and the backlog, from the project
	press(a, "esc", "j", "enter")
	expect(t, a, "TrackLines → Backlog", "TL-2", "Idea")
}

func TestProjectPageShiftBOpensBacklogFromAnyRow(t *testing.T) {
	a, _ := start(t)
	press(a, "2", "enter") // project page with Web selected
	if !strings.Contains(a.top().help(), "B backlog") {
		t.Fatalf("project page hint omits Shift+B: %s", a.top().help())
	}
	press(a, "B")
	expect(t, a, "TrackLines → Backlog", "TL-2", "Idea")
	press(a, "esc", "j", "B") // return, select the Backlog row, and jump there too
	expect(t, a, "TrackLines → Backlog", "TL-2", "Idea")
}

func TestShiftBDoesNothingOutsideProjectPage(t *testing.T) {
	a, calls := start(t)
	beforeView, beforeCalls := a.View(), len(*calls)
	press(a, "B")
	if a.View() != beforeView || len(*calls) != beforeCalls {
		t.Fatalf("Shift+B changed a non-project screen:\n%s", a.View())
	}
}

func TestCreatingTickets(t *testing.T) {
	a, calls := start(t)
	press(a, "2", "enter", "enter") // the Web board
	press(a, "n")
	expect(t, a, "New ticket", "Estimate", "not estimated")
	press(a, "ctrl+s")
	expect(t, a, "A title, please.")
	press(a, "Add SSO", "ctrl+s")
	expect(t, a, "This board needs an estimate.")
	press(a, "tab", "right", "tab", "right", "right", "tab", "right") // bug, high, estimate 1
	press(a, "ctrl+s")
	c := last(calls)
	for _, want := range []string{"POST /api/columns/c1/tickets", `"title":"Add SSO"`, `"type":"bug"`, `"priority":"urgent"`, `"estimate":"1"`} {
		if !strings.Contains(strings.Join(*calls, "\n"), want) {
			t.Fatalf("create lacks %s: %s\n%v", want, c, *calls)
		}
	}
	expect(t, a, "Created TL-9.")
	// a backlog ticket: no estimate asked for
	press(a, "esc", "j", "enter", "n")
	expect(t, a, "New ticket")
	if strings.Contains(a.View(), "Estimate") {
		t.Fatal("backlog form asks for an estimate")
	}
	press(a, "Spike", "ctrl+s")
	if !strings.Contains(strings.Join(*calls, "\n"), `POST /api/projects/p1/backlog {"description":"","priority":"medium","title":"Spike","type":"task"}`) {
		t.Fatalf("backlog create: %v", *calls)
	}
}

func TestEditingAssigningCommentingMoving(t *testing.T) {
	a, calls := start(t)
	press(a, "enter") // TL-1
	expect(t, a, "TL-1", "Fix login", "estimate 3", "unassigned")
	press(a, "e")
	expect(t, a, "Edit TL-1", "Fix login", "Type  bug", "Estimate  3")
	press(a, " (SSO)", "ctrl+s")
	if c := strings.Join(*calls, "\n"); !strings.Contains(c, `PATCH /api/tickets/t1 {"description":"Login fails","estimate":"3","priority":"urgent","title":"Fix login (SSO)","type":"bug"}`) {
		t.Fatalf("edit: %v", *calls)
	}
	expect(t, a, "Saved.")
	press(a, "a")
	if c := strings.Join(*calls, "\n"); !strings.Contains(c, `PUT /api/tickets/t1/assignee {"assignee":"user_1"}`) {
		t.Fatalf("assign: %v", *calls)
	}
	press(a, "c", "ctrl+s") // empty: not sent
	if strings.Contains(strings.Join(*calls, "\n"), "POST /api/tickets/t1/comments") {
		t.Fatal("empty comment sent")
	}
	press(a, "done, see PR", "ctrl+s")
	if !strings.Contains(strings.Join(*calls, "\n"), `POST /api/tickets/t1/comments {"body":"done, see PR"}`) {
		t.Fatalf("comment: %v", *calls)
	}
	expect(t, a, "Comment added.")
	press(a, "m")
	expect(t, a, "Move TL-1 to", "(now)")
	press(a, "j", "j", "enter") // Done: refused, in the API's words
	expect(t, a, "this api key is read-only")
}

// A backlog ticket moves onto a board: board, column, then an estimate when the board sizes
// tickets (TL-603). A refused move says why, in the API's words, and stays put to try again.
func TestBacklogTicketMovesOntoABoard(t *testing.T) {
	a, calls := start(t)
	press(a, "j", "enter", "m") // TL-2, in the backlog
	expect(t, a, "Move TL-2 to which board", "Web")
	press(a, "enter")
	expect(t, a, "Move TL-2 to Web", "To do", "In progress")
	press(a, "enter")
	expect(t, a, "Estimate TL-2", "fibonacci", "21")
	press(a, "G", "k", "enter") // 13: the sprint's last day
	expect(t, a, "sprint 4 ends today: its scope is locked", "Estimate TL-2")
	press(a, "k", "k", "enter") // 5
	if c := find(*calls, "POST /api/tickets/t2/move"); c != `POST /api/tickets/t2/move {"column_id":"c1","estimate":"5","position":1}` {
		t.Fatalf("move: %q", c)
	}
	expect(t, a, "Moved to To do.")
}

func openBacklog(t *testing.T) (*app, *[]string) {
	t.Helper()
	a, calls := start(t)
	press(a, "2", "enter", "j", "enter") // Projects → TrackLines → Backlog
	expect(t, a, "TrackLines → Backlog", "TL-2", "Idea")
	return a, calls
}

func TestBacklogSelectedTicketMovesWithM(t *testing.T) {
	a, calls := openBacklog(t)
	if !strings.Contains(a.top().help(), "m move to board") {
		t.Fatalf("backlog hint omits m: %s", a.top().help())
	}
	press(a, "m")
	expect(t, a, "Move TL-2 to which board", "Web")
	press(a, "enter", "enter", "enter") // Web → To do → first estimate
	expect(t, a, "Moved to To do.", "Nothing here.")
	if !strings.Contains(strings.Join(*calls, "\n"), `POST /api/tickets/t2/move {"column_id":"c1","estimate":"1","position":1}`) {
		t.Fatalf("move wasn't sent from the selected backlog row: %v", *calls)
	}
}

func TestBacklogMoveCancelAndFailureKeepTicket(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		a, _ := openBacklog(t)
		press(a, "m", "esc")
		expect(t, a, "TrackLines → Backlog", "TL-2", "Idea")
	})
	t.Run("failure", func(t *testing.T) {
		a, _ := openBacklog(t)
		press(a, "m", "enter", "enter", "G", "k", "enter") // choose Web, then locked estimate 13
		expect(t, a, "sprint 4 ends today: its scope is locked", "Estimate TL-2")
		press(a, "esc")
		expect(t, a, "TrackLines → Backlog", "TL-2", "Idea")
	})
}

func TestMoveKeyDoesNothingOutsideBacklog(t *testing.T) {
	a, calls := start(t)
	before := len(*calls)
	press(a, "m")
	if len(*calls) != before || strings.Contains(a.View(), "Move TL-") {
		t.Fatalf("m triggered outside backlog: calls=%v\n%s", (*calls)[before:], a.View())
	}
}

func TestSearchFromATicket(t *testing.T) {
	a, calls := start(t)
	press(a, "j", "enter", "/", "login", "enter")
	if !strings.Contains(last(calls), "q=login") {
		t.Fatalf("search: %s", last(calls))
	}
	expect(t, a, "Search: login", "TL-1")
	press(a, "q") // typing ended: q quits again, not a letter
}

func TestBacklogTicketsResolveInPlace(t *testing.T) {
	a, calls := start(t)
	press(a, "j", "enter") // TL-2, in the backlog
	if !strings.Contains(a.View(), "x close/won") {
		t.Fatalf("backlog ticket offers no resolve:\n%s", a.View())
	}
	press(a, "x", "j", "enter") // open the picker, choose Won't do, resolve
	if c := strings.Join(*calls, "\n"); !strings.Contains(c, `POST /api/tickets/t2/resolve {"reason":"","resolution":"wont_do"}`) {
		t.Fatalf("resolve not sent:\n%s", c)
	}
	expect(t, a, "Closed as won") // back on the ticket with the result

	// a board ticket has a Done column instead, so x does nothing there
	a2, calls2 := start(t)
	press(a2, "enter") // TL-1, on the Web board
	if strings.Contains(a2.View(), "x close/won") {
		t.Fatalf("board ticket offers resolve:\n%s", a2.View())
	}
	n := len(*calls2)
	press(a2, "x")
	if len(*calls2) != n {
		t.Fatalf("board ticket resolved: %v", (*calls2)[n:])
	}
}

func TestHomeHidesEmptySections(t *testing.T) {
	a := newApp(&Client{})
	a.width, a.height = 100, 40
	h := newHome()
	h.sections[0].loaded, h.sections[0].items = true, []Ticket{{ID: "t1", Key: "TL-1", Title: "Fix"}}
	h.sections[1].loaded = true // Created by me: none yet
	h.sections[2].loaded = true // Mentions: none
	view := h.render(a)
	if !strings.Contains(view, "My work") {
		t.Fatalf("my work missing:\n%s", view)
	}
	if strings.Contains(view, "Created by me") || strings.Contains(view, "Mentions") {
		t.Fatalf("empty sections shown:\n%s", view)
	}
	if got := h.visible(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("visible: %v", got)
	}
	// raise a ticket: the Created by me section and its tab appear
	h.sections[1].items = []Ticket{{ID: "t9", Key: "TL-9", Title: "Idea"}}
	if !strings.Contains(h.render(a), "Created by me") {
		t.Fatal("created section didn't appear")
	}
	if !strings.Contains(h.help(), "created by me") {
		t.Fatalf("help: %s", h.help())
	}
}

func TestBadKey(t *testing.T) {
	api, _ := fakeAPI(t)
	api.Key = "tl_p_wrong"
	a := newApp(api)
	pump(a, a.Init(), 0)
	expect(t, a, "API key was refused")
}

func TestHelpers(t *testing.T) {
	if o := offset(100, 50, 10); o != 41 {
		t.Fatalf("offset %d", o)
	}
	if o := offset(5, 4, 10); o != 0 {
		t.Fatalf("short list offset %d", o)
	}
	if c := 7; !cursorKeys("g", &c, 10) || c != 0 { // vim: g to the top, G to the bottom
		t.Fatalf("g: %d", c)
	}
	if c := 0; !cursorKeys("G", &c, 10) || c != 9 {
		t.Fatalf("G: %d", c)
	}
	if c := cut("abcdefghij", 5); c != "abcd…" {
		t.Fatalf("cut %q", c)
	}
}

func TestSpikeTimebox(t *testing.T) {
	// spike is a selectable type in the create/edit form (TL-502)
	found := false
	for _, ty := range types {
		if ty == "spike" {
			found = true
		}
	}
	if !found {
		t.Fatalf("types should offer spike: %v", types)
	}
	d, h := 1, 4
	if got := timeboxLabel(&d, &h); got != "1d 4h" {
		t.Fatalf("timeboxLabel(1,4) = %q", got)
	}
	zero := 0
	if got := timeboxLabel(&d, &zero); got != "1d" {
		t.Fatalf("timeboxLabel(1,0) = %q", got)
	}
	if got := timeboxLabel(nil, nil); got != "" {
		t.Fatalf("timeboxLabel(nil,nil) = %q, want empty", got)
	}
}

func TestTimeboxBusinessHours(t *testing.T) {
	// 2026-01-05 is a Monday, -09 a Friday, -12 the next Monday (local, tz-independent).
	at := func(day, hour int) time.Time { return time.Date(2026, 1, day, hour, 0, 0, 0, time.Local) }

	if m := businessMinutesBetween(at(5, 10), at(5, 15), api.DefaultWeek()); m != 300 {
		t.Fatalf("Mon 10–15 = %d, want 300", m)
	}
	if m := businessMinutesBetween(at(5, 8), at(5, 18), api.DefaultWeek()); m != 480 {
		t.Fatalf("clamp to 09–17 = %d, want 480", m)
	}
	if m := businessMinutesBetween(at(9, 16), at(12, 10), api.DefaultWeek()); m != 120 {
		t.Fatalf("Fri 16→Mon 10 (weekend skipped) = %d, want 120", m)
	}
	if m := businessMinutesBetween(at(10, 9), at(11, 17), api.DefaultWeek()); m != 0 {
		t.Fatalf("all weekend = %d, want 0", m)
	}
	for in, want := range map[int]string{420: "7h", 90: "1h 30m", 480: "1d", 540: "1d 1h", 45: "45m"} {
		if got := formatDuration(in, api.DefaultWeek()); got != want {
			t.Fatalf("formatDuration(%d) = %q, want %q", in, got, want)
		}
	}

	ip := func(n int) *int { return &n }
	sp := func(tm time.Time) *string { s := tm.Format(time.RFC3339); return &s }
	spike := func(started *string, done *string) *Ticket {
		return &Ticket{Type: "spike", TimeboxDays: ip(1), TimeboxHours: ip(0), StartedAt: started, DoneAt: done, Done: done != nil}
	}
	if got := timeboxMeta(spike(nil, nil), at(5, 10), api.DefaultWeek()); got != "timebox 1d" {
		t.Fatalf("not started = %q", got)
	}
	if got := timeboxMeta(spike(sp(at(5, 9)), nil), at(5, 10), api.DefaultWeek()); got != "timebox 1d · 7h left" {
		t.Fatalf("in progress within = %q", got)
	}
	over := &Ticket{Type: "spike", TimeboxDays: ip(0), TimeboxHours: ip(1), StartedAt: sp(at(5, 9))}
	if got := timeboxMeta(over, at(5, 11), api.DefaultWeek()); got != "timebox 1h · over by 1h" {
		t.Fatalf("over = %q", got)
	}
	if got := timeboxMeta(spike(sp(at(5, 9)), sp(at(5, 10))), at(5, 12), api.DefaultWeek()); got != "timebox 1d · within, 7h spare" {
		t.Fatalf("done within = %q", got)
	}
}

func TestBacklogSortAndLabels(t *testing.T) {
	a, calls := start(t)
	press(a, "2", "enter", "j", "enter") // TrackLines → Backlog
	expect(t, a, "TrackLines → Backlog", "sort: manual", "TL-2", "ux")
	if !strings.Contains(last(calls), "/api/projects/p1/backlog/page?per_page=100") {
		t.Fatalf("backlog: %s", last(calls))
	}
	press(a, "s")
	expect(t, a, "sort: newest")
	if !strings.Contains(last(calls), "sort=newest") {
		t.Fatalf("sort: %s", last(calls))
	}
	press(a, "s", "s") // oldest, then back to the backlog's own order (no sort sent)
	if strings.Contains(last(calls), "sort=") {
		t.Fatalf("manual sort sent: %s", last(calls))
	}
	// f: the labels in use, with counts; pick api and ux, apply
	press(a, "f")
	expect(t, a, "Show tickets with any of", "[ ] api 2", "[ ] ux 1")
	press(a, " ", "j", " ", "enter")
	if c := last(calls); !strings.Contains(c, "label=api") || !strings.Contains(c, "label=ux") {
		t.Fatalf("label filter: %s", c)
	}
	expect(t, a, "labels: api, ux")
	// reopened, it shows what's picked; c clears, esc keeps the filter as it was
	press(a, "f")
	expect(t, a, "[x] api 2", "[x] ux 1")
	press(a, "c", "esc")
	expect(t, a, "labels: api, ux")
	press(a, "f", "c", "enter")
	if strings.Contains(last(calls), "label=") {
		t.Fatalf("cleared filter still sent: %s", last(calls))
	}
}

func TestDoneTicketsArentAssigned(t *testing.T) {
	a, calls := start(t)
	a.stack = append(a.stack, &ticketView{id: "t1"})
	pump(a, msgOf(loadedMsg{"ticket:t1", ticketData{t: Ticket{ID: "t1", Key: "TL-1", Title: "Shipped", Done: true}}, nil}), 0)
	expect(t, a, "done")
	if strings.Contains(a.View(), "a assign") {
		t.Fatalf("done ticket offers assigning:\n%s", a.View())
	}
	n := len(*calls)
	press(a, "a")
	if len(*calls) != n {
		t.Fatalf("done ticket assigned: %v", (*calls)[n:])
	}
}

// q backs out of a screen and quits only from the top one; Q quits from anywhere; typing, both are letters.
func TestQuitting(t *testing.T) {
	isQuit := func(cmd tea.Cmd) bool {
		if cmd == nil {
			return false
		}
		_, quit := cmd().(tea.QuitMsg)
		return quit
	}
	a, _ := start(t)
	press(a, "enter") // TL-1
	if _, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}); isQuit(cmd) {
		t.Fatal("q on a ticket quit")
	} else {
		pump(a, cmd, 0)
	}
	if len(a.stack) != 1 {
		t.Fatalf("q didn't go back: %d screens", len(a.stack))
	}
	press(a, "enter")
	if _, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Q")}); !isQuit(cmd) {
		t.Fatal("Q on a ticket didn't quit")
	}
	press(a, "esc")
	if _, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}); !isQuit(cmd) {
		t.Fatal("q at the top didn't quit")
	}
	press(a, "/")
	if _, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Q")}); isQuit(cmd) {
		t.Fatal("Q while typing quit")
	}
}

// t goes to a ticket by key, whatever its case.
func TestGotoAndGlobalSearch(t *testing.T) {
	a, calls := start(t)
	press(a, "t", "tl-1", "enter")
	if !strings.Contains(strings.Join(*calls, "\n"), "GET /api/ticket-keys/TL-1") {
		t.Fatalf("goto: %v", *calls)
	}
	expect(t, a, "TL-1", "Fix login", "Login fails")
	press(a, "esc") // back to My work, not to the goto box
	expect(t, a, "My work")
	press(a, "t", "TL-999", "enter")
	expect(t, a, "There’s no ticket TL-999.", "Go to a ticket")
	press(a, "esc", "t", "308", "enter")
	expect(t, a, "A ticket key, like TL-308.")
}

// / searches every project the key can see (the API scopes it); results say where each ticket is,
// open, and esc comes back to them; / again starts from the same words.
func TestSearchResults(t *testing.T) {
	a, calls := start(t)
	press(a, "/", "tl-1", "enter") // a key: the API puts its ticket first
	if c := find(*calls, "GET /api/search"); !strings.HasPrefix(c, "GET /api/search?limit=50&q=tl-1 ") {
		t.Fatalf("search request: %q", c)
	}
	expect(t, a, "Search: tl-1", "TL-1", "Fix login", "TrackLines · Web · To do", "TL-2", "r retry")
	press(a, "j", "enter")
	expect(t, a, "Idea")
	press(a, "esc")
	expect(t, a, "Search: tl-1", "TL-2")
	press(a, "/")
	expect(t, a, "/ tl-1") // the same words, to change
	press(a, "esc", "q")
	expect(t, a, "My work")
}

func TestSearchEmptyAndFailing(t *testing.T) {
	a, calls := start(t)
	press(a, "/", "nothing", "enter")
	expect(t, a, "No tickets match “nothing”.")
	press(a, "/", "ctrl+u", "flaky", "enter")
	expect(t, a, "The search didn’t work.", "r to try again", "search is down")
	n := len(*calls)
	press(a, "r")
	if !strings.Contains(strings.Join((*calls)[n:], "\n"), "q=flaky") {
		t.Fatalf("retry: %v", (*calls)[n:])
	}
	expect(t, a, "Search: flaky", "TL-1")
	// still loading: says so rather than "no tickets"
	v := &listView{heading: "Search: x", query: "x"}
	a.loading = true
	if !strings.Contains(v.render(a), "Searching…") {
		t.Fatalf("loading: %s", v.render(a))
	}
}

// The comment box and the ticket form work like vim: esc leaves insert mode instead of throwing the
// text away, and q cancels (twice once something's typed).
func TestVimComment(t *testing.T) {
	a, calls := start(t)
	press(a, "enter", "c", "helo", "esc")
	expect(t, a, "Comment on TL-1", "-- NORMAL --") // still here, text kept
	press(a, "h", "i", "l", "esc")                  // fix the typo: in before the second l
	expect(t, a, "-- NORMAL --")
	press(a, "A", " there", "esc", "enter")
	if !strings.Contains(strings.Join(*calls, "\n"), `POST /api/tickets/t1/comments {"body":"hello there"}`) {
		t.Fatalf("vim-edited comment: %v", *calls)
	}
	expect(t, a, "Comment added.")
	// q with text asks again; the second q discards, sending nothing
	n := len(*calls)
	press(a, "c", "draft", "esc", "q")
	expect(t, a, "Unsent text: q again to discard it")
	press(a, "q")
	if strings.Contains(strings.Join((*calls)[n:], "\n"), "POST") {
		t.Fatalf("discarded comment sent: %v", (*calls)[n:])
	}
	expect(t, a, "Fix login", "e edit")
}

func TestVimEditing(t *testing.T) {
	v := newVimArea("")
	type_ := func(s string) {
		for _, r := range s {
			if r == '\n' {
				v.update(tea.KeyMsg{Type: tea.KeyEnter})
			} else {
				v.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			}
		}
	}
	keys := func(ks ...string) {
		for _, k := range ks {
			msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
			if k == "esc" {
				msg = tea.KeyMsg{Type: tea.KeyEsc}
			}
			v.update(msg)
		}
	}
	type_("one\ntwo\nthree")
	keys("esc", "k", "d", "d") // the middle line
	if v.Value() != "one\nthree" {
		t.Fatalf("dd middle: %q", v.Value())
	}
	keys("j", "d", "d") // the last line, leaving no blank line behind
	if v.Value() != "one" {
		t.Fatalf("dd last: %q", v.Value())
	}
	keys("o")
	type_("two")
	keys("esc", "O")
	type_("1.5")
	keys("esc", "0", "x")
	if v.Value() != "one\n.5\ntwo" {
		t.Fatalf("o, O, 0, x: %q", v.Value())
	}
	if action, _ := v.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}); action != vimNone || v.pending != "q" {
		t.Fatal("q with text quit straight away")
	}
}

func TestVimForm(t *testing.T) {
	a, calls := start(t)
	press(a, "2", "enter", "enter", "n", "SSO", "esc")
	expect(t, a, "New ticket", "-- NORMAL --")
	press(a, "q")
	expect(t, a, "Unsaved changes: q again to discard them")
	press(a, "j", "l") // q's warning goes; type: bug
	expect(t, a, "‹ bug ›")
	press(a, "j", "j", "l", "j", "l", "j") // priority, estimate 1, due tomorrow (TL-640), then the description, still in normal mode
	press(a, "i", "Steps", "esc")
	expect(t, a, "-- NORMAL --")
	press(a, "ctrl+s")
	tomorrow := time.Now().AddDate(0, 0, 1).Format(time.DateOnly)
	if c := strings.Join(*calls, "\n"); !strings.Contains(c, `"description":"Steps"`) || !strings.Contains(c, `"title":"SSO"`) || !strings.Contains(c, `"type":"bug"`) || !strings.Contains(c, `"estimate":"1"`) || !strings.Contains(c, `"due_date":"`+tomorrow+`"`) {
		t.Fatalf("vim form: %v", *calls)
	}
	// an untouched form closes on one q
	press(a, "n", "esc", "q")
	expect(t, a, "TrackLines → Web")
}

// The TUI follows a standup or refinement hosted on the web, and only follows (TL-465): it joins,
// moves with the host's ticket, leaves when you go, and never calls a host's endpoint.
func TestFollowingSessions(t *testing.T) {
	liveRunning, liveFocus = true, nil
	t.Cleanup(func() { liveRunning, liveFocus = false, nil })
	a, calls := start(t)
	press(a, "2", "enter")
	expect(t, a, "Refinement running, hosted by Alex Roe", "f to follow it")
	press(a, "enter") // the Web board
	expect(t, a, "Standup running, hosted by Alex Roe")
	press(a, "f")
	expect(t, a, "Following the standup on Web", "hosted by Alex Roe", "Here: Alex Roe, Sam Doe", "Now: Alex", "next: Sam", "The host hasn’t opened a ticket.")
	if find(*calls, "POST /api/boards/b1/live/standup/join") == "" {
		t.Fatalf("didn't join: %v", *calls)
	}
	// the host opens TL-1: the next check shows it
	liveFocus = "t1"
	f := a.top().(*followView)
	pump(a, f.check(a, false), 0)
	expect(t, a, "TL-1", "Fix login", "Login fails")
	// leaving tells the server
	press(a, "esc")
	if find(*calls, "POST /api/boards/b1/live/standup/leave") == "" {
		t.Fatalf("didn't leave: %v", *calls)
	}
	expect(t, a, "TrackLines → Web")
	// it ends while you follow: back to the board, saying so
	press(a, "f")
	liveRunning = false
	pump(a, a.top().(*followView).check(a, false), 0)
	expect(t, a, "The standup has ended.", "TrackLines → Web")
	// refinement, from the project
	liveRunning = true
	press(a, "esc", "f")
	expect(t, a, "Following the refinement on TrackLines")
	if find(*calls, "POST /api/projects/p1/live/refinement/join") == "" {
		t.Fatalf("refinement join: %v", *calls)
	}
	// switching tab leaves too
	press(a, "1")
	if find(*calls, "POST /api/projects/p1/live/refinement/leave") == "" {
		t.Fatalf("tab switch didn't leave: %v", *calls)
	}
	// never a host's action
	for _, c := range *calls {
		for _, host := range []string{"/focus", "/host", "/live/standup/action", "POST /api/boards/b1/live/standup ", "DELETE", "POST /api/projects/p1/live/refinement "} {
			if strings.Contains(c, host) {
				t.Fatalf("the TUI acted as host: %s", c)
			}
		}
	}
}

// My work shows what's running in the organization (one request, TL-466), and enter follows it; coming
// back doesn't look again straight away, r does.
func TestMyWorkShowsRunningSessions(t *testing.T) {
	liveRunning, liveFocus = true, nil
	t.Cleanup(func() { liveRunning, liveFocus = false, nil })
	a, calls := start(t)
	expect(t, a, "Running now", "Refinement on TrackLines", "Standup on TrackLines → Web", "hosted by Alex Roe", "tab running now/my work/created by me/mentions")
	count := func() (n int) {
		for _, c := range *calls {
			if strings.HasPrefix(c, "GET /api/live/sessions ") {
				n++
			}
		}
		return n
	}
	before := count()
	press(a, "shift+tab", "j", "enter") // back up to Running now; j to the standup, follow it
	expect(t, a, "Following the standup on Web")
	press(a, "esc")
	expect(t, a, "Running now")
	if count() != before {
		t.Fatal("looked for sessions again on coming back")
	}
	press(a, "r")
	if count() != before+1 {
		t.Fatal("r didn't look for sessions again")
	}
	// nothing running: no section, and tab is just the ticket sections
	liveRunning = false
	press(a, "r")
	if strings.Contains(a.View(), "Running now") {
		t.Fatalf("ended sessions still shown:\n%s", a.View())
	}
	expect(t, a, "tab my work/created by me/mentions ·")
}

// A project's boards line up: the sprints/kanban column starts in the same place whatever the name.
func TestProjectBoardsLineUp(t *testing.T) {
	a := newApp(&Client{})
	a.width, a.height = 100, 30
	v := &projectView{id: "p1", project: &Project{Name: "P", Boards: []Board{{Name: "Web", Style: "sprints"}, {Name: "LandingPage", Style: "kanban"}, {Name: "Über", Style: "sprints"}}}}
	var at []int
	for _, line := range strings.Split(v.render(a), "\n") {
		for _, label := range []string{"sprints", "kanban"} {
			if i := strings.Index(line, label); i >= 0 {
				at = append(at, lipgloss.Width(line[:i]))
			}
		}
	}
	if len(at) != 3 || at[0] != at[1] || at[1] != at[2] {
		t.Fatalf("labels start at %v:\n%s", at, v.render(a))
	}
}

// A list's labels (backlog) or location (My work) line up after the titles, whatever their length.
func TestListColumnsLineUp(t *testing.T) {
	a := newApp(&Client{})
	a.width, a.height = 100, 30
	v := &listView{tag: "backlog:p1", heading: "Backlog", project: "p1", items: []Ticket{
		{Key: "TL-1", Title: "Short", Labels: []string{"ux"}},
		{Key: "TL-22", Title: "A much longer ticket title here", Labels: []string{"api", "backend"}},
		{Key: "TL-333", Title: "Ünïcode title", Labels: []string{"ops"}},
	}}
	var at []int
	for _, line := range strings.Split(v.render(a), "\n") {
		for _, label := range []string{"ux", "api, backend", "ops"} {
			if i := strings.Index(line, label); i >= 0 {
				at = append(at, lipgloss.Width(line[:i]))
			}
		}
	}
	if len(at) != 3 || at[0] != at[1] || at[1] != at[2] {
		t.Fatalf("labels start at %v:\n%s", at, v.render(a))
	}
	// a title too long for the screen is cut, and its row still fits
	v.items[1].Title = strings.Repeat("long ", 40)
	for _, line := range strings.Split(v.render(a), "\n") {
		if w := lipgloss.Width(line); w > a.width {
			t.Fatalf("row %d wide on a %d screen: %q", w, a.width, line)
		}
	}
}

// vim's : commands save and leave (TL-475): :w / :wq send, :q won't throw text away, :q! will.
func TestVimCommands(t *testing.T) {
	a, calls := start(t)
	press(a, "enter", "c", "ship it", "esc", ":", "q", "enter")
	expect(t, a, "E37: No write since last change", "Comment on TL-1") // refused: still here, text kept
	press(a, ":", "wq", "enter")
	if find(*calls, "POST /api/tickets/t1/comments") != `POST /api/tickets/t1/comments {"body":"ship it"}` {
		t.Fatalf(":wq didn't send: %v", *calls)
	}
	n := len(*calls)
	press(a, "c", "draft", "esc", ":", "q!", "enter") // discards
	expect(t, a, "Fix login", "e edit")
	if strings.Contains(strings.Join((*calls)[n:], "\n"), "POST") {
		t.Fatal(":q! sent the comment")
	}
	press(a, "c", "x", "esc", ":", "nope", "enter")
	expect(t, a, "E492: Not an editor command: nope")
	press(a, ":", "q!", "enter")

	// the ticket form: :w saves; :q in the description weighs the title too
	press(a, "esc", "2", "enter", "enter", "n", "SSO", "tab", "tab", "tab", "right", "tab") // title, then the estimate, then the description
	press(a, "esc", ":", "q", "enter")
	expect(t, a, "E37: No write since last change") // the title is typed, though the description is empty
	press(a, ":", "w", "enter")
	if !strings.Contains(strings.Join(*calls, "\n"), `"title":"SSO"`) {
		t.Fatalf(":w didn't save the ticket: %v", *calls)
	}
	expect(t, a, "Created TL-9.")
}

// keys = standard: no modes; typing types (q and : too), ctrl+s saves, esc cancels, twice over unsaved text.
func TestStandardKeys(t *testing.T) {
	vimKeys = false
	t.Cleanup(func() { vimKeys = true })
	a, calls := start(t)
	press(a, "enter", "c", "quick :q note", "esc")
	expect(t, a, "Unsaved text: esc again to discard it")
	press(a, "esc")
	expect(t, a, "Fix login", "e edit") // discarded
	press(a, "c", "keep me", "ctrl+s")
	if find(*calls, "POST /api/tickets/t1/comments") != `POST /api/tickets/t1/comments {"body":"keep me"}` {
		t.Fatalf("ctrl+s: %v", *calls)
	}
	press(a, "e")
	expect(t, a, "ctrl+s save · esc cancel")
	if strings.Contains(a.View(), "-- NORMAL --") || strings.Contains(a.View(), "-- INSERT --") {
		t.Fatal("standard keys show vim modes")
	}
	press(a, "esc") // nothing changed: straight out
	expect(t, a, "Fix login", "e edit")
}

// Your most used projects become tabs 3–5 (TL-477), from a count kept on this machine only.
func TestMostUsedProjectTabs(t *testing.T) {
	a, _ := start(t)
	if strings.Contains(a.View(), " 3 ") {
		t.Fatalf("a project tab with no history:\n%s", a.View())
	}
	press(a, "3") // no third tab yet: nothing happens
	expect(t, a, "My work")
	press(a, "2", "enter") // open TrackLines
	expect(t, a, "TrackLines")
	if strings.Contains(a.View(), " 3 TrackLines ") {
		t.Fatal("the tabs reordered while inside the project")
	}
	press(a, "1") // back to My work: now it's a tab
	expect(t, a, " 3 TrackLines ")
	press(a, "3")
	expect(t, a, "Web", "Backlog") // straight into the project
	press(a, "enter", "esc")       // a board and back: the same visit, not another
	if got := usage.Load()["p1"]; got.Count != 2 || got.Name != "TrackLines" {
		t.Fatalf("visits counted: %+v", got) // two visits: from Projects, then from its tab
	}
}

func TestAboutAndUpdateNotice(t *testing.T) {
	a, _ := start(t)
	press(a, "?")
	expect(t, a, "TrackLin.es for the terminal", "Version  dev", "A development build: it doesn’t check for updates.", "t        go to a ticket")
	press(a, "esc")
	// a release build that's behind: the header says so, and ? offers it
	r := update.Release{Tag: "v0.2.0"}
	a.latest = &r
	expect(t, a, "v0.2.0 is out · ? to update")
	press(a, "?")
	expect(t, a, "v0.2.0 is out.", "u to update")
}

// ? then h is the help answers (TL-657): the API's newer ones replace the baked ones without a restart,
// typing searches them, and enter opens the page for the project you came from.
func TestHelpAnswers(t *testing.T) {
	var opened []string
	real := openFile
	openFile = func(url string) error { opened = append(opened, url); return nil }
	t.Cleanup(func() { openFile = real })
	a, calls := start(t)
	if find(*calls, "GET /api/help?locale=en") == "" {
		t.Fatalf("help isn't checked on starting: %v", *calls)
	}
	press(a, "2", "enter") // TrackLines: a project to fill [project] in
	press(a, "?", "h")
	expect(t, a, "Help", "How do I add another board?", "From the API: Edit project → Boards.")
	press(a, "b", "o", "a", "r", "d", "enter")
	if len(opened) != 1 || opened[0] != siteURL(a.api.Base)+"/projects/p1/edit" {
		t.Fatalf("opened %v", opened)
	}
	// the next refresh sends what it has: unchanged, so nothing is swapped
	before := len(*calls)
	_, cmd := a.Update(refreshTick{})
	pump(a, cmd, 0)
	if c := find((*calls)[before:], "GET /api/help"); c == "" {
		t.Fatalf("the refresh didn't check help: %v", (*calls)[before:])
	}
	expect(t, a, "From the API")
	press(a, "x", "y", "z", "z", "y")
	expect(t, a, "Nothing about that yet")
}

// backlog_sort in the config: every backlog opens in that order, and s cycles on from there (TL-481).
func TestDefaultBacklogSort(t *testing.T) {
	backlogSort = "newest"
	t.Cleanup(func() { backlogSort = "manual" })
	a, calls := start(t)
	press(a, "2", "enter", "j", "enter") // TrackLines → Backlog
	expect(t, a, "TrackLines → Backlog", "sort: newest")
	if !strings.Contains(last(calls), "sort=newest") {
		t.Fatalf("opened unsorted: %s", last(calls))
	}
	press(a, "s")
	expect(t, a, "sort: oldest")
	press(a, "?")
	expect(t, a, "Backlog  newest")
}

// update_check = off: a release build doesn't look for updates, and ? says why.
func TestUpdateCheckOff(t *testing.T) {
	version, updateCheck = "v1.0.0", false
	t.Cleanup(func() { version, updateCheck = "dev", true })
	a, _ := start(t)
	if cmd := a.checkUpdate(); cmd != nil {
		t.Fatal("checked for updates with update_check = off")
	}
	press(a, "?")
	expect(t, a, "Version  v1.0.0", "Update checks are off (update_check in")
}

// h/l slide the selected row's title along so a long one can be read; the other rows stay put, and a
// new row starts at the beginning (TL-483). l no longer opens: enter does.
func TestScrollingATitle(t *testing.T) {
	a := newApp(&Client{})
	a.width, a.height = 60, 30
	long := "Make the payment retries back off exponentially and give up after a day"
	v := &listView{tag: "backlog:p1", heading: "Backlog", project: "p1", items: []Ticket{
		{ID: "t1", Key: "TL-1", Title: long, Labels: []string{"api"}},
		{ID: "t2", Key: "TL-2", Title: "Another rather long title that also gets cut short", Labels: []string{"ux"}},
	}}
	a.stack = []view{v}
	row := func(key string) string {
		for _, l := range strings.Split(a.View(), "\n") {
			if strings.Contains(l, key) {
				return l
			}
		}
		return ""
	}
	before, other := row("TL-1"), row("TL-2")
	if !strings.Contains(before, "Make the payment") || strings.Contains(before, "give up after a day") {
		t.Fatalf("not cut to start with: %q", before)
	}
	press(a, "l", "l")
	if got := row("TL-1"); !strings.Contains(got, "…") || strings.Contains(got, "Make the") || got == before {
		t.Fatalf("l didn't slide the title: %q", got)
	}
	if row("TL-2") != other {
		t.Fatal("another row moved")
	}
	if a.top() != view(v) {
		t.Fatal("l opened the ticket")
	}
	press(a, "$")
	if got := row("TL-1"); !strings.Contains(got, "give up after a day ") {
		t.Fatalf("$ didn't reach the end: %q", got)
	}
	press(a, "h")
	press(a, "0")
	if row("TL-1") != before {
		t.Fatalf("0 didn't go back to the start: %q", row("TL-1"))
	}
	press(a, "$", "j", "k") // another row and back: from the start again
	if row("TL-1") != before {
		t.Fatalf("the title stayed slid after moving rows: %q", row("TL-1"))
	}
	press(a, "l", "l", "l", "l", "l", "l", "l", "l", "l", "l", "l", "l", "l", "l", "l", "l", "l", "l", "l", "l") // can't slide past its end
	if v.hscroll != v.maxScroll() {
		t.Fatalf("slid past the end: %d of %d", v.hscroll, v.maxScroll())
	}
}

// TL-514: a parent or epic lists its sub-tickets (with the epic's progress), tab picks one and enter
// opens it, esc comes back; a sub-ticket names its parent and p opens it.
func TestSubTicketsAndParent(t *testing.T) {
	a, _ := start(t)
	key, board, me := "TL-2", "Web", "user_1"
	epic := Ticket{ID: "t1", Key: "TL-1", Title: "Fix login", Type: "epic",
		Children: []api.Child{
			{ID: "t2", Key: &key, Title: "Idea", Type: "task", Done: true, AssignedTo: &me, BoardName: &board},
			{ID: "t3", Title: "Later", Type: "bug"},
		},
		Epic: &api.EpicProgress{Done: 1, Total: 2, Points: 3, Unestimated: 1}}
	a.stack = append(a.stack, &ticketView{id: "t1"})
	pump(a, msgOf(loadedMsg{"ticket:t1", ticketData{t: epic}, nil}), 0)
	expect(t, a, "Sub-tickets 1/2 done · 3 pts · 1 unestimated", "✓ TL-2  Idea", "task · Sam Doe · Web", "Later", "bug · unassigned · backlog", "tab pick sub-ticket")

	press(a, "enter") // nothing picked yet: stays put
	expect(t, a, "Sub-tickets")
	press(a, "tab", "enter")
	expect(t, a, "TL-2", "Idea")
	if strings.Contains(a.View(), "Sub-tickets") {
		t.Fatalf("a ticket without sub-tickets shows the section:\n%s", a.View())
	}
	press(a, "esc")
	expect(t, a, "TL-1  Fix login") // back on the parent (reloaded fresh)

	child := Ticket{ID: "t2", Key: "TL-2", Title: "Idea", Parent: &api.Parent{ID: "t1", Title: "Fix login"}}
	a.stack = append(a.stack, &ticketView{id: "t2"})
	pump(a, msgOf(loadedMsg{"ticket:t2", ticketData{t: child}, nil}), 0)
	expect(t, a, "parent: Fix login", "p parent")
	press(a, "p")
	expect(t, a, "TL-1", "Fix login", "Login fails")
}

// TL-522: a ticket lists its files; tab picks one and o (or enter) opens it in the system's viewer.
func TestAttachmentsOpenInTheSystemViewer(t *testing.T) {
	a, _ := start(t)
	var opened []string
	fail := false
	real := openFile
	openFile = func(url string) error {
		if fail {
			return errors.New("no viewer")
		}
		opened = append(opened, url)
		return nil
	}
	t.Cleanup(func() { openFile = real })

	a.stack = append(a.stack, &ticketView{id: "t1"})
	pump(a, a.top().load(a), 0)
	expect(t, a, "Attachments (1)", "shot.png · 2.0 KB · Sam Doe", "tab pick file · o open")
	press(a, "o") // nothing picked: nothing opens
	if len(opened) != 0 {
		t.Fatalf("opened without a pick: %v", opened)
	}
	press(a, "tab", "o")
	if len(opened) != 1 || opened[0] != "https://x.ufs.sh/f/shot" {
		t.Fatalf("opened %v", opened)
	}
	expect(t, a, "Opened shot.png.")
	press(a, "enter")
	if len(opened) != 2 {
		t.Fatalf("enter didn't open the picked file: %v", opened)
	}
	fail = true
	press(a, "o")
	expect(t, a, "Couldn’t open shot.png: no viewer")

	a.stack = append(a.stack, &ticketView{id: "t2"})
	pump(a, a.top().load(a), 0)
	expect(t, a, "TL-2")
	if strings.Contains(a.View(), "Attachments") {
		t.Fatalf("a ticket without files shows the section:\n%s", a.View())
	}
}

func TestFileSize(t *testing.T) {
	for n, want := range map[int64]string{512: "512 B", 1536: "1.5 KB", 2 * 1024 * 1024: "2.0 MB"} {
		if got := fileSize(n); got != want {
			t.Fatalf("fileSize(%d) = %q, want %q", n, got, want)
		}
	}
}

// TL-523: the footer is on the window's last line whatever the screen's height, and the comment box
// fills the screen and follows the window.
func TestFooterPinnedAndCommentBoxFills(t *testing.T) {
	a, _ := start(t) // 120×40
	lastLine := func() (int, string) {
		lines := strings.Split(a.View(), "\n")
		return len(lines), lines[len(lines)-1]
	}
	n, foot := lastLine()
	if n != 40 || !strings.Contains(foot, a.top().help()) {
		t.Fatalf("home: %d lines, last %q", n, foot)
	}
	a.stack = append(a.stack, newComment(Ticket{ID: "t1", Key: "TL-1", Title: "Fix login"}))
	pump(a, a.top().load(a), 0)
	n, foot = lastLine()
	if n != 40 || !strings.Contains(foot, "save") {
		t.Fatalf("comment: %d lines, last %q", n, foot)
	}
	c := a.top().(*commentView)
	if c.input.Height() != a.rows()-3 || c.input.Height() <= 6 {
		t.Fatalf("comment box %d high, want %d", c.input.Height(), a.rows()-3)
	}
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if n, _ = lastLine(); n != 30 || c.input.Height() != a.rows()-3 {
		t.Fatalf("after resize: %d lines, box %d high", n, c.input.Height())
	}
}

// count is how many calls start with prefix.
func count(calls []string, prefix string) int {
	n := 0
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func outage(t *testing.T) {
	t.Cleanup(func() { apiDown, replyLost, ticketUpdated = false, false, "2026-10-25T10:00:00Z" })
}

// A comment that can't be sent is kept, survives a restart, and goes once it's asked to (TL-599).
func TestDraftsSurviveAnOutage(t *testing.T) {
	outage(t)
	a, calls := start(t)
	apiDown = true
	press(a, "enter", "c", "hello", "esc", "enter")
	expect(t, a, "service unavailable", "kept as a draft", "Comment on TL-1") // still writing, text kept
	press(a, "esc", "q", "q")                                                 // leave it (q twice discards the box, not the draft)
	expect(t, a, "Unsent comment", "c to finish it")

	// restart: the draft is still there
	b := newApp(a.api)
	b.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	pump(b, b.Init(), 0)
	expect(t, b, "1 unsent draft", "D to see them")
	press(b, "D")
	expect(t, b, "Unsent drafts", "drafts.json", "TL-1", "hello", "not sent: service unavailable")
	press(b, "r") // still down: still kept, nothing lost
	expect(t, b, "TL-1", "not sent: service unavailable")
	apiDown = false
	n := len(*calls)
	press(b, "r")
	expect(t, b, "Sent to TL-1.", "Nothing waiting")
	if got := count((*calls)[n:], "POST /api/tickets/t1/comments"); got != 1 {
		t.Fatalf("sent %d times: %v", got, (*calls)[n:])
	}
	if len(drafts.Load()) != 0 {
		t.Fatal("sent draft still on disk")
	}
}

// The server saved the comment but the reply was lost: sending again finds it and doesn't post twice.
func TestRetryNeverPostsTwice(t *testing.T) {
	outage(t)
	a, calls := start(t)
	replyLost = true
	press(a, "enter", "c", "on my way", "esc", "enter")
	expect(t, a, "kept as a draft")
	replyLost = false
	press(a, "esc", "q", "q", "D", "r")
	expect(t, a, "Already sent: TL-1 has that comment.")
	if got := count(*calls, "POST /api/tickets/t1/comments"); got != 1 {
		t.Fatalf("posted %d times", got)
	}
}

// An edit that didn't save isn't sent over changes made since; the text is kept to compare.
func TestEditDraftConflict(t *testing.T) {
	outage(t)
	a, calls := start(t)
	apiDown = true
	press(a, "enter", "e", "esc", "A", " now", "esc", "ctrl+s")
	expect(t, a, "kept as a draft")
	apiDown = false
	ticketUpdated = "2026-10-25T11:00:00Z" // someone else changed it
	press(a, "esc", "q", "q")              // leave the form (discarding it there, not the draft)
	expect(t, a, "Unsent edit", "e to finish it")
	n := len(*calls)
	press(a, "D", "r")
	expect(t, a, "conflict: the ticket changed since", "TL-1 changed since your edit")
	if find((*calls)[n:], "PATCH") != "" {
		t.Fatalf("sent over the change: %v", (*calls)[n:])
	}
	press(a, "enter", "e") // the draft, flagged
	expect(t, a, "Fix login now", "has changed since: saving replaces those changes")
	press(a, "ctrl+s")
	if c := find(*calls, "PATCH /api/tickets/t1"); !strings.Contains(c, `"title":"Fix login now"`) {
		t.Fatalf("save after compare: %q", c)
	}
	if _, ok := a.drafts.Get(drafts.Edit, "t1"); ok {
		t.Fatal("saved edit still a draft")
	}
}

// People no longer in the org read "Former member", like the web app, never their raw id (TL-607).
func TestNamesOfPeopleWhoLeft(t *testing.T) {
	a := &app{}
	if got := a.name("user_gone"); got != "A teammate" { // the people list hasn't arrived
		t.Fatalf("loading: %q", got)
	}
	a.people = map[string]string{"user_1": "Sam", "Claude": "Claude"}
	for id, want := range map[string]string{
		"user_1": "Sam", "user_gone": "Former member", "Claude": "Claude",
		"claude-482193": "Claude", "agent-123456": "AI agent", "Bugfixes": "Bugfixes", "TrackLin.es": "TrackLin.es", "legacy": "Imported",
	} {
		if got := a.name(id); got != want {
			t.Fatalf("%s: %q, want %q", id, got, want)
		}
	}
}

// A comment by someone who has left shows them as a former member.
func TestFormerMemberOnATicket(t *testing.T) {
	a, _ := start(t)
	delete(a.people, "user_1")
	press(a, "enter")
	expect(t, a, "Former member")
	if strings.Contains(a.View(), "user_1") {
		t.Fatalf("raw id shown:\n%s", a.View())
	}
}

// language = nl: the screens speak Dutch; tickets, names and keys stay as they are (TL-604).
func TestDutch(t *testing.T) {
	i18n.Set("nl")
	defer i18n.Set("en")
	a, _ := start(t)
	expect(t, a, "Mijn werk", "Projecten", "j/k bewegen", "Fix login") // navigation, help, ticket text as written
	press(a, "enter")
	expect(t, a, "urgent", "schatting 3", "Reacties (1)", "e bewerken")
	press(a, "c")
	expect(t, a, "Reageren op TL-1", "-- INVOEGEN --")
	press(a, "esc", "q", "e") // the form
	expect(t, a, "TL-1 bewerken", "Titel", "Prioriteit", "Schatting", "Beschrijving")
	press(a, "esc", "q", "esc", "t", "TL-999", "enter") // an error
	expect(t, a, "Er is geen ticket TL-999.", "Naar een ticket")
}

// h on a ticket shows its history, newest first, and esc goes back to the ticket (TL-598).
func TestTicketHistory(t *testing.T) {
	t.Cleanup(func() { historyFails = false })
	a, _ := start(t)
	press(a, "enter")
	expect(t, a, "h history")
	press(a, "h")
	expect(t, a, "History of TL-1", "Resolved: won’t do", "by TrackLin.es", "Moved to Web · To do", "by Sam Doe", "Into the backlog", "by Alex Roe")
	// TL-681: every change, not just stages; text says only that it changed
	expect(t, a, "Assigned to Alex Roe", "Changed priority medium → urgent, edited the title", "Commented")
	v := a.View()
	if !(strings.Index(v, "Resolved") < strings.Index(v, "Moved to") && strings.Index(v, "Moved to") < strings.Index(v, "Into the backlog")) {
		t.Fatalf("not newest first:\n%s", v)
	}
	press(a, "esc")
	expect(t, a, "Fix login", "Login fails") // back on the ticket

	historyFails = true
	press(a, "h")
	expect(t, a, "Couldn’t load the history.", "r to try again", "history is down")
	historyFails = false
	press(a, "r")
	expect(t, a, "Moved to Web · To do")

	press(a, "esc", "esc", "j", "enter", "h") // TL-2: nothing has happened to it yet (and its backend has no /activity: /history instead)
	expect(t, a, "History of TL-2", "No history yet.")
}

// Due dates count down by day, like the web app (TL-640).
func TestDueCountdown(t *testing.T) {
	now := time.Date(2026, 10, 25, 23, 30, 0, 0, time.Local)
	for due, want := range map[string]string{
		"2026-10-28": "due in 3 days", "2026-10-26": "due tomorrow", "2026-10-25": "due today",
		"2026-10-24": "1 day overdue", "2026-10-20": "5 days overdue", "2027-01-01": "due in 68 days", "nope": "",
	} {
		if got := dueText(due, now); got != want {
			t.Fatalf("%s: %q, want %q", due, got, want)
		}
	}
	due := "2026-10-20"
	if dueBadge(&Ticket{DueDate: &due, Done: true}, now) != "" || dueBadge(&Ticket{}, now) != "" {
		t.Fatal("no countdown once done, or without a date")
	}
	// the form's field: l/h a day, L/H a week, from no date tomorrow or today, x clears
	for _, c := range []struct{ from, key, want string }{
		{"", "l", "2026-10-26"}, {"", "h", "2026-10-25"}, {"2026-10-31", "l", "2026-11-01"},
		{"2026-10-25", "L", "2026-11-01"}, {"2026-10-25", "H", "2026-10-18"}, {"2026-10-25", "x", ""}, {"2026-10-25", "q", "2026-10-25"},
	} {
		if got := stepDue(c.from, c.key, now); got != c.want {
			t.Fatalf("%q %s: %q, want %q", c.from, c.key, got, c.want)
		}
	}
}

func TestDueDatesOnScreen(t *testing.T) {
	t.Cleanup(func() { ticketDue = nil })
	ticketDue = time.Now().AddDate(0, 0, 3).Format(time.DateOnly)
	a, calls := start(t)
	expect(t, a, "due in 3 days") // My work
	press(a, "enter")
	expect(t, a, "due in 3 days") // the ticket's meta
	press(a, "e")
	expect(t, a, "Due date", ticketDue.(string))
	press(a, "esc", "j", "j", "j", "j", "x", "ctrl+s") // to the due date, clear it
	if c := find(*calls, "PATCH /api/tickets/t1"); !strings.Contains(c, `"due_date":""`) {
		t.Fatalf("clear: %q", c)
	}
}

// A board cuts long titles to its column width; the selected ticket shows in full under it (TL-642).
func TestBoardShowsTheSelectedTitleInFull(t *testing.T) {
	long := "Frontend: saved views and bulk edit in the backlog and search, with partial failures listed"
	v := &boardView{board: &Board{Name: "Frontend", Columns: []Column{
		{Name: "To do", Tickets: []Ticket{{Key: "TL-634", Title: long, Priority: "high"}, {Key: "TL-638", Title: "CSV export"}}},
		{Name: "Done", Tickets: nil},
	}}}
	a := &app{width: 100, height: 40}
	out := v.render(a)
	if !strings.Contains(strings.Join(strings.Fields(out), " "), long) {
		t.Fatalf("the selected title isn't in full:\n%s", out)
	}
	if !strings.Contains(out, "To do · ") || !strings.Contains(out, "high") {
		t.Fatalf("no column or priority under it:\n%s", out)
	}
	v.row = 1 // j: the next ticket
	if sel := v.selected(a); !strings.Contains(sel, "CSV export") || strings.Contains(sel, "bulk edit") {
		t.Fatalf("moving didn't follow:\n%s", sel)
	}
	v.col, v.row = 1, 0
	if v.selected(a) != "" {
		t.Fatal("an empty column shows no ticket")
	}
}

// TL-649: the screen you're on reloads in the background, like the website: every minute, and at
// once when the terminal is focused again; never while away, or on a screen you're typing on.
func TestBackgroundRefresh(t *testing.T) {
	a, calls := start(t)
	const myWork = "GET /api/search?assignee=me"
	tick := func() { _, cmd := a.Update(refreshTick{}); pump(a, cmd, 0) }
	n := count(*calls, myWork)
	tick()
	if got := count(*calls, myWork); got != n+1 {
		t.Fatalf("a refresh tick didn't reload my work: %d → %d", n, got)
	}
	if a.loading {
		t.Fatal("a background refresh shows Loading…")
	}

	a.Update(tea.BlurMsg{})
	tick()
	if got := count(*calls, myWork); got != n+1 {
		t.Fatal("refreshed while the terminal wasn't focused")
	}
	_, cmd := a.Update(tea.FocusMsg{})
	pump(a, cmd, 0)
	if got := count(*calls, myWork); got != n+2 {
		t.Fatal("coming back to the terminal didn't refresh at once")
	}

	// a ticket keeps its place, and a form or comment is never reloaded under you
	press(a, "enter")
	tickets := count(*calls, "GET /api/tickets/t1/comments")
	tick()
	if count(*calls, "GET /api/tickets/t1/comments") != tickets+1 {
		t.Fatal("the ticket screen didn't refresh")
	}
	press(a, "c")
	tickets = count(*calls, "GET /api/tickets/t1")
	tick()
	if count(*calls, "GET /api/tickets/t1") != tickets {
		t.Fatal("refreshed while writing a comment")
	}
}

// TL-681: a line for every action the backend records, in the web History panel's terms; one this
// version doesn't know is "Made a change"
func TestActivityLines(t *testing.T) {
	a, _ := start(t)
	ev := func(action string, data map[string]any) api.ActivityEntry {
		return api.ActivityEntry{Action: action, Data: data}
	}
	actions := map[string]map[string]any{
		"ticket.stage":              {"stage": "column", "outcome": "skipped", "column_name": "Doing"},
		"ticket.assigned":           {"from": "user_1", "to": "user_2"},
		"ticket.updated":            {"fields": map[string]any{"estimate": map[string]any{"from": nil, "to": "3"}, "timebox_days": map[string]any{}, "timebox_hours": map[string]any{}}},
		"ticket.labels_changed":     {"removed": []any{"api"}},
		"ticket.blocked_by_changed": {"added": []any{"x", "y"}},
		"ticket.parent_changed":     {"to": "e"},
		"ticket.sprint_changed":     {"from": "s1", "to": "s2"},
		"ticket.planned_changed":    {"to": "s3"},
		"ticket.reopened":           {}, "ticket.duplicate_of": {"of": "x"}, "ticket.deleted": {}, "ticket.restored": {},
		"ticket.review_requested": {}, "ticket.review_ended": {},
		"comment.added": {"reply_to": "c0"}, "comment.edited": {}, "comment.deleted": {},
		"attachment.added": {"name": "spec.pdf"}, "attachment.removed": {"name": "old.png"}, "attachment.kept": {"name": "log.txt"},
		"pr.linked": {"repo": "tl/web", "number": 42.0}, "pr.state_changed": {"repo": "tl/web", "number": 42.0, "to": "merged"},
		"pr.unlinked": {"repo": "tl/web", "number": 42.0},
	}
	if len(actions) != 23 {
		t.Fatalf("%d actions", len(actions))
	}
	for action, data := range actions {
		if got := activityLine(a, ev(action, data)); got == "" || strings.Contains(got, "Made a change") {
			t.Errorf("%s: %q", action, got)
		}
	}
	for action, want := range map[string]string{
		"ticket.assigned":       "Reassigned from Sam Doe to Alex Roe",
		"ticket.updated":        "Changed estimate – → 3, the timebox",
		"ticket.labels_changed": "Labels removed: api",
		"pr.state_changed":      "Pull request tl/web#42 merged",
		"comment.added":         "Replied to a comment",
	} {
		if got := activityLine(a, ev(action, actions[action])); !strings.Contains(got, want) {
			t.Errorf("%s: %q, want %q", action, got, want)
		}
	}
	if got := activityLine(a, ev("something.new", nil)); got != "Made a change" {
		t.Errorf("unknown: %q", got)
	}
}

// TL-704: a ticket's timebox counts its project's working week; the week is its own, the org's, or
// the default
func TestTimeboxWorkingWeek(t *testing.T) {
	// 2026-01-09 is a Friday, -11 a Sunday (local)
	at := func(day, hour int) time.Time { return time.Date(2026, 1, day, hour, 0, 0, 0, time.Local) }
	gulf := api.WorkWeek{Days: []int{0, 1, 2, 3, 4}, WeekStart: 0, From: 8 * 60, To: 18 * 60}
	if m := businessMinutesBetween(at(9, 9), at(11, 10), gulf); m != 120 { // Friday and Saturday off; Sunday 08–10
		t.Fatalf("Sun–Thu week: %d", m)
	}
	if got := formatDuration(12*60, gulf); got != "1d 2h" { // a 10h working day
		t.Fatalf("10h day: %q", got)
	}
	sp := func(v string) *string { return &v }
	one := 1
	spike := &Ticket{TimeboxDays: &one, StartedAt: sp(at(11, 8).UTC().Format(time.RFC3339))}
	if got := timeboxMeta(spike, at(11, 13), gulf); got != "timebox 1d · 5h left" {
		t.Fatalf("Sunday on a Sun–Thu week: %q", got)
	}

	a := &app{}
	if w := a.week("p1"); fmt.Sprint(w.Days) != "[1 2 3 4 5]" {
		t.Fatalf("not loaded yet: %+v", w)
	}
	a.weeks = &api.WorkWeeks{Org: gulf, Projects: map[string]api.WorkWeek{"p1": api.DefaultWeek()}}
	if w := a.week("p1"); w.From != 9*60 {
		t.Fatalf("project's own: %+v", w)
	}
	if w := a.week("p2"); w.From != 8*60 {
		t.Fatalf("the org's: %+v", w)
	}
}
