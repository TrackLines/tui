package ui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/tracklines/tui/internal/config"
)

func matchesFilter(t Ticket, f config.TicketFilters, me string) bool {
	eq := func(want, id, name string) bool {
		return want == "" || strings.EqualFold(want, id) || strings.EqualFold(want, name)
	}
	val := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	if !eq(f.Project, t.ProjectID, t.ProjectName) || !eq(f.Board, val(t.BoardID), val(t.BoardName)) {
		return false
	}
	if f.Status == "open" && t.Done || f.Status == "done" && !t.Done {
		return false
	}
	if f.Status != "" && f.Status != "open" && f.Status != "done" && !strings.EqualFold(f.Status, val(t.ColumnName)) {
		return false
	}
	if !eq(f.Type, t.Type, "") || !eq(f.Priority, t.Priority, "") {
		return false
	}
	if f.Assignee != "" {
		assigned := val(t.AssignedTo)
		switch f.Assignee {
		case "me":
			if me == "" || assigned != me {
				return false
			}
		case "unassigned":
			if assigned != "" {
				return false
			}
		default:
			if !strings.EqualFold(f.Assignee, assigned) {
				return false
			}
		}
	}
	for _, label := range f.Labels {
		if !slices.Contains(t.Labels, label) {
			return false
		}
	}
	return true
}

func (v *listView) applyFilters(a *app) {
	v.items, v.mentions = nil, nil
	for i, t := range v.allItems {
		if matchesFilter(t, v.filters, a.me) {
			v.items = append(v.items, t)
			if i < len(v.allMentions) {
				v.mentions = append(v.mentions, v.allMentions[i])
			}
		}
	}
	v.cursor = min(v.cursor, max(0, len(v.items)-1))
	v.hscroll = 0
}

func (v *listView) filterSummary() string {
	f := v.filters
	var parts []string
	for _, p := range [][2]string{{T("Project"), f.Project}, {T("Board"), f.Board}, {T("Status"), f.Status}, {T("Type"), f.Type}, {T("Priority"), f.Priority}, {T("Assignee"), f.Assignee}, {T("Labels"), strings.Join(f.Labels, ",")}} {
		if p[1] != "" {
			parts = append(parts, p[0]+": "+p[1])
		}
	}
	if v.preset != "" {
		parts = append([]string{F("View: %s", v.preset)}, parts...)
	}
	return strings.Join(parts, " · ")
}

func (a *app) filterList() *listView { return filterListFrom(a.top()) }

func filterListFrom(screen view) *listView {
	switch v := screen.(type) {
	case *listView:
		return v
	case *homeView:
		if v.focus >= 0 && v.focus < len(v.sections) {
			return v.sections[v.focus]
		}
	case *boardView:
		if v.board != nil {
			return &listView{tag: "board:" + v.id, heading: v.board.Name, boardID: v.id}
		}
	}
	return nil
}

func (a *app) filterShortcut(key string) (tea.Cmd, bool) {
	if key == "V" {
		if _, open := a.top().(*savedViews); open {
			return nil, true
		}
		return msgOf(pushMsg{&savedViews{}}), true
	}
	if key != "F" && key != "ctrl+x" {
		return nil, false
	}
	list := a.filterList()
	if list == nil {
		return nil, false
	}
	if key == "ctrl+x" {
		list.filters, list.preset, list.labels = config.TicketFilters{}, "", nil
		list.cursor = 0
		list.applyFilters(a)
		a.loading = true
		return list.load(a), true
	}
	if _, board := a.top().(*boardView); board {
		return msgOf(pushMsg{list}), true // next F edits the board's list
	}
	return msgOf(pushMsg{newFilterEditor(list)}), true
}

func filterFieldNames() []string {
	return []string{T("Project (name or ID)"), T("Board (name or ID)"), T("Status (open, done or column)"), T("Type (bug, feature, task, spike, epic)"), T("Priority (urgent, high, medium, low)"), T("Assignee (me, unassigned or ID)"), T("Labels (comma separated; all required)")}
}

type filterEditor struct {
	list   *listView
	inputs []textinput.Model
	at     int
}

func newFilterEditor(list *listView) *filterEditor {
	f := list.filters
	values := []string{f.Project, f.Board, f.Status, f.Type, f.Priority, f.Assignee, strings.Join(f.Labels, ", ")}
	v := &filterEditor{list: list}
	for _, value := range values {
		input := textinput.New()
		input.SetValue(value)
		input.CharLimit = 512
		input.Width = 45
		v.inputs = append(v.inputs, input)
	}
	v.inputs[0].Focus()
	return v
}
func (v *filterEditor) load(a *app) tea.Cmd { a.loading = false; return textinput.Blink }
func (v *filterEditor) typing() bool        { return true }
func (v *filterEditor) help() string {
	return T("tab/shift+tab field · enter apply · ctrl+x clear fields · esc cancel")
}
func (v *filterEditor) render(a *app) string {
	var b strings.Builder
	b.WriteString(accent.Render(T("Ticket filters")) + "\n" + T("Filters narrow the source list’s fetched tickets; they do not search beyond it.") + "\n\n")
	for i, input := range v.inputs {
		b.WriteString(filterFieldNames()[i] + "\n" + input.View() + "\n\n")
	}
	return b.String()
}
func (v *filterEditor) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			return v, msgOf(popMsg{})
		case "tab", "shift+tab":
			v.inputs[v.at].Blur()
			d := 1
			if key.String() == "shift+tab" {
				d = -1
			}
			v.at = (v.at + d + len(v.inputs)) % len(v.inputs)
			return v, v.inputs[v.at].Focus()
		case "ctrl+x":
			for i := range v.inputs {
				v.inputs[i].SetValue("")
			}
			return v, nil
		case "enter":
			values := make([]string, len(v.inputs))
			for i := range v.inputs {
				values[i] = strings.TrimSpace(v.inputs[i].Value())
			}
			f := config.TicketFilters{Project: values[0], Board: values[1], Status: values[2], Type: values[3], Priority: values[4], Assignee: values[5]}
			if f.Type != "" && !slices.Contains([]string{"bug", "feature", "task", "spike", "epic"}, f.Type) {
				a.status = T("Choose a listed ticket type.")
				return v, nil
			}
			if f.Priority != "" && !slices.Contains([]string{"urgent", "high", "medium", "low"}, f.Priority) {
				a.status = T("Choose a listed priority.")
				return v, nil
			}
			for _, label := range strings.Split(values[6], ",") {
				if l := strings.TrimSpace(label); l != "" && !slices.Contains(f.Labels, l) {
					f.Labels = append(f.Labels, l)
				}
			}
			v.list.filters, v.list.preset, v.list.cursor = f, "", 0
			v.list.applyFilters(a)
			return v, msgOf(popMsg{})
		}
	}
	var cmd tea.Cmd
	v.inputs[v.at], cmd = v.inputs[v.at].Update(msg)
	return v, cmd
}

type savedViews struct {
	views  []config.SavedFilter
	cursor int
	source *listView
	input  *textinput.Model
	rename bool
	err    string
}

func (v *savedViews) load(a *app) tea.Cmd {
	a.loading = false
	if len(a.stack) > 1 {
		v.source = filterListFrom(a.stack[len(a.stack)-2])
	}
	var err error
	v.views, err = config.LoadFilters(config.FiltersPath())
	if err != nil {
		v.err = F("Could not read saved views: %s", err.Error())
	}
	return nil
}
func (v *savedViews) typing() bool { return v.input != nil }
func (v *savedViews) help() string {
	if v.input != nil {
		return T("enter save name · esc cancel")
	}
	h := T("j/k select · enter open · r rename · d delete · esc/q back")
	if v.source != nil {
		h += T(" · s save current filters")
	}
	return h
}
func (v *savedViews) render(a *app) string {
	var b strings.Builder
	b.WriteString(accent.Render(T("Saved views")) + "\n\n")
	if v.err != "" {
		b.WriteString(warn.Render(v.err) + "\n")
	}
	if v.input != nil {
		return b.String() + T("Name: ") + v.input.View() + "\n"
	}
	if len(v.views) == 0 {
		b.WriteString(T("No saved views yet. Open a ticket list, press F to filter, then V and s to save.") + "\n")
	}
	var rows []string
	for _, p := range v.views {
		rows = append(rows, p.Name+" · "+p.Heading)
	}
	return b.String() + lines(rows, v.cursor, a.rows()-3)
}
func (v *savedViews) persist(next []config.SavedFilter) bool {
	if err := config.SaveFilters(config.FiltersPath(), next); err != nil {
		v.err = F("Could not save views: %s", err.Error())
		return false
	}
	v.views, v.err = next, ""
	v.cursor = min(v.cursor, max(0, len(next)-1))
	return true
}
func (v *savedViews) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return v, nil
	}
	k := key.String()
	if v.input != nil {
		switch k {
		case "esc":
			v.input = nil
			return v, nil
		case "enter":
			name := strings.TrimSpace(v.input.Value())
			if name == "" {
				v.err = T("Enter a name.")
				return v, nil
			}
			next := slices.Clone(v.views)
			if v.rename {
				next[v.cursor].Name = name
			} else {
				s := v.source
				next = append(next, config.SavedFilter{Name: name, Source: s.tag, Heading: s.heading, Project: s.project, Board: s.boardID, Query: s.query, Sort: s.sort, BacklogLabels: slices.Clone(s.labels), Filters: s.filters})
			}
			if v.persist(next) {
				v.input = nil
			}
			return v, nil
		}
		var cmd tea.Cmd
		*v.input, cmd = v.input.Update(msg)
		return v, cmd
	}
	if cursorKeys(k, &v.cursor, len(v.views)) {
		return v, nil
	}
	switch k {
	case "s", "r":
		if k == "s" && v.source == nil || k == "r" && len(v.views) == 0 {
			return v, nil
		}
		v.rename = k == "r"
		input := textinput.New()
		input.CharLimit = 100
		input.Width = 45
		if v.rename {
			input.SetValue(v.views[v.cursor].Name)
		}
		v.input = &input
		return v, v.input.Focus()
	case "d":
		if len(v.views) > 0 {
			next := slices.Clone(v.views)
			v.persist(append(next[:v.cursor], next[v.cursor+1:]...))
		}
	case "enter":
		if len(v.views) > 0 {
			p := v.views[v.cursor]
			list := &listView{tag: p.Source, heading: p.Heading, project: p.Project, boardID: p.Board, query: p.Query, sort: p.Sort, labels: slices.Clone(p.BacklogLabels), filters: p.Filters, preset: p.Name}
			return v, msgOf(replaceMsg{v: list, status: F("Opened view %s", p.Name)})
		}
	}
	return v, nil
}
