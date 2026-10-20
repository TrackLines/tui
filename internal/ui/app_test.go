package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tracklines/tui/internal/update"
	"github.com/tracklines/tui/internal/usage"
)

// liveRunning: a standup on b1 and a refinement on p1 are running, hosted by Alex (user_2), who has
// liveFocus open. Off unless a test turns it on.
var (
	liveRunning bool
	liveFocus   any
)

// fakeAPI is TrackLin.es for Sam: project TL with a Web board (To do → In progress → Done, Fibonacci
// estimates), TL-1 on it, TL-2 in the backlog, and a mention.
func fakeAPI(t *testing.T) (*Client, *[]string) {
	var calls []string
	var mu sync.Mutex
	ticket := func(id string) map[string]any {
		if id == "t2" {
			return map[string]any{"id": "t2", "key": "TL-2", "title": "Idea", "project_id": "p1"}
		}
		return map[string]any{"id": "t1", "key": "TL-1", "title": "Fix login", "description": "Login fails", "type": "bug", "priority": "urgent", "estimate": "3",
			"project_id": "p1", "board_id": "b1", "board_name": "Web", "column_id": "c1", "column_name": "To do"}
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
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		b, _ := json.Marshal(body)
		mu.Lock() // the TUI asks some things side by side (running sessions)
		calls = append(calls, r.Method+" "+r.URL.RequestURI()+" "+string(b))
		mu.Unlock()
		var out any
		session := map[string]any{"host": "user_2", "people": []string{"user_2", "user_1"}, "focus": map[string]any{"ticketId": liveFocus}}
		switch p := r.URL.Path; {
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
		case r.Method == http.MethodPost && p == "/api/tickets/t1/move" && body["column_id"] == "c3":
			http.Error(w, "this api key is read-only", http.StatusForbidden)
			return
		case r.Method == http.MethodPost && (p == "/api/columns/c1/tickets" || p == "/api/projects/p1/backlog"):
			out = map[string]any{"id": "t9", "key": "TL-9", "title": body["title"]}
		case r.Method != http.MethodGet:
			w.WriteHeader(http.StatusNoContent)
			return
		case p == "/api/search":
			out = []any{ticket("t1"), map[string]any{"id": "t2", "key": "TL-2", "title": "Idea"}}
		case p == "/api/ticket-keys/TL-1":
			out = ticket("t1")
		case strings.HasPrefix(p, "/api/ticket-keys/"):
			http.Error(w, "not found", http.StatusNotFound)
			return
		case p == "/api/tickets/t1", p == "/api/tickets/t2":
			out = ticket(strings.TrimPrefix(p, "/api/tickets/"))
		case strings.HasSuffix(p, "/comments"):
			out = []map[string]any{{"id": "m1", "body": "on it", "author": "user_1", "created_at": "2026-10-20T08:00:00Z"}}
		case p == "/api/boards/b1":
			out = board
		case p == "/api/assignees":
			out = []map[string]any{{"id": "user_1", "label": "You", "name": "Sam Doe"}, {"id": "user_2", "label": "Alex", "name": "Alex Roe"}, {"id": "Claude", "label": "Claude", "kind": "ai"}}
		case p == "/api/projects":
			out = []map[string]any{{"id": "p1", "name": "TrackLines", "key_prefix": "TL", "description": "The tracker"}}
		case p == "/api/projects/p1":
			out = map[string]any{"id": "p1", "name": "TrackLines", "key_prefix": "TL", "boards": []any{board}}
		case p == "/api/projects/p1/backlog/page":
			t2 := ticket("t2")
			t2["labels"] = []string{"ux"}
			out = map[string]any{"tickets": []any{t2}, "labels": []map[string]any{{"label": "api", "count": 2}, {"label": "ux", "count": 1}}}
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
	if !strings.Contains(strings.Join(*calls, "\n"), "/api/search?assignee=me&limit=50&status=open") {
		t.Fatalf("my work isn't my open tickets: %v", *calls)
	}
	expect(t, a, "1 My work", "2 Projects", "TL-1", "Web · To do", "TL-2", "Backlog")
	// Mentions are on the same screen, below; tab moves between the two
	expect(t, a, "Mentions", "Sam Doe · 2026-10-19")
	if !strings.Contains(strings.Join(*calls, "\n"), "/api/mentions/page?page=1&per_page=50&hide_done=true") {
		t.Fatalf("mentions: %v", *calls)
	}
	press(a, "j") // My work's cursor: TL-2
	press(a, "tab", "enter")
	expect(t, a, "TL-1", "Fix login") // Mentions' first, not My work's TL-2
	press(a, "esc", "tab", "enter")
	expect(t, a, "TL-2", "Idea") // back to My work, its cursor where it was
	press(a, "esc", "tab", "enter")
	expect(t, a, "Fix login", "Login fails", "on it", "Sam Doe") // a comment, by name
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

func TestBacklogTicketsAreMovedInTheWebAppAndSearch(t *testing.T) {
	a, calls := start(t)
	press(a, "j", "enter", "m") // TL-2, in the backlog
	expect(t, a, "put it on a board in the web app")
	press(a, "esc", "/", "login", "enter")
	if !strings.Contains(last(calls), "q=login") {
		t.Fatalf("search: %s", last(calls))
	}
	expect(t, a, "Search: login", "TL-1")
	press(a, "q") // typing ended: q quits again, not a letter
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
	press(a, "j", "j", "l", "j") // priority, estimate 1, then the description, still in normal mode
	press(a, "i", "Steps", "esc")
	expect(t, a, "-- NORMAL --")
	press(a, "ctrl+s")
	if c := strings.Join(*calls, "\n"); !strings.Contains(c, `"description":"Steps"`) || !strings.Contains(c, `"title":"SSO"`) || !strings.Contains(c, `"type":"bug"`) || !strings.Contains(c, `"estimate":"1"`) {
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
	expect(t, a, "Running now", "Refinement on TrackLines", "Standup on TrackLines → Web", "hosted by Alex Roe", "tab running now/my work/mentions")
	count := func() (n int) {
		for _, c := range *calls {
			if strings.HasPrefix(c, "GET /api/live/sessions ") {
				n++
			}
		}
		return n
	}
	before := count()
	press(a, "tab", "tab", "j", "enter") // Mentions, then Running now; the standup
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
	// nothing running: no section, and tab is just My work and Mentions
	liveRunning = false
	press(a, "r")
	if strings.Contains(a.View(), "Running now") {
		t.Fatalf("ended sessions still shown:\n%s", a.View())
	}
	expect(t, a, "tab my work/mentions ·")
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
