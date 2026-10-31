package ui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tracklines/tui/internal/api"
)

func TestBlockerDetailsAndNavigation(t *testing.T) {
	a, _ := start(t)
	var ticket Ticket
	if err := json.Unmarshal([]byte(`{"id":"blocked","key":"TL-9","blocked":true,"blocked_by":[{"id":"t1","title":"Fix login","project_id":"p1","project_name":"Other project","done":false},{"id":"t2","title":"Completed prerequisite","done":true}]}`), &ticket); err != nil {
		t.Fatal(err)
	}
	v := &ticketView{id: ticket.ID, ticket: &ticket}
	a.stack = []view{v}
	detail := v.detail(a)
	for _, want := range []string{"blocked", "Blocked by", "Other project", "Fix login", "Completed prerequisite", "done", "b to browse blockers"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail lacks %q: %s", want, detail)
		}
	}
	if !strings.Contains(v.help(), "b blockers") {
		t.Fatal("missing shortcut hint")
	}
	_, cmd := v.update(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	push := cmd().(pushMsg)
	picker := push.v.(*blockersView)
	// Render and select with the API-provided order, without changing child/file selection.
	a.stack = append(a.stack, picker)
	if !strings.Contains(picker.render(a), "Other project") {
		t.Fatal("missing cross-project context")
	}
	_, cmd = picker.update(a, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd().(pushMsg).v.(*ticketView).id != "t1" {
		t.Fatal("wrong blocker opened")
	}
	picker.update(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	_, cmd = picker.update(a, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd().(pushMsg).v.(*ticketView).id != "t2" {
		t.Fatal("completed dependency cannot be opened")
	}
	// Actual stack navigation to the first blocker and back to its picker and original ticket.
	picker.cursor = 0
	press(a, "enter")
	if a.top().(*ticketView).id != "t1" {
		t.Fatal("blocker did not open")
	}
	press(a, "esc")
	if a.top() != picker {
		t.Fatal("did not return to blockers")
	}
	press(a, "esc")
	if a.top() != v {
		t.Fatal("did not return to original ticket")
	}
}

func TestBlockerRefreshAndEmptyState(t *testing.T) {
	a := &app{width: 80, height: 24}
	picker := &blockersView{ticket: Ticket{ID: "t", BlockedBy: []api.Dependency{{ID: "a"}, {ID: "b"}}}, cursor: 1}
	picker.update(a, loadedMsg{tag: "blockers:t", data: Ticket{ID: "t"}})
	if picker.cursor != 0 || !strings.Contains(picker.render(a), "No blocking tickets.") {
		t.Fatal("removed blockers not handled")
	}
	if _, cmd := picker.update(a, tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("empty picker opened a ticket")
	}
	picker.update(a, loadedMsg{tag: "blockers:other", data: Ticket{ID: "other"}})
	if picker.ticket.ID != "t" {
		t.Fatal("unrelated update replaced blockers")
	}
	v := &ticketView{ticket: &Ticket{ID: "unblocked"}}
	if strings.Contains(v.help(), "b blockers") {
		t.Fatal("unavailable shortcut shown")
	}
	if _, cmd := v.update(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")}); cmd != nil {
		t.Fatal("unblocked ticket opened picker")
	}
}
