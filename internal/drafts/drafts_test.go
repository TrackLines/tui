package drafts

import (
	"testing"
	"time"

	"github.com/tracklines/tui/internal/api"
)

func TestStoreKeepsTheFirstAttempt(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	s := Load()
	first := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	s.Put(Draft{Kind: Comment, TicketID: "t1", Body: "hi", Tried: first, Error: "down"})
	s.Put(Draft{Kind: Comment, TicketID: "t1", Body: "hi", Tried: first.Add(time.Hour), Error: "still down"})
	got := Load().List()
	if len(got) != 1 || !got[0].Tried.Equal(first) || got[0].Error != "still down" {
		t.Fatalf("reloaded: %+v", got)
	}
	s.Remove(Comment, "t1")
	if len(Load()) != 0 {
		t.Fatal("removed draft came back")
	}
}

func TestSent(t *testing.T) {
	d := Draft{Body: "on my way", Tried: time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)}
	c := func(body, at string) []api.Comment { return []api.Comment{{Body: body, CreatedAt: at}} }
	for _, tc := range []struct {
		comments []api.Comment
		want     bool
	}{
		{c("on my way", "2026-10-25T09:00:01Z"), true},  // got through before the reply was lost
		{c("on my way", "2026-10-25T08:57:00Z"), true},  // within the drift allowance
		{c("on my way", "2026-10-20T09:00:00Z"), false}, // the same words, said days ago
		{c("something else", "2026-10-25T09:00:01Z"), false},
		{nil, false},
	} {
		if got := d.Sent(tc.comments); got != tc.want {
			t.Fatalf("%v: %v", tc.comments, got)
		}
	}
}
