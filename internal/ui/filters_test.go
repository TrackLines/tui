package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tracklines/tui/internal/config"
)

func TestCombinedFilters(t *testing.T) {
	board, name, column, assignee := "b1", "Web", "In progress", "user_1"
	ticket := Ticket{ID: "t1", ProjectID: "p1", ProjectName: "TrackLines", BoardID: &board, BoardName: &name, ColumnName: &column, Type: "bug", Priority: "urgent", AssignedTo: &assignee, Labels: []string{"api", "urgent"}}
	f := config.TicketFilters{Project: "TrackLines", Board: "Web", Status: "In progress", Type: "bug", Priority: "urgent", Assignee: "me", Labels: []string{"api", "urgent"}}
	if !matchesFilter(ticket, f, "user_1") {
		t.Fatal("matching combination excluded")
	}
	for _, change := range []func(*config.TicketFilters){
		func(f *config.TicketFilters) { f.Project = "wrong" }, func(f *config.TicketFilters) { f.Board = "wrong" }, func(f *config.TicketFilters) { f.Status = "done" }, func(f *config.TicketFilters) { f.Type = "task" }, func(f *config.TicketFilters) { f.Priority = "low" }, func(f *config.TicketFilters) { f.Assignee = "unassigned" }, func(f *config.TicketFilters) { f.Labels = []string{"api", "missing"} },
	} {
		copy := f
		change(&copy)
		if matchesFilter(ticket, copy, "user_1") {
			t.Fatalf("dimension ignored: %+v", copy)
		}
	}
	if matchesFilter(ticket, config.TicketFilters{Assignee: "me"}, "") {
		t.Fatal("unknown identity matched me")
	}
	ticket.AssignedTo = nil
	if !matchesFilter(ticket, config.TicketFilters{Assignee: "unassigned", Status: "open"}, "") {
		t.Fatal("unassigned open excluded")
	}
}

func TestFilterSelectionAndMentionsStayAligned(t *testing.T) {
	a := &app{me: "user_1"}
	v := &listView{allItems: []Ticket{{ID: "a", Type: "task"}, {ID: "b", Type: "bug"}}, allMentions: []Mention{{TicketID: "a"}, {TicketID: "b"}}, cursor: 1, filters: config.TicketFilters{Type: "bug"}}
	v.applyFilters(a)
	if len(v.items) != 1 || v.items[0].ID != "b" || v.mentions[0].TicketID != "b" || v.cursor != 0 {
		t.Fatalf("misaligned: %+v", v)
	}
	v.filters = config.TicketFilters{}
	v.applyFilters(a)
	if len(v.items) != 2 {
		t.Fatal("clear lost source tickets")
	}
	v.cursor = 1
	v.applyFilters(a)
	if v.cursor != 1 {
		t.Fatal("refresh reset selection")
	}
}

func TestSavedViewsWorkflowAndTyping(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a, _ := start(t)
	press(a, "F")
	editor, ok := a.top().(*filterEditor)
	if !ok {
		t.Fatalf("F opened %T", a.top())
	}
	// Navigation letters remain input in either editing mode.
	previous := vimKeys
	defer func() { vimKeys = previous }()
	for _, mode := range []bool{true, false} {
		vimKeys = mode
		press(a, "qV?F123/")
	}
	if a.top() != editor || !strings.Contains(editor.inputs[0].Value(), "qV?F123/") {
		t.Fatal("shortcut intercepted typing")
	}
	for i := range editor.inputs {
		editor.inputs[i].SetValue("")
	}
	editor.inputs[3].SetValue("bug")
	editor.inputs[4].SetValue("urgent")
	press(a, "enter", "V", "s", "Urgent bugs", "enter")
	picker := a.top().(*savedViews)
	if len(picker.views) != 1 || picker.views[0].Filters.Type != "bug" {
		t.Fatalf("save: %+v", picker.views)
	}
	press(a, "r")
	picker.input.SetValue("Renamed")
	press(a, "enter", "enter")
	list, ok := a.top().(*listView)
	if !ok || list.preset != "Renamed" || list.filters.Priority != "urgent" {
		t.Fatalf("restore: %T", a.top())
	}
	expect(t, a, "Renamed", "urgent")
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	pump(a, cmd, 0)
	if list.filterSummary() != "" || len(list.items) == 0 {
		t.Fatal("clear failed")
	}
	press(a, "V", "d")
	if len(a.top().(*savedViews).views) != 0 {
		t.Fatal("delete failed")
	}
	stored, err := config.LoadFilters(config.FiltersPath())
	if err != nil || len(stored) != 0 {
		t.Fatalf("delete not persisted: %v %v", stored, err)
	}
}

func TestEmptyFilteredHomeSectionStaysReachable(t *testing.T) {
	home := newHome()
	for _, s := range home.sections {
		s.loaded = true
	}
	home.sections[0].filters = config.TicketFilters{Type: "bug"}
	if got := home.visible(); len(got) != 1 || got[0] != 0 {
		t.Fatal("empty filtered section cannot be cleared", got)
	}
}

func TestSavedViewsFailureAndDuplicate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	source := &listView{tag: "mywork", heading: "My work"}
	picker := &savedViews{source: source}
	a := &app{stack: []view{source, picker}}
	press(a, "s", "Example", "enter", "s", "Example", "enter")
	if len(picker.views) != 1 || picker.input == nil || picker.err == "" {
		t.Fatal("duplicate silently overwrote")
	}
	press(a, "esc")
	if cmd, _ := a.filterShortcut("V"); cmd != nil {
		t.Fatal("saved view picker nests itself")
	}
}

func TestSavedBoardAndBacklogContext(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a, _ := start(t)
	press(a, "2", "enter", "enter", "F")
	list, ok := a.top().(*listView)
	if !ok || list.boardID != "b1" || len(list.items) != 1 {
		t.Fatalf("board list: %T", a.top())
	}
	press(a, "F")
	editor := a.top().(*filterEditor)
	editor.inputs[2].SetValue("To do")
	editor.inputs[3].SetValue("bug")
	press(a, "enter", "V", "s", "Board bugs", "enter", "enter")
	list = a.top().(*listView)
	if list.boardID != "b1" || len(list.items) != 1 || list.items[0].ID != "t1" {
		t.Fatal("board source lost")
	}
	press(a, "enter")
	if a.top().(*ticketView).id != "t1" {
		t.Fatal("wrong filtered selection opened")
	}
	// A backlog preset keeps the existing sort and label filters too.
	source := &listView{tag: "backlog:p1", heading: "Backlog", project: "p1", sort: "newest", labels: []string{"ux"}, filters: config.TicketFilters{Project: "p1"}}
	picker := &savedViews{source: source}
	a.stack = []view{source, picker}
	press(a, "s", "UX backlog", "enter", "enter")
	list = a.top().(*listView)
	if list.project != "p1" || list.sort != "newest" || len(list.labels) != 1 || list.labels[0] != "ux" || list.filters.Project != "p1" {
		t.Fatalf("lost context: %+v", list)
	}
}

func TestFilterEditorCancelClearAndValidation(t *testing.T) {
	list := &listView{filters: config.TicketFilters{Type: "bug"}}
	editor := newFilterEditor(list)
	a := &app{stack: []view{list, editor}}
	editor.inputs[3].SetValue("nonsense")
	_, cmd := editor.update(a, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || a.status == "" || list.filters.Type != "bug" {
		t.Fatal("invalid filter applied")
	}
	editor.update(a, tea.KeyMsg{Type: tea.KeyCtrlX})
	for _, input := range editor.inputs {
		if input.Value() != "" {
			t.Fatal("clear left a field")
		}
	}
	if list.filters.Type != "bug" {
		t.Fatal("editing changed applied filters before enter")
	}
	_, cmd = editor.update(a, tea.KeyMsg{Type: tea.KeyEsc})
	if _, ok := cmd().(popMsg); !ok {
		t.Fatal("cancel did not go back")
	}
}

func TestFilterHelpAndFailureAreVisible(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a, _ := start(t)
	press(a, "?")
	expect(t, a, "Previous screen shortcuts", "F filters", "V saved views", "ctrl+x")
	picker := &savedViews{source: &listView{tag: "mywork"}}
	t.Setenv("XDG_CONFIG_HOME", "/dev/null")
	a.stack = []view{picker.source, picker}
	picker.load(a)
	press(a, "s", "Keep this name", "enter")
	if picker.input == nil || picker.input.Value() != "Keep this name" || picker.err == "" {
		t.Fatal("failed save lost pending name")
	}
}
