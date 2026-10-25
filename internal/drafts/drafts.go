// Package drafts keeps comments and ticket edits that didn't reach the server (TL-599), on this
// machine only (drafts.json in the state directory), so an outage or a restart never loses them.
// They're sent again only when asked: never replayed on their own.
package drafts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tracklines/tui/internal/api"
	"github.com/tracklines/tui/internal/config"
)

const (
	Comment = "comment"
	Edit    = "edit"
)

// Draft is one unsent change: a comment's body, or an edit's fields.
type Draft struct {
	Kind     string           `json:"kind"`
	TicketID string           `json:"ticket_id"`
	Key      string           `json:"key"`
	Title    string           `json:"title"`
	Body     string           `json:"body,omitempty"`  // comment
	Input    *api.TicketInput `json:"input,omitempty"` // edit
	Base     string           `json:"base,omitempty"`  // edit: the ticket's updated_at when editing began
	Tried    time.Time        `json:"tried"`           // the first attempt to send it
	Error    string           `json:"error"`           // why the last attempt failed
	Conflict bool             `json:"conflict,omitempty"`
}

// ID is one draft per kind per ticket: a newer one replaces it.
func (d Draft) ID() string { return d.Kind + ":" + d.TicketID }

type Store map[string]Draft

// Path is where drafts are kept: ~/.local/state/tracklines/drafts.json (or under $XDG_STATE_HOME).
func Path() string {
	dir, err := config.StateDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "drafts.json")
}

// Load reads them; missing or unreadable is none.
func Load() Store {
	s := Store{}
	if b, err := os.ReadFile(Path()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

// Get is the kind's draft for a ticket, if there is one.
func (s Store) Get(kind, ticket string) (Draft, bool) {
	d, ok := s[kind+":"+ticket]
	return d, ok
}

// Put keeps d (keeping when it was first tried) and writes the file.
func (s Store) Put(d Draft) {
	if old, ok := s[d.ID()]; ok && !old.Tried.IsZero() {
		d.Tried = old.Tried
	}
	s[d.ID()] = d
	s.save()
}

// Remove drops a draft: it was sent, or discarded.
func (s Store) Remove(kind, ticket string) {
	if _, ok := s[kind+":"+ticket]; ok {
		delete(s, kind+":"+ticket)
		s.save()
	}
}

// List is the drafts, oldest first.
func (s Store) List() []Draft {
	out := make([]Draft, 0, len(s))
	for _, d := range s {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Tried.Equal(out[j].Tried) {
			return out[i].Tried.Before(out[j].Tried)
		}
		return out[i].ID() < out[j].ID()
	})
	return out
}

// save writes the file whole, through a temp file so a crash never leaves half of it.
func (s Store) save() {
	path := Path()
	if path == "" {
		return
	}
	b, err := json.Marshal(s)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

// Sent is whether comments already holds this draft: the request that "failed" may have got through
// before the connection dropped. A comment with the same text made since the first attempt (less a
// few minutes for clock drift) counts, so a retry never posts it twice.
func (d Draft) Sent(comments []api.Comment) bool {
	since := d.Tried.Add(-5 * time.Minute)
	for _, c := range comments {
		at, err := time.Parse(time.RFC3339, c.CreatedAt)
		if c.Body == d.Body && (err != nil || !at.Before(since)) {
			return true
		}
	}
	return false
}
