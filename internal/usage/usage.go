// Package usage is which projects you open most, kept on this machine only (TL-477), so the TUI's
// tabs 3–5 can be your top three.
package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tracklines/tui/internal/config"
)

// ponytail: plain counts, ties broken by the latest; weight recent use more if old favourites linger.

type projectUse struct {
	Name  string    `json:"name"`
	Count int       `json:"count"`
	Last  time.Time `json:"last"`
}

type Projects map[string]projectUse // by project id

func usagePath() string {
	dir, err := config.StateDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "projects.json")
}

// Load reads it; missing or unreadable is just no history.
func Load() Projects {
	u := Projects{}
	if b, err := os.ReadFile(usagePath()); err == nil {
		_ = json.Unmarshal(b, &u)
	}
	return u
}

// save writes it whole, through a temp file so a crash never leaves half of it.
func (u Projects) save() {
	path := usagePath()
	if path == "" {
		return
	}
	b, err := json.Marshal(u)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

// Opened counts a visit to the project (and keeps its name current for the tab).
func (u Projects) Opened(id, name string, now time.Time) {
	p := u[id]
	p.Name, p.Count, p.Last = name, p.Count+1, now
	u[id] = p
	u.save()
}

// Prune drops projects no longer there (deleted, or another organization's key): ids is what you see now.
func (u Projects) Prune(ids map[string]bool) {
	changed := false
	for id := range u {
		if !ids[id] {
			delete(u, id)
			changed = true
		}
	}
	if changed {
		u.save()
	}
}

// Favourite is one of your most used projects.
type Favourite struct{ ID, Name string }

// Top is the n projects opened most, the latest first among equals.
func (u Projects) Top(n int) []Favourite {
	ids := make([]string, 0, len(u))
	for id := range u {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := u[ids[i]], u[ids[j]]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Last.After(b.Last)
	})
	out := make([]Favourite, 0, n)
	for _, id := range ids[:min(n, len(ids))] {
		out = append(out, Favourite{id, u[id].Name})
	}
	return out
}
