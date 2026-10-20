package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	peopleMsg struct {
		people map[string]string
		me     string
	}
)

var tabs = []string{"My work", "Projects"}

func newApp(api *Client) *app { return &app{api: api, stack: []view{tabView(0)}} }

func tabView(i int) view {
	if i == 1 {
		return &projectsView{}
	}
	return newHome()
}

func (a *app) top() view { return a.stack[len(a.stack)-1] }

func (a *app) Init() tea.Cmd {
	a.loading = true
	return tea.Batch(a.top().load(a), func() tea.Msg {
		p, me, _ := a.api.People(context.Background()) // ponytail: without names, comments show ids; not worth an error
		return peopleMsg{p, me}
	})
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
	case pushMsg:
		a.stack = append(a.stack, msg.v)
		a.status, a.loading = "", true
		return a, msg.v.load(a)
	case replaceMsg:
		a.stack[len(a.stack)-1] = msg.v
		a.loading = true
		return a, msg.v.load(a)
	case popMsg:
		if len(a.stack) > 1 {
			a.stack = a.stack[:len(a.stack)-1]
		}
		a.status, a.loading = msg.status, true
		return a, a.top().load(a)
	case loadedMsg:
		a.loading = false
		if msg.err != nil {
			a.status = warn.Render(msg.err.Error())
			return a, nil
		}
	case statusMsg:
		if msg.err != nil {
			a.loading, a.status = false, warn.Render(msg.err.Error())
			return a, nil
		}
		a.status, a.loading = ok.Render(msg.text), true
		return a, a.top().load(a) // show it as it is now
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}
		if !a.top().typing() {
			switch k := msg.String(); k {
			case "q": // like lazygit: back out of a screen, and quit only from the top one
				if len(a.stack) > 1 {
					return a, msgOf(popMsg{})
				}
				return a, tea.Quit
			case "Q": // quit from anywhere
				return a, tea.Quit
			case "1", "2":
				a.tab = int(k[0] - '1')
				a.stack = []view{tabView(a.tab)}
				a.status, a.loading = "", true
				return a, a.top().load(a)
			case "/":
				return a, msgOf(pushMsg{newSearch()})
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
	for i, t := range tabs {
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
	return strings.Join(head, " ") + "\n\n" + a.top().render(a) + "\n" + foot
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
