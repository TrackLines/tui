package ui

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tracklines/tui/internal/api"
)

// listView is a list of tickets: My work, Mentions, a search, or a project's backlog (which can take
// new tickets: project is set).
type listView struct {
	tag, heading string
	query        string // search
	project      string // backlog
	items        []Ticket
	mentions     []Mention // Mentions: who and when, per item
	loaded       bool      // the first load has arrived (so empty is "nothing" not "not yet")
	cursor       int
	height       int      // rows to draw in; 0: the whole screen
	hscroll      int      // how far the selected row's title is slid along (h/l), in characters
	titleW       int      // the titles' width when last drawn, which caps hscroll
	inactive     bool     // a section that isn't focused: no cursor shown
	sort         string   // backlog: manual (its own order), newest or oldest
	labels       []string // backlog: only tickets with any of these
	inUse        []Label  // backlog: the labels its tickets have, for the filter
}

var sorts = []string{"manual", "newest", "oldest"}

func (v *listView) load(a *app) tea.Cmd {
	return fetch(v.tag, func(ctx context.Context) (any, error) {
		switch {
		case v.tag == "mentions":
			return a.api.Mentions(ctx)
		case v.tag == "created":
			return a.api.MyCreated(ctx)
		case v.project != "":
			return a.api.Backlog(ctx, v.project, v.sort, v.labels)
		case v.query != "":
			return a.api.Search(ctx, v.query)
		}
		return a.api.MyWork(ctx)
	})
}

func (v *listView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		if msg.tag != v.tag {
			return v, nil
		}
		v.items, v.mentions, v.loaded = nil, nil, true
		switch d := msg.data.(type) {
		case []Ticket:
			v.items = d
		case BacklogPage:
			v.items, v.inUse = d.Tickets, d.Labels
		case []Mention:
			v.mentions = d
			for _, m := range d {
				v.items = append(v.items, Ticket{ID: m.TicketID, Key: m.Key, Title: m.Title, ProjectName: m.ProjectName})
			}
		}
		v.cursor = min(v.cursor, max(len(v.items)-1, 0))
		v.hscroll = 0
	case tea.KeyMsg:
		k := msg.String()
		if cursorKeys(k, &v.cursor, len(v.items)) {
			v.hscroll = 0 // a new row starts at the beginning of its title
			return v, nil
		}
		switch k {
		case "h", "left": // slide the selected title along (TL-483); enter opens
			v.hscroll = max(0, v.hscroll-scrollStep)
		case "l", "right":
			v.hscroll = min(v.maxScroll(), v.hscroll+scrollStep)
		case "0":
			v.hscroll = 0
		case "$":
			v.hscroll = v.maxScroll()
		case "enter":
			if len(v.items) > 0 {
				return v, msgOf(pushMsg{&ticketView{id: v.items[v.cursor].ID}})
			}
		case "n":
			if v.project != "" {
				return v, msgOf(pushMsg{newForm(nil, v.project, "", "none")})
			}
		case "s":
			if v.project != "" {
				v.sort = sorts[(max(indexOf(sorts, v.sort), 0)+1)%len(sorts)] // unset is manual
				v.cursor, a.loading = 0, true
				return v, v.load(a)
			}
		case "f":
			if v.project != "" {
				return v, msgOf(pushMsg{&labelsView{list: v}})
			}
		case "r":
			a.loading = true
			return v, v.load(a)
		}
	}
	return v, nil
}

func (v *listView) render(a *app) string {
	var b strings.Builder
	b.WriteString(accent.Render(v.heading) + muted.Render(fmt.Sprintf("  %d", len(v.items))))
	if v.project != "" {
		sort := v.sort
		if sort == "" {
			sort = "manual"
		}
		b.WriteString(muted.Render("  · sort: " + sort))
		if len(v.labels) > 0 {
			b.WriteString(muted.Render("  · labels: ") + strings.Join(v.labels, ", "))
		}
	}
	b.WriteString("\n\n")
	if v.query != "" && !v.loaded { // the search failed: loadedMsg's error is in the status line
		if a.loading {
			return b.String() + muted.Render("Searching…") + "\n"
		}
		return b.String() + warn.Render("The search didn’t work.") + muted.Render(" r to try again") + "\n"
	}
	if len(v.items) == 0 {
		if v.query != "" {
			return b.String() + muted.Render("No tickets match “"+v.query+"”.") + "\n"
		}
		return b.String() + muted.Render("Nothing here.") + "\n"
	}
	extras := make([]string, len(v.items))
	widest, longest := 0, 0
	for i, t := range v.items {
		extras[i] = where(t)
		if v.query != "" && t.ProjectName != "" { // a search spans projects
			extras[i] = t.ProjectName + " · " + extras[i]
		}
		if v.project != "" {
			extras[i] = strings.Join(t.Labels, ", ")
		}
		if v.mentions != nil { // named as it's drawn: the names may arrive after the list
			m := v.mentions[i]
			extras[i] = a.name(m.Actor) + " · " + day(m.MentionedAt) + " · " + t.ProjectName
		}
		widest, longest = max(widest, lipgloss.Width(extras[i])), max(longest, lipgloss.Width(t.Title))
	}
	// one width for every title (the longest, or what's left beside the widest extra), so the labels
	// or location after them line up in a column
	titleW := min(longest, max(20, a.width-12-widest-2))
	v.titleW = titleW
	rows := make([]string, len(v.items))
	for i, t := range v.items {
		title := cut(t.Title, titleW)
		if i == v.cursor && !v.inactive {
			title = slide(t.Title, v.hscroll, titleW)
		}
		row := fmt.Sprintf("%-9s %s%s  %s", t.Key, title, strings.Repeat(" ", max(0, titleW-lipgloss.Width(title))), muted.Render(extras[i]))
		if p := t.Priority; p == "urgent" || p == "high" {
			row += " " + priority(p)
		}
		rows[i] = row
	}
	height, cursor := a.rows()-2, v.cursor
	if v.height > 0 {
		height = v.height
	}
	if v.inactive {
		cursor = -1
	}
	return b.String() + lines(rows, cursor, height)
}

const scrollStep = 4 // characters per h/l

// maxScroll is how far the selected title can slide: until its end shows.
func (v *listView) maxScroll() int {
	if len(v.items) == 0 || v.titleW < 2 {
		return 0
	}
	return max(0, len([]rune(v.items[v.cursor].Title))-(v.titleW-1))
}

// slide is title from character from, in width cells: "…" first when it doesn't start at the beginning.
func slide(title string, from, width int) string {
	r := []rune(title)
	if from <= 0 || from >= len(r) {
		return cut(title, width)
	}
	return "…" + cut(string(r[from:]), width-1)
}

func (v *listView) help() string {
	h := "j/k move · h/l scroll title · enter open · / search · t go to · 1–5 tabs · r reload · ? help · q quit"
	if v.project != "" {
		h = "j/k move · h/l scroll title · enter open · n new ticket · s sort · f labels · esc/q back · Q quit"
	}
	if v.query != "" {
		h = "j/k move · h/l scroll title · enter open · / search again · r retry · esc/q back · Q quit"
	}
	return h
}

func (v *listView) typing() bool { return false }

// homeView is My work, Created by me and Mentions stacked one above the other, with any standup or
// refinement running in the organization on top (enter follows it); tab moves between the sections.
// A section with nothing in it isn't shown: Created by me appears only once you've raised a ticket,
// and Mentions once someone has mentioned you (TL-490).
type homeView struct {
	sections  [3]*listView // 0 My work, 1 Created by me, 2 Mentions
	live      []liveSession
	liveAt    time.Time // when the sessions were last looked for
	liveRow   int
	focus     int // a section index 0–2, or focusLive for the running sessions
	forceLive bool
}

const liveEvery = 30 * time.Second // look for sessions again at most this often when coming back here

const focusLive = 3 // focus value for the running-sessions row (sections are 0–2)

func newHome() *homeView {
	return &homeView{sections: [3]*listView{{tag: "mywork", heading: "My work"}, {tag: "created", heading: "Created by me"}, {tag: "mentions", heading: "Mentions"}}}
}

func (v *homeView) load(a *app) tea.Cmd {
	cmds := []tea.Cmd{v.sections[0].load(a), v.sections[1].load(a), v.sections[2].load(a)}
	if v.forceLive || time.Since(v.liveAt) > liveEvery {
		v.liveAt, v.forceLive = time.Now(), false
		cmds = append(cmds, fetch("live", func(ctx context.Context) (any, error) { return runningSessions(ctx, a.api) }))
	}
	return tea.Batch(cmds...)
}

// visible is the sections with something to show, in order; one still loading counts as visible so it
// doesn't flash in and out. My work is kept even when empty until it has loaded.
func (v *homeView) visible() []int {
	var out []int
	for i, s := range v.sections {
		if !s.loaded || len(s.items) > 0 {
			out = append(out, i)
		}
	}
	return out
}

// order is what tab goes through, top to bottom: running sessions (while there are some), then the
// visible sections.
func (v *homeView) order() []int {
	o := v.visible()
	if len(v.live) > 0 {
		return append([]int{focusLive}, o...)
	}
	return o
}

// focusValid keeps the focus on something that's actually on screen (a section may have just been
// hidden, or the sessions ended).
func (v *homeView) focusValid() {
	o := v.order()
	if len(o) == 0 {
		v.focus = 0
		return
	}
	if indexOf(o, v.focus) < 0 {
		v.focus = o[0]
	}
}

func (v *homeView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg: // each section takes its own
		if l, isLive := msg.data.([]liveSession); isLive && msg.tag == "live" {
			v.live = l
			v.liveRow = min(v.liveRow, max(len(l)-1, 0))
			v.focusValid()
			return v, nil
		}
		var cmds []tea.Cmd
		for _, s := range v.sections {
			_, cmd := s.update(a, msg)
			cmds = append(cmds, cmd)
		}
		v.focusValid()
		return v, tea.Batch(cmds...)
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "shift+tab":
			o := v.order()
			if len(o) == 0 {
				return v, nil
			}
			d := 1
			if msg.String() == "shift+tab" {
				d = -1
			}
			v.focus = o[(indexOf(o, v.focus)+d+len(o))%len(o)]
			return v, nil
		case "r":
			a.loading, v.forceLive = true, true
			return v, v.load(a)
		}
		if v.focus == focusLive {
			if cursorKeys(msg.String(), &v.liveRow, len(v.live)) {
				return v, nil
			}
			if k := msg.String(); (k == "enter" || k == "f") && len(v.live) > 0 {
				l := v.live[v.liveRow]
				return v, msgOf(pushMsg{&followView{kind: l.kind, id: l.id, name: l.name}})
			}
			return v, nil
		}
	}
	_, cmd := v.sections[v.focus].update(a, msg)
	return v, cmd
}

func (v *homeView) render(a *app) string {
	var out []string
	used := 0
	if len(v.live) > 0 {
		rows := make([]string, len(v.live))
		for i, l := range v.live {
			where := l.name
			if l.where != "" {
				where = l.where + " → " + l.name
			}
			rows[i] = ok.Render("● ") + strings.ToUpper(l.kind[:1]) + l.kind[1:] + " on " + bold.Render(where) + muted.Render(", hosted by "+a.name(l.host))
		}
		cursor := -1
		if v.focus == focusLive {
			cursor = v.liveRow
		}
		out = append(out, accent.Render("Running now")+muted.Render("  enter to follow")+"\n"+lines(rows, cursor, 3))
		used = min(len(rows), 3) + 2
	}
	// split what's left evenly between the visible sections (heading + blank cost a few rows each)
	vis := v.visible()
	rest := a.rows() - used
	each := max(3, rest/max(1, len(vis))-3)
	for _, i := range vis {
		s := v.sections[i]
		s.height, s.inactive = each, i != v.focus
		out = append(out, s.render(a))
	}
	return strings.Join(out, "\n")
}

func (v *homeView) help() string {
	var names []string
	if len(v.live) > 0 {
		names = append(names, "running now")
	}
	for _, i := range v.visible() {
		names = append(names, strings.ToLower(v.sections[i].heading))
	}
	return "tab " + strings.Join(names, "/") + " · j/k move · h/l scroll title · enter open · / search · t go to · 1–5 tabs · r reload · ? help · q quit"
}
func (v *homeView) typing() bool { return false }

// searchView asks what to look for, then shows the results in its place.
type searchView struct{ input textinput.Model }

// newSearch starts from q: the last search, when searching again from its results.
func newSearch(q string) *searchView {
	in := textinput.New()
	in.SetValue(q)
	in.Placeholder = "words or a key (TL-123)"
	in.Prompt = "/ "
	in.Focus()
	return &searchView{in}
}

func (v *searchView) load(a *app) tea.Cmd {
	a.loading = false
	return textinput.Blink
}

func (v *searchView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	if k, isKey := msg.(tea.KeyMsg); isKey {
		switch k.String() {
		case "esc":
			return v, msgOf(popMsg{})
		case "enter":
			q := strings.TrimSpace(v.input.Value())
			if q == "" {
				return v, msgOf(popMsg{})
			}
			return v, msgOf(replaceMsg{&listView{tag: "search:" + q, heading: "Search: " + q, query: q}})
		}
	}
	var cmd tea.Cmd
	v.input, cmd = v.input.Update(msg)
	return v, cmd
}

func (v *searchView) render(a *app) string {
	return accent.Render("Search") + "\n\n" + v.input.View() + "\n"
}
func (v *searchView) help() string { return "enter search · esc cancel" }
func (v *searchView) typing() bool { return true }

// gotoView opens a ticket by its key: TL-308, or tl-308.
type gotoView struct{ input textinput.Model }

func newGoto() *gotoView {
	in := textinput.New()
	in.Placeholder = "TL-308"
	in.Prompt = "go to "
	in.CharLimit = 20
	in.Focus()
	return &gotoView{in}
}

func (v *gotoView) load(a *app) tea.Cmd {
	a.loading = false
	return textinput.Blink
}

// validKey is a ticket key: letters (and digits), a dash, a number.
var validKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-[0-9]+$`)

func (v *gotoView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	if k, isKey := msg.(tea.KeyMsg); isKey {
		switch k.String() {
		case "esc":
			return v, msgOf(popMsg{})
		case "enter":
			key := strings.ToUpper(strings.TrimSpace(v.input.Value()))
			if !validKey.MatchString(key) {
				a.status = warn.Render("A ticket key, like TL-308.")
				return v, nil
			}
			a.loading = true
			return v, func() tea.Msg {
				t, err := a.api.TicketByKey(context.Background(), key)
				if e, isAPI := err.(*APIError); isAPI && e.Status == http.StatusNotFound {
					return statusMsg{err: fmt.Errorf("There’s no ticket %s.", key)}
				}
				if err != nil {
					return statusMsg{err: err}
				}
				return replaceMsg{&ticketView{id: t.ID}}
			}
		}
	}
	var cmd tea.Cmd
	v.input, cmd = v.input.Update(msg)
	return v, cmd
}

func (v *gotoView) render(a *app) string {
	return accent.Render("Go to a ticket") + "\n\n" + v.input.View() + "\n"
}
func (v *gotoView) help() string { return "enter open · esc cancel" }
func (v *gotoView) typing() bool { return true }

// projectsView lists the organization's projects.
type projectsView struct {
	items  []Project
	cursor int
}

func (v *projectsView) load(a *app) tea.Cmd {
	return fetch("projects", func(ctx context.Context) (any, error) { return a.api.Projects(ctx) })
}

func (v *projectsView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		if p, isProjects := msg.data.([]Project); isProjects && msg.tag == "projects" {
			v.items = p
			v.cursor = min(v.cursor, max(len(p)-1, 0))
			ids := map[string]bool{}
			for _, x := range p {
				ids[x.ID] = true
			}
			a.usage.Prune(ids) // gone, or another organization's: no longer a favourite
		}
	case tea.KeyMsg:
		if cursorKeys(msg.String(), &v.cursor, len(v.items)) {
			return v, nil
		}
		if k := msg.String(); (k == "enter" || k == "l") && len(v.items) > 0 {
			return v, msgOf(pushMsg{&projectView{id: v.items[v.cursor].ID}})
		}
	}
	return v, nil
}

func (v *projectsView) render(a *app) string {
	rows := make([]string, len(v.items))
	for i, p := range v.items {
		rows[i] = fmt.Sprintf("%-5s %s  %s", p.KeyPrefix, bold.Render(p.Name), muted.Render(cut(p.Description, max(10, a.width-len(p.Name)-12))))
	}
	if len(rows) == 0 {
		return accent.Render("Projects") + "\n\n" + muted.Render("No projects yet.") + "\n"
	}
	return accent.Render("Projects") + "\n\n" + lines(rows, v.cursor, a.rows()-2)
}

func (v *projectsView) help() string {
	return "j/k move · enter open · / search · t go to · 1–5 tabs · ? help · q quit"
}
func (v *projectsView) typing() bool { return false }

// projectView is a project's boards, and its backlog.
type projectView struct {
	id         string
	project    *Project
	cursor     int
	counted    bool     // this visit's been counted toward your most used (TL-477)
	refinement *Session // a refinement running on it
}

func (v *projectView) load(a *app) tea.Cmd {
	return tea.Batch(fetch("project:"+v.id, func(ctx context.Context) (any, error) { return a.api.Project(ctx, v.id) }), running(a, "refinement", v.id))
}

func (v *projectView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		if p, isProject := msg.data.(Project); isProject && msg.tag == "project:"+v.id {
			v.project = &p
			if !v.counted { // once per visit: coming back from a board isn't another
				v.counted = true
				a.usage.Opened(p.ID, p.Name, time.Now())
			}
		}
		if s, isSession := msg.data.(*Session); isSession && msg.tag == "running:refinement:"+v.id {
			v.refinement = s
		}
	case tea.KeyMsg:
		if v.project == nil {
			return v, nil
		}
		if msg.String() == "f" && v.refinement != nil {
			return v, msgOf(pushMsg{&followView{kind: "refinement", id: v.id, name: v.project.Name}})
		}
		n := len(v.project.Boards) + 1 // the backlog is last
		if cursorKeys(msg.String(), &v.cursor, n) {
			return v, nil
		}
		if k := msg.String(); k == "enter" || k == "l" {
			if v.cursor < len(v.project.Boards) {
				return v, msgOf(pushMsg{&boardView{id: v.project.Boards[v.cursor].ID, project: v.project.Name}})
			}
			return v, msgOf(pushMsg{&listView{tag: "backlog:" + v.id, heading: v.project.Name + " → Backlog", project: v.id, sort: backlogSort}})
		}
	}
	return v, nil
}

func (v *projectView) render(a *app) string {
	if v.project == nil {
		return ""
	}
	// names padded to the longest, so the sprints/kanban column lines up
	width := len("Backlog")
	for _, b := range v.project.Boards {
		width = max(width, lipgloss.Width(b.Name))
	}
	var rows []string
	for _, b := range v.project.Boards {
		style := "sprints"
		if b.Style == "kanban" {
			style = "kanban"
		}
		rows = append(rows, b.Name+strings.Repeat(" ", width-lipgloss.Width(b.Name))+"  "+muted.Render(style))
	}
	rows = append(rows, "Backlog")
	head := accent.Render(v.project.Name)
	if d := strings.TrimSpace(v.project.Description); d != "" {
		head += "\n" + muted.Render(wrap(d, a.width))
	}
	return head + "\n" + sessionBanner(a, "refinement", v.refinement) + "\n" + lines(rows, v.cursor, a.rows()-4)
}

func (v *projectView) help() string { return "j/k move · enter open · esc/q back · Q quit" }
func (v *projectView) typing() bool { return false }

// boardView shows a board's columns side by side. h/l pick a column, j/k a ticket; H/L move the
// ticket to the column either side, n makes a new one in this column.
type boardView struct {
	id         string
	standup    *Session // a standup running on it
	project    string   // its project's name: the board doesn't carry it
	board      *Board
	col, row   int
	firstShown int // the leftmost column on screen, when they don't all fit
}

func (v *boardView) load(a *app) tea.Cmd {
	return tea.Batch(fetch("board:"+v.id, func(ctx context.Context) (any, error) { return a.api.Board(ctx, v.id) }), running(a, "standup", v.id))
}

func (v *boardView) current() *Ticket {
	if v.board == nil || len(v.board.Columns) == 0 || len(v.board.Columns[v.col].Tickets) == 0 {
		return nil
	}
	return &v.board.Columns[v.col].Tickets[v.row]
}

func (v *boardView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		if s, isSession := msg.data.(*Session); isSession && msg.tag == "running:standup:"+v.id {
			v.standup = s
		}
		if b, isBoard := msg.data.(Board); isBoard && msg.tag == "board:"+v.id {
			v.board = &b
			v.col = min(v.col, max(len(b.Columns)-1, 0))
			if len(b.Columns) > 0 {
				v.row = min(v.row, max(len(b.Columns[v.col].Tickets)-1, 0))
			}
		}
	case tea.KeyMsg:
		if v.board == nil || len(v.board.Columns) == 0 {
			return v, nil
		}
		cols := v.board.Columns
		switch msg.String() {
		case "h", "left":
			v.col, v.row = step(v.col, -1, len(cols)), 0
		case "l", "right":
			v.col, v.row = step(v.col, 1, len(cols)), 0
		case "j", "down", "k", "up", "g", "home", "G":
			cursorKeys(msg.String(), &v.row, len(cols[v.col].Tickets))
		case "enter":
			if t := v.current(); t != nil {
				return v, msgOf(pushMsg{&ticketView{id: t.ID}})
			}
		case "n":
			return v, msgOf(pushMsg{newForm(nil, v.board.ProjectID, cols[v.col].ID, v.board.EstimateScale)})
		case "f":
			if v.standup != nil {
				return v, msgOf(pushMsg{&followView{kind: "standup", id: v.id, name: v.board.Name}})
			}
		case "H", "L":
			t := v.current()
			to := v.col - 1
			if msg.String() == "L" {
				to = v.col + 1
			}
			if t == nil || to < 0 || to >= len(cols) {
				return v, nil
			}
			id, key, dest := t.ID, t.Key, cols[to]
			v.col, v.row = to, len(dest.Tickets) // follow it to the bottom of its new column
			a.loading = true
			return v, act("Moved "+key+" to "+dest.Name+".", func(ctx context.Context) error { return a.api.Move(ctx, id, dest) })
		case "r":
			a.loading = true
			return v, v.load(a)
		}
	}
	return v, nil
}

const colWidth = 28

func (v *boardView) render(a *app) string {
	if v.board == nil {
		return ""
	}
	cols := v.board.Columns
	fit := max(1, a.width/(colWidth+1))
	if v.col < v.firstShown {
		v.firstShown = v.col
	} else if v.col >= v.firstShown+fit {
		v.firstShown = v.col - fit + 1
	}
	end := min(len(cols), v.firstShown+fit)
	height := a.rows() - 4
	var blocks []string
	for i := v.firstShown; i < end; i++ {
		c := cols[i]
		head := fmt.Sprintf("%s %d", c.Name, len(c.Tickets))
		if i == v.col {
			head = accent.Render(head)
		} else {
			head = bold.Render(head)
		}
		var rows []string
		for _, t := range c.Tickets {
			row := cut(t.Key+" "+t.Title, colWidth-1)
			if t.Priority == "urgent" {
				row = warn.Render(row)
			}
			rows = append(rows, row)
		}
		cursor := -1
		if i == v.col {
			cursor = v.row
		}
		body := lines(rows, cursor, height)
		if len(rows) == 0 {
			body = muted.Render("empty")
		}
		blocks = append(blocks, lipgloss.NewStyle().Width(colWidth).Render(head+"\n"+body))
	}
	more := ""
	if v.firstShown > 0 || end < len(cols) {
		more = muted.Render(fmt.Sprintf("  columns %d–%d of %d", v.firstShown+1, end, len(cols)))
	}
	title := v.board.Name
	if v.project != "" {
		title = v.project + " → " + title // e.g. BugFixes → LandingPage
	}
	return accent.Render(title) + more + "\n" + sessionBanner(a, "standup", v.standup) + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, intersperse(blocks)...)
}

// intersperse puts a gap between the columns.
func intersperse(blocks []string) []string {
	var out []string
	for i, b := range blocks {
		if i > 0 {
			out = append(out, " ")
		}
		out = append(out, b)
	}
	return out
}

func (v *boardView) help() string {
	return "h/l column · j/k ticket · enter open · H/L move ticket · n new · r reload · esc/q back"
}
func (v *boardView) typing() bool { return false }

// ticketView is one ticket: what it is, where, its description and comments.
type ticketView struct {
	id       string
	ticket   *Ticket
	comments []Comment
	files    []Attachment
	view     viewport.Model
	sel      int // picked row, 1-based over sub-tickets then files; 0 = none (TL-514, TL-522)
}

type ticketData struct {
	t        Ticket
	comments []Comment
	files    []Attachment
}

// openFile hands a file's URL to the system's viewer (xdg-open, or open on macOS) and doesn't wait
// for it, so the TUI keeps running (TL-522). A variable so tests can see what would open.
var openFile = func(url string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	cmd := exec.Command(opener, url)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck // reaped, nothing to report: the viewer is on its own now
	return nil
}

func (v *ticketView) load(a *app) tea.Cmd {
	return fetch("ticket:"+v.id, func(ctx context.Context) (any, error) {
		t, err := a.api.Ticket(ctx, v.id)
		if err != nil {
			return nil, err
		}
		cs, err := a.api.Comments(ctx, v.id)
		if err != nil {
			return nil, err
		}
		files, _ := a.api.Attachments(ctx, v.id) // ponytail: the ticket still shows if its files can't load
		return ticketData{t, cs, files}, nil
	})
}

func (v *ticketView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		if d, isTicket := msg.data.(ticketData); isTicket && msg.tag == "ticket:"+v.id {
			v.ticket, v.comments, v.files = &d.t, d.comments, d.files
			v.view = viewport.New(a.width, a.rows())
			v.view.SetContent(v.detail(a))
			return v, v.timeboxTickCmd() // live countdown while this spike is In Progress (TL-512)
		}
	case timeboxTick:
		if msg.id != v.id {
			return v, nil
		}
		return v, v.timeboxTickCmd() // re-arm; render() recomputes detail() with the current time
	case tea.KeyMsg:
		if v.ticket == nil {
			return v, nil
		}
		t := v.ticket
		switch msg.String() {
		case "e":
			if t.BoardID == nil {
				return v, msgOf(pushMsg{newForm(t, t.ProjectID, "", "none")})
			}
			// the board says what estimates it takes
			board := *t.BoardID
			return v, func() tea.Msg {
				b, err := a.api.Board(context.Background(), board)
				if err != nil {
					return statusMsg{err: err}
				}
				return pushMsg{newForm(t, t.ProjectID, "", b.EstimateScale)}
			}
		case "m":
			return v, msgOf(pushMsg{&moveView{ticket: *t}})
		case "x":
			// backlog tickets have no Done column to move into, so they close in place (TL-272);
			// board tickets close by moving to Done, which 'm' already does.
			if t.BoardID != nil || t.Done {
				return v, nil
			}
			return v, msgOf(pushMsg{&resolveView{ticket: *t}})
		case "c":
			return v, msgOf(pushMsg{newComment(*t)})
		case "a":
			if t.Done { // finished work isn't assigned (the API refuses it too)
				return v, nil
			}
			if a.me == "" {
				a.status = warn.Render("Couldn’t tell who you are, so can’t assign.")
				return v, nil
			}
			id := t.ID
			if t.AssignedTo != nil && *t.AssignedTo == a.me {
				a.loading = true
				return v, act("Unassigned.", func(ctx context.Context) error { return a.api.Assign(ctx, id, nil) })
			}
			me := a.me
			a.loading = true
			return v, act("Assigned to you.", func(ctx context.Context) error { return a.api.Assign(ctx, id, &me) })
		case "r":
			a.loading = true
			return v, v.load(a)
		case "tab", "shift+tab": // pick a sub-ticket or file; enter opens it (TL-514, TL-522)
			if n := len(t.Children) + len(v.files); n > 0 {
				if msg.String() == "tab" {
					v.sel = v.sel%n + 1
				} else if v.sel <= 1 {
					v.sel = n
				} else {
					v.sel--
				}
			}
			return v, nil
		case "enter", "o":
			if k := msg.String(); k == "enter" && v.sel > 0 && v.sel <= len(t.Children) {
				return v, msgOf(pushMsg{&ticketView{id: t.Children[v.sel-1].ID}})
			}
			if i := v.sel - len(t.Children) - 1; i >= 0 && i < len(v.files) {
				f := v.files[i]
				if err := openFile(f.URL); err != nil {
					a.status = warn.Render("Couldn’t open " + f.Name + ": " + err.Error())
				} else {
					a.status = ok.Render("Opened " + f.Name + ".")
				}
			}
			return v, nil
		case "p":
			if t.Parent != nil {
				return v, msgOf(pushMsg{&ticketView{id: t.Parent.ID}})
			}
			return v, nil
		}
		var cmd tea.Cmd
		v.view, cmd = v.view.Update(msg)
		return v, cmd
	}
	return v, nil
}

type timeboxTick struct{ id string }

// timeboxTickCmd ticks every minute while viewing an In Progress spike that has a timebox, so the
// countdown advances live; nil (no tick) otherwise, so it stops when the spike is done or on leaving.
func (v *ticketView) timeboxTickCmd() tea.Cmd {
	t := v.ticket
	if t == nil || t.Type != "spike" || t.Done || t.StartedAt == nil {
		return nil
	}
	if timeboxLabel(t.TimeboxDays, t.TimeboxHours) == "" {
		return nil
	}
	id := v.id
	return tea.Tick(time.Minute, func(time.Time) tea.Msg { return timeboxTick{id} })
}

func (v *ticketView) detail(a *app) string {
	t := v.ticket
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", accent.Render(t.Key), bold.Render(t.Title))
	meta := []string{t.Type, priority(t.Priority), where(*t)}
	if t.Estimate != nil {
		meta = append(meta, "estimate "+*t.Estimate)
	}
	if tb := timeboxMeta(t, time.Now()); tb != "" {
		meta = append(meta, tb)
	}
	if t.AssignedTo != nil {
		meta = append(meta, "assigned to "+a.name(*t.AssignedTo))
	} else {
		meta = append(meta, "unassigned")
	}
	if t.Blocked {
		meta = append(meta, warn.Render("blocked"))
	}
	if t.Done {
		meta = append(meta, ok.Render("done"))
	}
	b.WriteString(muted.Render(strings.Join(meta, " · ")) + "\n")
	if t.Parent != nil {
		b.WriteString(muted.Render("parent: ") + t.Parent.Title + muted.Render(" (p to open)") + "\n")
	}
	b.WriteString("\n")
	if d := strings.TrimSpace(t.Description); d != "" {
		b.WriteString(wrap(d, a.width) + "\n\n")
	} else {
		b.WriteString(muted.Render("No description.") + "\n\n")
	}
	if len(t.Children) > 0 {
		b.WriteString(v.subTickets(a) + "\n")
	}
	if len(v.files) > 0 {
		b.WriteString(v.attachments(a) + "\n")
	}
	b.WriteString(accent.Render(fmt.Sprintf("Comments (%d)", len(v.comments))) + "\n")
	for _, c := range v.comments {
		fmt.Fprintf(&b, "\n%s %s\n%s\n", bold.Render(a.name(c.Author)), muted.Render(day(c.CreatedAt)), wrap(c.Body, a.width))
	}
	return b.String()
}

// subTickets lists the sub-tickets (TL-514), the picked one highlighted; an epic's header carries
// its progress: "Sub-tickets 3/7 done · 13 pts · 2 unestimated".
func (v *ticketView) subTickets(a *app) string {
	t := v.ticket
	head := fmt.Sprintf("Sub-tickets (%d)", len(t.Children))
	if e := t.Epic; e != nil {
		parts := []string{fmt.Sprintf("%d/%d done", e.Done, e.Total)}
		if e.Points > 0 {
			parts = append(parts, strconv.FormatFloat(e.Points, 'f', -1, 64)+" pts")
		}
		if e.Unestimated > 0 {
			parts = append(parts, fmt.Sprintf("%d unestimated", e.Unestimated))
		}
		head = "Sub-tickets " + strings.Join(parts, " · ")
	}
	var b strings.Builder
	b.WriteString(accent.Render(head) + "\n")
	for i, c := range t.Children {
		mark := "  "
		if c.Done {
			mark = ok.Render("✓ ")
		}
		key := ""
		if c.Key != nil {
			key = *c.Key + "  "
		}
		where := "backlog"
		if c.BoardName != nil {
			where = *c.BoardName
		}
		who := "unassigned"
		if c.AssignedTo != nil {
			who = a.name(*c.AssignedTo)
		}
		row := key + c.Title
		if i+1 == v.sel {
			row = picked.Render(row)
		}
		b.WriteString(mark + row + muted.Render(" · "+c.Type+" · "+who+" · "+where) + "\n")
	}
	return b.String()
}

// attachments lists the ticket's files (TL-522), picked after the sub-tickets; o opens one.
func (v *ticketView) attachments(a *app) string {
	var b strings.Builder
	b.WriteString(accent.Render(fmt.Sprintf("Attachments (%d)", len(v.files))) + "\n")
	for i, f := range v.files {
		name := f.Name
		if len(v.ticket.Children)+i+1 == v.sel {
			name = picked.Render(name)
		}
		b.WriteString("  " + name + muted.Render(" · "+fileSize(f.Size)+" · "+a.name(f.CreatedBy)) + "\n")
	}
	return b.String()
}

// fileSize reads like the web app's: 512 B, 1.5 KB, 2.0 MB.
func fileSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/1024/1024)
}

func (v *ticketView) render(a *app) string {
	if v.ticket == nil {
		return ""
	}
	v.view.Width, v.view.Height = a.width, a.rows()
	v.view.SetContent(v.detail(a)) // names may have arrived, or the window changed
	return v.view.View()
}

func (v *ticketView) help() string {
	if v.ticket != nil && v.ticket.Done {
		return "j/k scroll · " + v.treeHelp() + "e edit · m move · c comment · r reload · esc/q back"
	}
	resolve := ""
	if v.ticket != nil && v.ticket.BoardID == nil {
		resolve = "x close/won’t do · "
	}
	return "j/k scroll · " + v.treeHelp() + "e edit · m move · c comment · a assign me / unassign · " + resolve + "r reload · esc/q back"
}

// treeHelp is the sub-ticket/parent keys, when the ticket has either.
func (v *ticketView) treeHelp() string {
	if v.ticket == nil {
		return ""
	}
	h := ""
	switch kids, files := len(v.ticket.Children), len(v.files); {
	case kids > 0 && files > 0:
		h += "tab pick sub-ticket/file · enter open · "
	case kids > 0:
		h += "tab pick sub-ticket · enter open · "
	case files > 0:
		h += "tab pick file · o open · "
	}
	if v.ticket.Parent != nil {
		h += "p parent · "
	}
	return h
}
func (v *ticketView) typing() bool { return false }

// resolveView closes a backlog ticket in place: done (finished) or won't do (decided against). The
// creator is told either way; a reason goes on first as a comment ('c'). Moving it to a board reopens it.
type resolveView struct {
	ticket Ticket
	cursor int
}

func (v *resolveView) load(a *app) tea.Cmd { a.loading = false; return nil }

func (v *resolveView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	if msg, isKey := msg.(tea.KeyMsg); isKey {
		if cursorKeys(msg.String(), &v.cursor, 2) {
			return v, nil
		}
		if msg.String() == "enter" {
			id := v.ticket.ID
			res, done := api.ResolutionDone, "Closed as done."
			if v.cursor == 1 {
				res, done = api.ResolutionWontDo, "Closed as won’t do."
			}
			return v, func() tea.Msg {
				if err := a.api.Resolve(context.Background(), id, res, ""); err != nil {
					return statusMsg{err: err}
				}
				return popMsg{ok.Render(done)}
			}
		}
	}
	return v, nil
}

func (v *resolveView) render(a *app) string {
	rows := []string{"  Close (done)", "  Won’t do (ignore)"}
	return accent.Render("Resolve "+v.ticket.Key) + "  " + v.ticket.Title + "\n\n" + lines(rows, v.cursor, a.rows()-2)
}

func (v *resolveView) help() string { return "j/k pick · enter resolve · esc cancel" }
func (v *resolveView) typing() bool { return false }

// moveView picks the column to move a ticket to, on its board. A backlog ticket picks a board
// first, and an estimate after the column when that board sizes its tickets (TL-603). The API says
// why when a move isn't allowed (a read-only key, a sprint on its last day).
type moveView struct {
	ticket    Ticket
	boards    []Board // backlog: the project's boards, until one is picked
	board     *Board
	column    *Column // picked; waiting for an estimate
	estimates []string
	cursor    int
}

func (v *moveView) load(a *app) tea.Cmd {
	switch {
	case v.board != nil:
		a.loading = false
		return nil
	case v.ticket.BoardID == nil:
		project := v.ticket.ProjectID
		return fetch("move:"+v.ticket.ID, func(ctx context.Context) (any, error) { return a.api.Project(ctx, project) })
	}
	return v.loadBoard(a, *v.ticket.BoardID)
}

func (v *moveView) loadBoard(a *app, id string) tea.Cmd {
	a.loading = true
	return fetch("move:"+v.ticket.ID, func(ctx context.Context) (any, error) { return a.api.Board(ctx, id) })
}

func (v *moveView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		if msg.tag != "move:"+v.ticket.ID {
			return v, nil
		}
		switch d := msg.data.(type) {
		case Project:
			v.boards = d.Boards
		case Board:
			v.board, v.cursor = &d, 0
		}
	case tea.KeyMsg:
		k := msg.String()
		if cursorKeys(k, &v.cursor, len(v.rows())) || k != "enter" || len(v.rows()) == 0 {
			return v, nil
		}
		switch {
		case v.board == nil:
			return v, v.loadBoard(a, v.boards[v.cursor].ID)
		case v.column == nil:
			col := v.board.Columns[v.cursor]
			if v.ticket.BoardID == nil && api.Scales[v.board.EstimateScale] != nil {
				v.column, v.estimates = &col, api.Scales[v.board.EstimateScale]
				v.cursor = 0
				if v.ticket.Estimate != nil {
					v.cursor = max(indexOf(v.estimates, *v.ticket.Estimate), 0)
				}
				return v, nil
			}
			return v, v.move(a, col, "")
		default:
			return v, v.move(a, *v.column, v.estimates[v.cursor])
		}
	}
	return v, nil
}

func (v *moveView) move(a *app, col Column, estimate string) tea.Cmd {
	id := v.ticket.ID
	return func() tea.Msg {
		if err := a.api.MoveSized(context.Background(), id, col, estimate); err != nil {
			return statusMsg{err: err}
		}
		return popMsg{ok.Render("Moved to " + col.Name + ".")}
	}
}

// rows is what's being picked from: boards, columns or estimates.
func (v *moveView) rows() []string {
	var rows []string
	switch {
	case v.board == nil:
		for _, b := range v.boards {
			rows = append(rows, "  "+b.Name)
		}
	case v.column == nil:
		for _, c := range v.board.Columns {
			row := "  " + c.Name
			if v.ticket.ColumnID != nil && *v.ticket.ColumnID == c.ID {
				row += muted.Render("  (now)")
			}
			rows = append(rows, row)
		}
	default:
		for _, e := range v.estimates {
			rows = append(rows, "  "+e)
		}
	}
	return rows
}

func (v *moveView) render(a *app) string {
	title := "Move " + v.ticket.Key + " to"
	switch {
	case v.board == nil && v.boards == nil:
		return ""
	case v.board == nil:
		if len(v.boards) == 0 {
			return accent.Render(title) + "\n\n" + muted.Render("This project has no boards yet.") + "\n"
		}
		title += " which board"
	case v.column == nil:
		title += " " + v.board.Name
	default:
		title = "Estimate " + v.ticket.Key + muted.Render("  "+v.board.Name+" sizes on the "+v.board.EstimateScale+" scale")
	}
	return accent.Render(title) + "\n\n" + lines(v.rows(), v.cursor, a.rows()-2)
}

func (v *moveView) help() string {
	switch {
	case v.board == nil:
		return "j/k pick a board · enter next · esc cancel"
	case v.column != nil:
		return "j/k pick an estimate · enter move · esc cancel"
	}
	return "j/k pick a column · enter move · esc cancel"
}
func (v *moveView) typing() bool { return false }

// commentView writes a comment on a ticket.
type commentView struct {
	ticket Ticket
	input  vimArea
}

func newComment(t Ticket) *commentView { return &commentView{t, newVimArea("Your comment (markdown)")} }

func (v *commentView) load(a *app) tea.Cmd {
	a.loading = false
	v.size(a)
	return textarea.Blink
}

// size fills the screen's body with the text area, less the title and mode lines (TL-523).
func (v *commentView) size(a *app) {
	v.input.SetWidth(max(20, a.width-2))
	v.input.SetHeight(max(3, a.rows()-3))
}

func (v *commentView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	action, cmd := v.input.update(msg)
	switch action {
	case vimCancel:
		return v, msgOf(popMsg{})
	case vimSend:
		body, id := strings.TrimSpace(v.input.Value()), v.ticket.ID
		if body == "" {
			return v, nil
		}
		return v, func() tea.Msg {
			if err := a.api.Comment(context.Background(), id, body); err != nil {
				return statusMsg{err: err}
			}
			return popMsg{ok.Render("Comment added.")}
		}
	}
	return v, cmd
}

func (v *commentView) render(a *app) string {
	v.size(a) // the window may have changed
	return accent.Render("Comment on "+v.ticket.Key) + "  " + v.ticket.Title + "\n\n" + v.input.View() + "\n" + v.input.mode() + "\n"
}
func (v *commentView) help() string { return editHelp() }
func (v *commentView) typing() bool { return true }

var (
	types      = []string{"task", "bug", "feature", "spike"}
	priorities = []string{"low", "medium", "high", "urgent"}
)

// formView creates a ticket (in a column, or a project's backlog) or edits one: title, type,
// priority, estimate (on boards that use them), an optional spike timebox, and description. It works
// like vim: typing in the title or description is insert mode, and esc leaves it. Out of insert mode,
// j/k or tab move between fields, i or enter edits one, h/l change a choice, q cancels (twice once
// something's typed), and ctrl+s saves from anywhere.
type formView struct {
	editing                *Ticket
	project, column, scale string
	title                  textinput.Model
	description            vimArea
	kind, prio, estimate   int // indexes into types, priorities and the scale (-1: not estimated)
	timeboxDays            int // -1: not set; otherwise 0–60
	timeboxHours           int // -1: not set; otherwise 0–23
	field                  int
	typingTitle            bool // insert mode in the title
	pendingQ               bool // q (esc with standard keys) pressed once with something typed: again discards it
	cmd                    cmdLine
	before                 string // the full form state as it started, to tell if anything changed
}

const (
	fieldTitle = iota
	fieldType
	fieldPriority
	fieldEstimate
	fieldTimeboxDays
	fieldTimeboxHours
	fieldDescription
)

func newForm(t *Ticket, project, column, scale string) *formView {
	f := &formView{editing: t, project: project, column: column, scale: scale, prio: 1, estimate: -1, timeboxDays: -1, timeboxHours: -1, typingTitle: true}
	f.title = textinput.New()
	f.title.Placeholder = "Title"
	f.title.CharLimit = 200
	f.description = newVimArea("Description (markdown)")
	f.description.Blur()
	if t != nil {
		f.title.SetValue(t.Title)
		f.description.SetValue(t.Description)
		f.kind, f.prio = max(0, indexOf(types, t.Type)), max(0, indexOf(priorities, t.Priority))
		if t.Estimate != nil {
			f.estimate = indexOf(api.Scales[scale], *t.Estimate)
		}
		if t.TimeboxDays != nil {
			f.timeboxDays = *t.TimeboxDays
		}
		if t.TimeboxHours != nil {
			f.timeboxHours = *t.TimeboxHours
		}
	}
	f.before = f.snapshot()
	f.description.unsaved = f.changed // :q in the description weighs the whole form, title too
	f.title.Focus()
	return f
}

func indexOf[T comparable](list []T, s T) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

func (f *formView) load(a *app) tea.Cmd {
	a.loading = false
	f.description.SetWidth(max(20, a.width-2))
	f.description.SetHeight(max(3, a.rows()-11))
	return textinput.Blink
}

func (f *formView) fields() []int {
	fields := []int{fieldTitle, fieldType, fieldPriority}
	if api.Scales[f.scale] != nil {
		fields = append(fields, fieldEstimate)
	}
	if types[f.kind] == "spike" {
		fields = append(fields, fieldTimeboxDays, fieldTimeboxHours)
	}
	return append(fields, fieldDescription)
}

func (f *formView) snapshot() string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d\x00%d\x00%d", f.title.Value(), f.description.Value(), f.kind, f.prio, f.estimate, f.timeboxDays, f.timeboxHours)
}

// focus moves d fields along. typing: start typing in a text field (tab), or not (j/k).
func (f *formView) focus(d int, typing bool) {
	fs := f.fields()
	f.field = fs[(indexOf(fs, f.field)+d+len(fs))%len(fs)]
	f.title.Blur()
	f.description.Blur()
	f.typingTitle = false
	switch f.field {
	case fieldTitle:
		f.title.Focus()
		f.typingTitle = typing
	case fieldDescription:
		f.description.Focus()
		f.description.normal = !typing
	}
}

func (f *formView) changed() bool { return f.snapshot() != f.before }

// input is what saving sends, or why it can't yet.
func (f *formView) input() (TicketInput, string) {
	in := TicketInput{Title: strings.TrimSpace(f.title.Value()), Description: strings.TrimSpace(f.description.Value()), Type: types[f.kind], Priority: priorities[f.prio]}
	if in.Title == "" {
		return in, "A title, please."
	}
	if values := api.Scales[f.scale]; values != nil {
		if f.estimate < 0 {
			if f.editing == nil {
				return in, "This board needs an estimate."
			}
			none := ""
			in.Estimate = &none
		} else {
			in.Estimate = &values[f.estimate]
		}
	}
	// Send a pair for spikes and when editing a spike into another type. Zero/zero means no timebox;
	// omitting fields for ordinary tickets preserves the API's unchanged semantics.
	if types[f.kind] == "spike" || (f.editing != nil && f.editing.Type == "spike") {
		days, hours := max(f.timeboxDays, 0), max(f.timeboxHours, 0)
		if types[f.kind] != "spike" {
			days, hours = 0, 0
		}
		in.TimeboxDays, in.TimeboxHours = &days, &hours
	}
	return in, ""
}

func (f *formView) save(a *app) tea.Cmd {
	in, problem := f.input()
	if problem != "" {
		a.status = warn.Render(problem)
		return nil
	}
	if f.editing != nil {
		id := f.editing.ID
		return func() tea.Msg {
			if err := a.api.Update(context.Background(), id, in); err != nil {
				return statusMsg{err: err}
			}
			return popMsg{ok.Render("Saved.")}
		}
	}
	project, column := f.project, f.column
	return func() tea.Msg {
		t, err := a.api.Create(context.Background(), project, column, in)
		if err != nil {
			return statusMsg{err: err}
		}
		return popMsg{ok.Render("Created " + t.Key + ".")}
	}
}

func (f *formView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	k, isKey := msg.(tea.KeyMsg)
	if !isKey {
		var cmd tea.Cmd
		if f.field == fieldTitle {
			f.title, cmd = f.title.Update(msg)
		} else if f.field == fieldDescription {
			_, cmd = f.description.update(msg)
		}
		return f, cmd
	}
	switch k.String() {
	case "ctrl+s":
		return f, f.save(a)
	case "tab":
		f.focus(1, true)
		return f, nil
	case "shift+tab":
		f.focus(-1, true)
		return f, nil
	}
	if !vimKeys {
		return f.standardKey(k)
	}
	if f.cmd.on && f.field != fieldDescription { // the : prompt (the description has its own)
		switch f.cmd.key(k, f.changed()) {
		case vimSend:
			return f, f.save(a)
		case vimCancel:
			return f, msgOf(popMsg{})
		}
		return f, nil
	}
	switch {
	case f.field == fieldDescription: // its own vim modes
		switch action, cmd := f.description.update(k); action {
		case vimSend:
			return f, f.save(a)
		case vimCancel:
			return f, msgOf(popMsg{})
		default:
			return f, cmd
		}
	case f.field == fieldTitle && f.typingTitle:
		if k.String() == "esc" {
			f.typingTitle = false
			return f, nil
		}
		var cmd tea.Cmd
		f.title, cmd = f.title.Update(k)
		return f, cmd
	}
	// normal mode, on the title or a choice
	key, pending := k.String(), f.pendingQ
	f.pendingQ, f.cmd.msg = false, ""
	switch key {
	case ":":
		f.cmd.open()
	case "q":
		if pending || !f.changed() {
			return f, msgOf(popMsg{})
		}
		f.pendingQ = true
	case "j", "down":
		f.focus(1, false)
	case "k", "up":
		f.focus(-1, false)
	case "i", "a", "I", "A", "enter":
		if f.field == fieldTitle {
			f.typingTitle = true
			switch key {
			case "I":
				f.title.CursorStart()
			case "A":
				f.title.CursorEnd()
			}
		}
	case "left", "right", "h", "l", " ":
		f.choose(key)
	}
	return f, nil
}

// choose cycles the focused choice (type, priority, estimate) back or forward.
func (f *formView) choose(key string) {
	d := 1
	if key == "left" || key == "h" {
		d = -1
	}
	switch f.field {
	case fieldType:
		oldType := types[f.kind]
		f.kind = (f.kind + d + len(types)) % len(types)
		if oldType == "spike" && types[f.kind] != "spike" {
			f.timeboxDays, f.timeboxHours = -1, -1
		}
	case fieldPriority:
		f.prio = (f.prio + d + len(priorities)) % len(priorities)
	case fieldEstimate:
		n := len(api.Scales[f.scale]) + 1 // and "not estimated" (-1)
		f.estimate = (f.estimate+1+d+n)%n - 1
	case fieldTimeboxDays:
		f.timeboxDays = cycleOptional(f.timeboxDays, 60, d)
	case fieldTimeboxHours:
		f.timeboxHours = cycleOptional(f.timeboxHours, 23, d)
	}
}

// cycleOptional moves between "not set" (-1) and the integer range 0..max.
func cycleOptional(value, maxValue, direction int) int {
	count := maxValue + 2 // the integer values plus "not set"
	return (value+1+direction+count)%count - 1
}

// standardKey is the form with standard keys (keys = standard): no modes, typing goes into the
// focused field, ←/→ change a choice, esc cancels (twice once something's typed).
func (f *formView) standardKey(k tea.KeyMsg) (view, tea.Cmd) {
	if k.String() == "esc" {
		if f.pendingQ || !f.changed() {
			return f, msgOf(popMsg{})
		}
		f.pendingQ = true
		return f, nil
	}
	f.pendingQ = false
	var cmd tea.Cmd
	switch f.field {
	case fieldTitle:
		f.title, cmd = f.title.Update(k)
	case fieldDescription:
		_, cmd = f.description.update(k)
	default:
		if key := k.String(); key == "left" || key == "right" || key == "h" || key == "l" || key == " " {
			f.choose(key)
		}
	}
	return f, cmd
}

func (f *formView) mode() string {
	switch {
	case f.pendingQ && !vimKeys:
		return warn.Render("Unsaved changes: esc again to discard them")
	case f.pendingQ:
		return warn.Render("Unsaved changes: q again to discard them")
	case !vimKeys:
		return ""
	case f.field == fieldDescription:
		return f.description.mode()
	case f.cmd.on || f.cmd.msg != "":
		return f.cmd.view()
	case f.field == fieldTitle && f.typingTitle:
		return accent.Render("-- INSERT --")
	}
	return bold.Render("-- NORMAL --")
}

func (f *formView) render(a *app) string {
	head := "New ticket"
	if f.editing != nil {
		head = "Edit " + f.editing.Key
	}
	label := func(field int, name string) string {
		if f.field == field {
			return accent.Render("› " + name)
		}
		return muted.Render("  " + name)
	}
	choice := func(field int, value string) string {
		if f.field == field {
			return "‹ " + bold.Render(value) + " ›"
		}
		return value
	}
	est := "not estimated"
	if f.estimate >= 0 {
		est = api.Scales[f.scale][f.estimate]
	}
	var b strings.Builder
	b.WriteString(accent.Render(head) + "\n\n")
	b.WriteString(label(fieldTitle, "Title") + "\n" + f.title.View() + "\n\n")
	b.WriteString(label(fieldType, "Type") + "  " + choice(fieldType, types[f.kind]) + "\n")
	b.WriteString(label(fieldPriority, "Priority") + "  " + choice(fieldPriority, priorities[f.prio]) + "\n")
	if api.Scales[f.scale] != nil {
		b.WriteString(label(fieldEstimate, "Estimate") + "  " + choice(fieldEstimate, est) + "\n")
	}
	if types[f.kind] == "spike" {
		days, hours := "not set", "not set"
		if f.timeboxDays >= 0 {
			days = fmt.Sprintf("%d days", f.timeboxDays)
			if f.timeboxDays == 1 {
				days = "1 day"
			}
		}
		if f.timeboxHours >= 0 {
			hours = fmt.Sprintf("%d hours", f.timeboxHours)
			if f.timeboxHours == 1 {
				hours = "1 hour"
			}
		}
		b.WriteString(label(fieldTimeboxDays, "Timebox days") + "  " + choice(fieldTimeboxDays, days) + "\n")
		b.WriteString(label(fieldTimeboxHours, "Timebox hours") + "  " + choice(fieldTimeboxHours, hours) + "\n")
	}
	b.WriteString("\n" + label(fieldDescription, "Description") + "\n" + f.description.View() + "\n" + f.mode() + "\n")
	return b.String()
}

func (f *formView) help() string {
	if !vimKeys {
		return "tab next field · ←/→ choose · ctrl+s save · esc cancel"
	}
	return "esc normal mode · tab or j/k field · i edit · h/l choose · :w save · :q cancel · ctrl+s save"
}
func (f *formView) typing() bool { return true }

// labelsView picks which labels the backlog shows (any of them); space toggles, enter applies.
type labelsView struct {
	list   *listView
	chosen map[string]bool
	cursor int
}

func (v *labelsView) load(a *app) tea.Cmd {
	a.loading = false
	if v.chosen == nil {
		v.chosen = map[string]bool{}
		for _, l := range v.list.labels {
			v.chosen[l] = true
		}
	}
	return nil
}

func (v *labelsView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	k, isKey := msg.(tea.KeyMsg)
	if !isKey {
		return v, nil
	}
	labels := v.list.inUse
	if cursorKeys(k.String(), &v.cursor, len(labels)) {
		return v, nil
	}
	switch k.String() {
	case " ", "x":
		if len(labels) > 0 {
			l := labels[v.cursor].Label
			v.chosen[l] = !v.chosen[l]
		}
	case "c":
		v.chosen = map[string]bool{}
	case "enter":
		var picked []string
		for _, l := range labels {
			if v.chosen[l.Label] {
				picked = append(picked, l.Label)
			}
		}
		v.list.labels, v.list.cursor = picked, 0
		return v, msgOf(popMsg{})
	}
	return v, nil
}

func (v *labelsView) render(a *app) string {
	if len(v.list.inUse) == 0 {
		return accent.Render("Labels") + "\n\n" + muted.Render("No labels in this backlog.") + "\n"
	}
	rows := make([]string, len(v.list.inUse))
	for i, l := range v.list.inUse {
		box := "[ ]"
		if v.chosen[l.Label] {
			box = "[x]"
		}
		rows[i] = fmt.Sprintf("%s %s %s", box, l.Label, muted.Render(fmt.Sprint(l.Count)))
	}
	return accent.Render("Show tickets with any of") + "\n\n" + lines(rows, v.cursor, a.rows()-2)
}

func (v *labelsView) help() string {
	return "j/k move · space toggle · c clear · enter apply · esc cancel"
}
func (v *labelsView) typing() bool { return false }
