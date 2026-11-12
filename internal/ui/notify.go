package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tracklines/tui/internal/desktop"
)

// Notifications (TL-886): the bell's list on its own screen (N), the unread count in the header, and
// each new one on the desktop too (D-Bus on Linux). An urgent one opens the screen, once on any client.

// noticeEvery: how often the TUI asks for new ones; it keeps asking while the terminal isn't focused,
// so the desktop hears about them.
// ponytail: polled, like the website's bell; the live stream if half a minute is too slow
const noticeEvery = 30 * time.Second

const brand = "TrackLin.es"

type (
	noticeTick struct{}
	// noticesMsg is a poll: the unread count, and the list when the count had changed
	noticesMsg struct {
		unread int
		list   []Notice
		listed bool
		err    error
	}
	urgentShownMsg struct{ err error }
)

func noticesLater() tea.Cmd {
	return tea.Tick(noticeEvery, func(time.Time) tea.Msg { return noticeTick{} })
}

// pollNotices asks for the unread count, and for the list only when it changed since the last look.
func (a *app) pollNotices() tea.Cmd {
	seen := a.unreadSeen
	return func() tea.Msg {
		ctx := context.Background()
		n, err := a.api.UnreadCount(ctx)
		if err != nil || n == seen {
			return noticesMsg{unread: n, err: err}
		}
		list, err := a.api.Notifications(ctx)
		return noticesMsg{unread: n, list: list, listed: err == nil, err: err}
	}
}

// fresh are the unread ones that arrived after since, oldest first; a burst gives its latest three.
func fresh(list []Notice, since string) []Notice {
	var out []Notice
	for _, n := range list {
		if n.ReadAt == nil && n.CreatedAt > since {
			out = append(out, n)
		}
	}
	slices.SortFunc(out, func(x, y Notice) int { return strings.Compare(x.CreatedAt, y.CreatedAt) })
	return out[max(0, len(out)-3):]
}

// urgentNotice is the one to open the screen for: urgent, unread, and no client has opened them for it.
func urgentNotice(list []Notice) *Notice {
	for i, n := range list {
		if n.Urgent && n.ReadAt == nil && n.ShownAt == nil {
			return &list[i]
		}
	}
	return nil
}

// notices handles a poll's answer: the count, the desktop, and an urgent one.
func (a *app) notices(msg noticesMsg) tea.Cmd {
	if msg.err != nil {
		return nil // missing a look isn't worth showing; the next one comes soon
	}
	a.unread = msg.unread
	if !msg.listed {
		return nil
	}
	a.unreadSeen = msg.unread
	for _, n := range fresh(msg.list, a.noticesSince) {
		a.desktop.Notify(brand, a.noticeLine(n), n.Urgent)
		a.noticesSince = max(a.noticesSince, n.CreatedAt)
	}
	u := urgentNotice(msg.list)
	if u == nil {
		return nil
	}
	if a.top().typing() {
		a.unreadSeen = -1 // not over what you're writing: look again next time
		return nil
	}
	id := u.ID
	return func() tea.Msg { return urgentShownMsg{a.api.NotificationShown(context.Background(), id)} }
}

// noticeLine is how one reads, as on the website: "Sam assigned you TL-12: Fix login".
func (a *app) noticeLine(n Notice) string {
	switch {
	case n.Kind == "legal" || n.Kind == "billing": // from TrackLin.es itself
		return n.Text
	case n.Kind == "due":
		return F("Reminder: %s", n.Text)
	case n.Kind == "downtime_credit":
		return brand + " " + n.Text
	case n.Actor == "":
		return T("Someone") + " " + n.Text
	}
	return a.name(n.Actor) + " " + n.Text
}

// notificationsView is the list: unread first, then newest.
type notificationsView struct {
	items  []Notice
	loaded bool
	cursor int
}

func (v *notificationsView) load(a *app) tea.Cmd {
	return fetch("notifications", func(ctx context.Context) (any, error) { return a.api.Notifications(ctx) })
}

func (v *notificationsView) unread() int {
	n := 0
	for _, x := range v.items {
		if x.ReadAt == nil {
			n++
		}
	}
	return n
}

func (v *notificationsView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		if msg.tag == "notifications" {
			v.items, _ = msg.data.([]Notice)
			v.loaded = true
			v.cursor = min(v.cursor, max(len(v.items)-1, 0))
			// ponytail: the first 50; more unread than that and the header's count is the server's again on the next poll
			a.unread = v.unread()
		}
	case tea.KeyMsg:
		k := msg.String()
		if cursorKeys(k, &v.cursor, len(v.items)) {
			return v, nil
		}
		if k == "r" {
			a.loading = true
			return v, v.load(a)
		}
		if len(v.items) == 0 {
			return v, nil
		}
		n := v.items[v.cursor]
		switch k {
		case "enter":
			return v, v.open(a, n)
		case "x":
			return v, act(T("Cleared."), T("Couldn’t clear it"), func(ctx context.Context) error { return a.api.DismissNotification(ctx, n.ID) })
		case "a":
			return v, act(T("All read."), T("Couldn’t mark them read"), func(ctx context.Context) error { return a.api.ReadAllNotifications(ctx) })
		}
	}
	return v, nil
}

// open marks it read and goes to its ticket or board; one about neither (the launch offer, new Terms)
// says where it is on the website.
func (v *notificationsView) open(a *app, n Notice) tea.Cmd {
	var next view
	switch {
	case n.TicketID != nil:
		next = &ticketView{id: *n.TicketID}
	case n.BoardID != nil:
		next = &boardView{id: *n.BoardID}
	}
	if n.ReadAt == nil {
		now := time.Now().UTC().Format(time.RFC3339)
		v.items[v.cursor].ReadAt = &now
		a.unread = max(0, a.unread-1)
	}
	read := func() tea.Msg {
		if n.ReadAt == nil {
			_ = a.api.ReadNotification(context.Background(), n.ID) // ponytail: a failed read stays unread; nothing to do about it here
		}
		return nil
	}
	if next != nil {
		return tea.Batch(read, msgOf(pushMsg{next}))
	}
	if n.Link != nil {
		a.status = F("Open it on the website: %s", siteURL(a.api.Base)+*n.Link)
	}
	return read
}

func (v *notificationsView) render(a *app) string {
	var b strings.Builder
	b.WriteString(accent.Render(T("Notifications")) + muted.Render(fmt.Sprintf("  %d", v.unread())) + "\n\n")
	if !v.loaded {
		return b.String()
	}
	if len(v.items) == 0 {
		return b.String() + muted.Render(T("You’re all caught up.")) + "\n"
	}
	var rows []string
	for _, n := range v.items {
		mark, line := "  ", cut(a.noticeLine(n), max(10, a.width-22))
		if n.ReadAt == nil {
			mark = "● "
			if n.Urgent {
				line = warn.Render(line)
			}
		} else {
			line = muted.Render(line)
		}
		rows = append(rows, mark+line+"  "+muted.Render(when(n.CreatedAt)))
	}
	b.WriteString(lines(rows, v.cursor, max(1, a.rows()-2)))
	return b.String()
}

func (v *notificationsView) help() string {
	return T("j/k move · enter open · x clear · a mark all read · r reload · esc/q back")
}
func (v *notificationsView) typing() bool { return false }

// newDesktop is the desktop's notifier; tests swap it for one that remembers.
var newDesktop = desktop.New
