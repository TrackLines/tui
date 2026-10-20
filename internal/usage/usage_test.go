package usage

import (
	"testing"
	"time"
)

func TestRanking(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	u := Projects{}
	day := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		u.Opened("busy", "Busy", day)
	}
	u.Opened("old", "Old", day)
	u.Opened("new", "New", day.Add(time.Hour)) // one visit each: the latest first
	u.Opened("fourth", "Fourth", day.Add(-time.Hour))
	if got := u.Top(3); len(got) != 3 || got[0].ID != "busy" || got[1].ID != "new" || got[2].ID != "old" {
		t.Fatalf("top 3: %+v", got)
	}
	if got := Load(); got["busy"].Count != 3 { // saved as it goes
		t.Fatalf("saved: %+v", got)
	}
	u.Prune(map[string]bool{"busy": true, "old": true}) // the others are gone
	if got := Load().Top(5); len(got) != 2 || got[0].ID != "busy" {
		t.Fatalf("after pruning: %+v", got)
	}
}
