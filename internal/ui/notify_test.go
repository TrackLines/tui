package ui

import (
	"strings"
	"testing"

	"github.com/tracklines/tui/internal/desktop"
)

// told remembers what went to the desktop.
type told struct{ got []string }

func (d *told) Notify(summary, body string, urgent bool) {
	if urgent {
		body += " (urgent)"
	}
	d.got = append(d.got, summary+": "+body)
}

// withNotices starts the TUI with these in the bell and a desktop that remembers.
func withNotices(t *testing.T, notices ...map[string]any) (*app, *[]string, *told) {
	t.Helper()
	fakeNotices = notices
	d := &told{}
	was := newDesktop
	newDesktop = func() desktop.Notifier { return d }
	t.Cleanup(func() { fakeNotices, newDesktop = nil, was })
	a, calls := start(t)
	return a, calls, d
}

// A new one goes to the desktop and the header counts it; N lists them, enter reads and opens its
// ticket, x clears one, a reads them all (TL-886).
func TestNotifications(t *testing.T) {
	old := map[string]any{"id": "n2", "kind": "mention", "text": "mentioned you on TL-1", "actor": "user_2", "ticket_id": "t1", "created_at": "2026-01-01T10:00:00Z", "read_at": "2026-01-01T11:00:00Z"}
	a, calls, d := withNotices(t, old)
	// one arrives: the next look finds it
	fakeNotices = []map[string]any{{"id": "n1", "kind": "assigned", "text": "assigned you TL-1: Fix login", "actor": "user_2", "ticket_id": "t1", "created_at": "2099-01-01T10:00:00Z"}, old}
	pump(a, a.pollNotices(), 0)
	if len(d.got) != 1 || d.got[0] != "TrackLin.es: Alex Roe assigned you TL-1: Fix login" {
		t.Fatalf("desktop: %v", d.got) // only the new one; the read one was there before
	}
	expect(t, a, "N 1 unread")
	press(a, "N")
	expect(t, a, "Notifications", "● Alex Roe assigned you TL-1: Fix login", "Alex Roe mentioned you on TL-1")
	press(a, "enter")
	if find(*calls, "POST /api/notifications/n1/read") == "" {
		t.Fatalf("not read: %v", *calls)
	}
	expect(t, a, "Fix login", "Login fails")
	press(a, "esc", "j", "x")
	if find(*calls, "DELETE /api/notifications/n2") == "" {
		t.Fatalf("not cleared: %v", *calls)
	}
	press(a, "a")
	if find(*calls, "POST /api/notifications/read-all") == "" {
		t.Fatalf("not all read: %v", *calls)
	}
}

// An urgent one no client has opened them for opens the screen, once (shown); one that's old isn't
// sent to the desktop, and one about no ticket says where it is on the website.
func TestUrgentNoticeOpensNotifications(t *testing.T) {
	a, calls, d := withNotices(t, map[string]any{"id": "o1", "kind": "billing", "urgent": true, "actor": "TrackLin.es",
		"text": "is giving your organization a free year of Pro.", "link": "/settings?offer=1", "created_at": "2026-01-01T10:00:00Z"})
	if find(*calls, "POST /api/notifications/o1/shown") == "" {
		t.Fatalf("not recorded as shown: %v", *calls)
	}
	if _, open := a.top().(*notificationsView); !open {
		t.Fatalf("not opened:\n%s", a.View())
	}
	if len(d.got) != 0 {
		t.Fatalf("an old one went to the desktop: %v", d.got)
	}
	press(a, "enter")
	expect(t, a, "Open it on the website:", "/settings?offer=1")

	// shown already (here or another client): it doesn't open again
	a, calls, _ = withNotices(t, map[string]any{"id": "o1", "kind": "billing", "urgent": true, "text": "x", "created_at": "2026-01-01T10:00:00Z", "shown_at": "2026-01-01T10:01:00Z"})
	if _, open := a.top().(*notificationsView); open || find(*calls, "POST /api/notifications/o1/shown") != "" {
		t.Fatalf("opened again: %v", *calls)
	}
}

// Not over what you're typing: it waits for the next look.
func TestUrgentNoticeWaitsWhileTyping(t *testing.T) {
	a, calls, _ := withNotices(t)
	press(a, "/") // the search box takes text
	fakeNotices = []map[string]any{{"id": "o1", "kind": "billing", "urgent": true, "text": "x", "created_at": "2099-01-01T10:00:00Z"}}
	pump(a, a.pollNotices(), 0)
	if _, typing := a.top().(*searchView); !typing || find(*calls, "POST /api/notifications/o1/shown") != "" {
		t.Fatalf("opened over typing: %v", *calls)
	}
	press(a, "esc")
	pump(a, a.pollNotices(), 0)
	if _, open := a.top().(*notificationsView); !open {
		t.Fatalf("not opened after:\n%s", a.View())
	}
}

func TestFreshNoticesAreTheLatestThreeUnread(t *testing.T) {
	read := "2026-01-01T00:00:00Z"
	list := []Notice{{ID: "a", CreatedAt: "2026-01-05T00:00:00Z"}, {ID: "b", CreatedAt: "2026-01-02T00:00:00Z"}, {ID: "c", CreatedAt: "2026-01-04T00:00:00Z"},
		{ID: "d", CreatedAt: "2026-01-03T00:00:00Z"}, {ID: "e", CreatedAt: "2026-01-06T00:00:00Z", ReadAt: &read}, {ID: "f", CreatedAt: "2025-12-31T00:00:00Z"}}
	var ids []string
	for _, n := range fresh(list, "2026-01-01T00:00:00Z") {
		ids = append(ids, n.ID)
	}
	if strings.Join(ids, "") != "dca" {
		t.Fatalf("fresh: %v", ids)
	}
}
