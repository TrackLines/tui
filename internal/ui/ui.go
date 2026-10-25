package ui

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tracklines/tui/internal/drafts"
	"github.com/tracklines/tui/internal/update"
	"github.com/tracklines/tui/internal/usage"
)

// Colors are the terminal's own ANSI ones, so the TUI wears whatever Omarchy theme is active.
var (
	accent  = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
	bold    = lipgloss.NewStyle().Bold(true)
	muted   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	warn    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	ok      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	picked  = lipgloss.NewStyle().Reverse(true)
	urgency = map[string]lipgloss.Style{"urgent": warn.Bold(true), "high": lipgloss.NewStyle().Foreground(lipgloss.Color("3")), "low": muted}
)

// view is one screen: it loads what it shows, handles its keys and draws itself. The app keeps a
// stack of them; esc goes back down it, reloading the screen underneath.
type view interface {
	load(a *app) tea.Cmd
	update(a *app, msg tea.Msg) (view, tea.Cmd)
	render(a *app) string
	help() string
	typing() bool // taking text, so 1/2/3, / and q are letters, not navigation
}

// app is the whole TUI: tabs (My work, Mentions, Projects) each starting a stack of screens.
type app struct {
	api           *Client
	stack         []view
	tab           int
	width, height int
	status        string
	loading       bool
	people        map[string]string
	me            string
	latest        *update.Release // a newer release than this one, when there is
	updated       string          // the release just installed: restart to use it
	usage         usage.Projects
	drafts        drafts.Store      // comments and edits that didn't reach the server (TL-599)
	favourites    []usage.Favourite // tabs 3–5: the projects you open most (TL-477)
}

type (
	pushMsg    struct{ v view }
	replaceMsg struct{ v view }
	popMsg     struct{ status string } // back, saying what happened
	// loadedMsg is a screen's data arriving; tag says whose, so a screen that's been left ignores it
	loadedMsg struct {
		tag  string
		data any
		err  error
	}
	statusMsg struct {
		text string
		err  error
	}
	keptMsg struct{ d drafts.Draft } // a send failed; keep its draft
	sentMsg struct {                 // a draft's change reached the server
		kind, ticket, status string
		pop                  bool // leave the screen it was sent from
	}
	peopleMsg struct {
		people map[string]string
		me     string
	}
)

func newApp(api *Client) *app {
	a := &app{api: api, usage: usage.Load(), drafts: drafts.Load()}
	a.favourites = a.usage.Top(3)
	if n := len(a.drafts); n > 0 {
		word := "drafts"
		if n == 1 {
			word = "draft"
		}
		a.status = warn.Render(fmt.Sprintf("%d unsent %s", n, word)) + muted.Render(" · D to see them")
	}
	a.stack = []view{a.tabView(0)}
	return a
}

// tabs are My work, Projects, and then your most used projects (TL-477).
func (a *app) tabs() []string {
	out := []string{"My work", "Projects"}
	for _, f := range a.favourites {
		out = append(out, cut(f.Name, 18))
	}
	return out
}

func (a *app) tabView(i int) view {
	switch {
	case i == 1:
		return &projectsView{}
	case i >= 2:
		f := a.favourites[i-2]
		return &projectView{id: f.ID}
	}
	return newHome()
}

func (a *app) top() view { return a.stack[len(a.stack)-1] }

// closer is a screen that has something to tidy up when it's left (following a session: leave it).
type closer interface{ close(a *app) tea.Cmd }

// leaving runs the close of each screen about to go (the top n).
func (a *app) leaving(n int) tea.Cmd {
	var cmds []tea.Cmd
	for _, v := range a.stack[max(0, len(a.stack)-n):] {
		if c, isCloser := v.(closer); isCloser {
			cmds = append(cmds, c.close(a))
		}
	}
	return tea.Batch(cmds...)
}

type latestMsg update.Release

func (a *app) Init() tea.Cmd {
	a.loading = true
	return tea.Batch(a.top().load(a), a.checkUpdate(), func() tea.Msg {
		p, me, _ := a.api.People(context.Background()) // ponytail: without names, comments show ids; not worth an error
		return peopleMsg{p, me}
	})
}

// checkUpdate looks for a newer release, quietly: no network, no answer, nothing shown.
func (a *app) checkUpdate() tea.Cmd {
	if !update.IsRelease(version) || !updateCheck {
		return nil
	}
	return func() tea.Msg {
		r, err := update.Latest(context.Background(), http.DefaultClient)
		if err != nil || !update.Newer(r.Tag, version) {
			return nil
		}
		return latestMsg(r)
	}
}

// fetch runs f off the UI and hands its result back tagged for the screen that asked.
func fetch(tag string, f func(context.Context) (any, error)) tea.Cmd {
	return func() tea.Msg {
		data, err := f(context.Background())
		return loadedMsg{tag, data, err}
	}
}

// act runs a change and says how it went; the screen reloads after.
func act(done string, f func(context.Context) error) tea.Cmd {
	return func() tea.Msg { return statusMsg{done, f(context.Background())} }
}

func msgOf(m tea.Msg) tea.Cmd { return func() tea.Msg { return m } }

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
	case peopleMsg:
		a.people, a.me = msg.people, msg.me
		return a, nil
	case latestMsg:
		r := update.Release(msg)
		a.latest = &r
		return a, nil
	case pushMsg:
		a.stack = append(a.stack, msg.v)
		a.status, a.loading = "", true
		return a, msg.v.load(a)
	case replaceMsg:
		a.stack[len(a.stack)-1] = msg.v
		a.loading = true
		return a, msg.v.load(a)
	case popMsg:
		var bye tea.Cmd
		if len(a.stack) > 1 {
			bye = a.leaving(1)
			a.stack = a.stack[:len(a.stack)-1]
		}
		a.status, a.loading = msg.status, true
		return a, tea.Batch(bye, a.top().load(a))
	case loadedMsg:
		a.loading = false
		if msg.err != nil {
			a.status = warn.Render(msg.err.Error())
			if f, isFollow := a.top().(*followView); isFollow && f.tag() == msg.tag {
				return a, msgOf(popMsg{a.status}) // the session ended (or went): back, saying so
			}
			return a, nil
		}
	case keptMsg: // a send failed: the text is kept, to send again when asked
		a.drafts.Put(msg.d)
		a.loading = false
		a.status = warn.Render(msg.d.Error) + muted.Render(" · kept as a draft: D to see drafts")
		if msg.d.Conflict {
			a.status = warn.Render(msg.d.Key + " changed since your edit: open it and e to compare, nothing was overwritten")
		}
	case sentMsg:
		a.drafts.Remove(msg.kind, msg.ticket)
		if msg.pop {
			return a, msgOf(popMsg{ok.Render(msg.status)})
		}
		a.loading, a.status = false, ok.Render(msg.status)
	case statusMsg:
		if msg.err != nil {
			a.loading, a.status = false, warn.Render(msg.err.Error())
			return a, nil
		}
		a.status, a.loading = ok.Render(msg.text), true
		return a, a.top().load(a) // show it as it is now
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a, a.quit()
		}
		if !a.top().typing() {
			switch k := msg.String(); k {
			case "q": // like lazygit: back out of a screen, and quit only from the top one
				if len(a.stack) > 1 {
					return a, msgOf(popMsg{})
				}
				return a, tea.Quit
			case "Q": // quit from anywhere
				return a, a.quit()
			case "1", "2", "3", "4", "5":
				i := int(k[0] - '1')
				if i >= len(a.tabs()) {
					break
				}
				bye := a.leaving(len(a.stack))
				a.tab = i
				if i < 2 { // the project tabs reorder here, never while you're on one of them
					a.favourites = a.usage.Top(3)
				}
				a.stack = []view{a.tabView(a.tab)}
				a.status, a.loading = "", true
				return a, tea.Batch(bye, a.top().load(a))
			case "/":
				q := ""
				if l, isList := a.top().(*listView); isList {
					q = l.query
				}
				return a, msgOf(pushMsg{newSearch(q)})
			case "?":
				if _, already := a.top().(*aboutView); !already {
					return a, msgOf(pushMsg{&aboutView{}})
				}
			case "D": // unsent comments and edits
				if _, already := a.top().(*draftsView); !already {
					return a, msgOf(pushMsg{&draftsView{sending: map[string]bool{}}})
				}
			case "t": // go to a ticket by its key
				return a, msgOf(pushMsg{newGoto()})
			case "esc", "backspace":
				if len(a.stack) > 1 {
					return a, msgOf(popMsg{})
				}
			}
		}
	}
	v, cmd := a.top().update(a, msg)
	a.stack[len(a.stack)-1] = v
	return a, cmd
}

func (a *app) View() string {
	var head []string
	for i, t := range a.tabs() {
		label := fmt.Sprintf(" %d %s ", i+1, t)
		if i == a.tab {
			label = picked.Render(label)
		}
		head = append(head, label)
	}
	foot := muted.Render(a.top().help())
	if a.loading {
		foot = muted.Render("Loading…  ") + foot
	} else if a.status != "" {
		foot = a.status + "\n" + foot
	}
	if a.latest != nil {
		head = append(head, ok.Render(" "+a.latest.Tag+" is out · ? to update"))
	}
	body := strings.Join(head, " ") + "\n\n" + strings.TrimRight(a.top().render(a), "\n")
	// the footer sits on the window's last lines, not wherever the screen happens to end (TL-523)
	gap := max(1, a.height-lipgloss.Height(body)-lipgloss.Height(foot)+1)
	return body + strings.Repeat("\n", gap) + foot
}

// quit leaves any session being followed first (briefly: quitting mustn't hang), then quits.
func (a *app) quit() tea.Cmd {
	for _, v := range a.stack {
		if f, isFollow := v.(*followView); isFollow {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = a.api.Leave(ctx, f.kind, f.id)
			cancel()
		}
	}
	return tea.Quit
}

// rows is how many lines a screen's body can use.
func (a *app) rows() int { return max(3, a.height-6) }

// name is a person's name, or what was recorded (an agent's key name) when we don't know them.
func (a *app) name(id string) string {
	if n, ok := a.people[id]; ok {
		return n
	}
	return id
}

func priority(p string) string {
	if s, ok := urgency[p]; ok {
		return s.Render(p)
	}
	return p
}

// timeboxLabel renders a spike's timebox as "1d 4h" (TL-502); "" when it isn't set.
func timeboxLabel(days, hours *int) string {
	var parts []string
	if days != nil && *days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", *days))
	}
	if hours != nil && *hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", *hours))
	}
	return strings.Join(parts, " ")
}

// Business-hours timebox tracking (TL-506): same spec as the web helper (lib/timebox.ts). Mon–Fri
// 09:00–17:00 in the local timezone, 8h per business day. ponytail: hard-coded; TL-510 makes it a setting.
const (
	workStartHour   = 9
	workEndHour     = 17
	workHoursPerDay = workEndHour - workStartHour // 8
)

func intp(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// businessMinutesBetween: minutes of [start, end] inside Mon–Fri 09:00–17:00 (in start's location).
func businessMinutesBetween(start, end time.Time) int {
	if !end.After(start) {
		return 0
	}
	total := 0.0
	day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	for i := 0; i < 4000 && !day.After(end); i++ {
		if wd := day.Weekday(); wd >= time.Monday && wd <= time.Friday {
			ws := time.Date(day.Year(), day.Month(), day.Day(), workStartHour, 0, 0, 0, day.Location())
			we := time.Date(day.Year(), day.Month(), day.Day(), workEndHour, 0, 0, 0, day.Location())
			os, oe := ws, we
			if start.After(os) {
				os = start
			}
			if end.Before(oe) {
				oe = end
			}
			if oe.After(os) {
				total += oe.Sub(os).Minutes()
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return int(total + 0.5)
}

// timeboxMeta renders a spike's timebox for the detail meta line: "timebox 1d 4h", plus the live
// business-hours state once In Progress ("· 3h left" / "· over by 1h" / "· within, 2h spare").
func timeboxMeta(t *Ticket, now time.Time) string {
	label := timeboxLabel(t.TimeboxDays, t.TimeboxHours)
	if label == "" {
		return ""
	}
	out := "timebox " + label
	if t.StartedAt == nil { // not In Progress yet
		return out
	}
	start, err := time.Parse(time.RFC3339, *t.StartedAt)
	if err != nil {
		return out
	}
	end := now
	if t.DoneAt != nil {
		if d, err := time.Parse(time.RFC3339, *t.DoneAt); err == nil {
			end = d
		}
	}
	budget := (intp(t.TimeboxDays)*workHoursPerDay + intp(t.TimeboxHours)) * 60
	elapsed := businessMinutesBetween(start.Local(), end.Local())
	remaining := budget - elapsed
	switch {
	case remaining < 0:
		return out + " · over by " + formatDuration(-remaining)
	case t.DoneAt != nil:
		return out + " · within, " + formatDuration(remaining) + " spare"
	default:
		return out + " · " + formatDuration(remaining) + " left"
	}
}

// formatDuration: business minutes as "2d 1h" / "3h" / "45m" (a day = 8 business hours).
func formatDuration(mins int) string {
	if mins < 0 {
		mins = 0
	}
	d := mins / 60 / workHoursPerDay
	h := (mins / 60) % workHoursPerDay
	m := mins % 60
	var parts []string
	if d > 0 {
		parts = append(parts, fmt.Sprintf("%dd", d))
	}
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%dh", h))
	}
	if m > 0 && d == 0 {
		parts = append(parts, fmt.Sprintf("%dm", m))
	}
	if len(parts) == 0 {
		return "0m"
	}
	return strings.Join(parts, " ")
}

// where is "Board · Column", or "Backlog".
func where(t Ticket) string {
	if t.BoardName == nil {
		return "Backlog"
	}
	if t.ColumnName == nil {
		return *t.BoardName
	}
	return *t.BoardName + " · " + *t.ColumnName
}

// day is "2026-10-20" from an RFC 3339 time.
func day(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

func wrap(s string, width int) string {
	if width <= 4 {
		return s
	}
	return lipgloss.NewStyle().Width(width - 2).Render(s)
}

// cut shortens s to n cells, with an ellipsis.
func cut(s string, n int) string {
	if n <= 1 || lipgloss.Width(s) <= n {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)) > n-1 {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// offset is the first row shown, so the cursor stays on screen in a list taller than rows.
func offset(n, cursor, rows int) int {
	if rows < 1 || n <= rows {
		return 0
	}
	return min(max(cursor-rows+1, 0), n-rows)
}

// step moves a cursor by d within n items.
func step(cursor, d, n int) int { return min(max(cursor+d, 0), max(n-1, 0)) }

// cursorKeys moves a list cursor with vim keys; true when the key was one of them.
func cursorKeys(k string, cursor *int, n int) bool {
	switch k {
	case "j", "down":
		*cursor = step(*cursor, 1, n)
	case "k", "up":
		*cursor = step(*cursor, -1, n)
	case "g", "home":
		*cursor = 0
	case "G", "end":
		*cursor = max(n-1, 0)
	default:
		return false
	}
	return true
}

// lines draws a list of rows with the cursor's highlighted, scrolled to keep it in view.
func lines(rows []string, cursor, height int) string {
	o := offset(len(rows), cursor, height)
	end := min(len(rows), o+height)
	var b strings.Builder
	for i := o; i < end; i++ {
		if i == cursor {
			b.WriteString(picked.Render(rows[i]) + "\n")
		} else {
			b.WriteString(rows[i] + "\n")
		}
	}
	return b.String()
}
