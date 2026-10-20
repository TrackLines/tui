// Package config is what the TUI reads from your machine: the API key, the config file, and where it
// keeps its state.
package config

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Settings is the config file's: ~/.config/tracklines/config (config.example in the repo).
type Settings struct {
	// VimKeys: writing tickets and comments works like vim (modes, :w, :q), the default; keys =
	// standard turns that off: no modes, ctrl+s saves, esc cancels (TL-475).
	VimKeys bool
	// BacklogSort is the order a backlog opens in: manual (its own order, the default), newest or
	// oldest; s still cycles from there (TL-481).
	BacklogSort string
}

// Defaults is what you get with no config file.
func Defaults() Settings { return Settings{VimKeys: true, BacklogSort: "manual"} }

// Dir is $XDG_CONFIG_HOME/tracklines (~/.config): the key and the config.
func Dir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "tracklines")
}

// Path is the config file.
func Path() string { return filepath.Join(Dir(), "config") }

// Load reads the config at path over the defaults: one "name = value" per line, # for comments. A
// missing file is the defaults; an unknown name or value is ignored, so an old binary reads a newer config.
func Load(path string) Settings {
	s := Defaults()
	f, err := os.Open(path)
	if err != nil {
		return s
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(name) {
		case "keys":
			switch strings.TrimSpace(value) {
			case "vim":
				s.VimKeys = true
			case "standard":
				s.VimKeys = false
			}
		case "backlog_sort":
			if v := strings.TrimSpace(value); v == "manual" || v == "newest" || v == "oldest" {
				s.BacklogSort = v
			}
		}
	}
	return s
}

// APIKey is TRACKLINES_API_KEY, or the first line of the key file (~/.config/tracklines/key).
func APIKey() (string, error) {
	if k := strings.TrimSpace(os.Getenv("TRACKLINES_API_KEY")); k != "" {
		return k, nil
	}
	b, err := os.ReadFile(filepath.Join(Dir(), "key"))
	if err != nil {
		return "", err
	}
	k, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	if k == "" {
		return "", errors.New("empty key file")
	}
	return k, nil
}

// StateDir is $XDG_STATE_HOME/tracklines (~/.local/state): the log, and which projects you use.
func StateDir() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	dir = filepath.Join(dir, "tracklines")
	return dir, os.MkdirAll(dir, 0o700)
}
