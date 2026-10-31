package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// TicketFilters combine dimensions with AND; labels require every listed label.
// Empty fields leave that dimension unrestricted within the source list.
type TicketFilters struct {
	Project  string   `json:"project,omitempty"`
	Board    string   `json:"board,omitempty"`
	Status   string   `json:"status,omitempty"`
	Type     string   `json:"type,omitempty"`
	Priority string   `json:"priority,omitempty"`
	Assignee string   `json:"assignee,omitempty"`
	Labels   []string `json:"labels,omitempty"`
}

type SavedFilter struct {
	Name          string        `json:"name"`
	Source        string        `json:"source"`
	Heading       string        `json:"heading"`
	Project       string        `json:"project,omitempty"`
	Board         string        `json:"board,omitempty"`
	Query         string        `json:"query,omitempty"`
	Sort          string        `json:"sort,omitempty"`
	BacklogLabels []string      `json:"backlog_labels,omitempty"`
	Filters       TicketFilters `json:"filters"`
}

func FiltersPath() string { return filepath.Join(Dir(), "filters.json") }

func LoadFilters(path string) ([]SavedFilter, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var views []SavedFilter
	if err := json.Unmarshal(b, &views); err != nil {
		return nil, err
	}
	return views, nil
}

// SaveFilters preserves the previous file if writing fails. Never store API credentials here.
func SaveFilters(path string, views []SavedFilter) error {
	names := map[string]bool{}
	for _, v := range views {
		name := strings.TrimSpace(v.Name)
		if name == "" || names[name] {
			return errors.New("saved view names must be nonempty and unique")
		}
		names[name] = true
	}
	b, err := json.MarshalIndent(views, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".filters-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
