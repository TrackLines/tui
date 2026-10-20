package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	os.WriteFile(path, []byte("# my settings\ncolour = blue\nkeys = standard  # no modes\n"), 0o600)
	if Load(path).VimKeys {
		t.Fatal("keys = standard not read")
	}
	os.WriteFile(path, []byte("keys=vim\n"), 0o600)
	if !Load(path).VimKeys {
		t.Fatal("keys=vim not read")
	}
	os.WriteFile(path, []byte("backlog_sort = newest\n"), 0o600)
	if s := Load(path); s.BacklogSort != "newest" || !s.VimKeys {
		t.Fatalf("backlog_sort = newest: %+v", s)
	}
	os.WriteFile(path, []byte("backlog_sort = sideways\n"), 0o600) // not a sort: the default stays
	if Load(path).BacklogSort != "manual" {
		t.Fatal("an unknown sort was taken")
	}
	if Load(filepath.Join(t.TempDir(), "missing")) != Defaults() {
		t.Fatal("a missing config isn't the defaults")
	}
}

// config.example, the default config in the repo, loads to the defaults and writes out every setting:
// it stays in step with the code.
func TestConfigExample(t *testing.T) {
	const example = "../../config.example"
	if Load(example) != Defaults() {
		t.Fatalf("config.example isn't the defaults: %+v", Load(example))
	}
	b, err := os.ReadFile(example)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"keys", "backlog_sort"} { // every setting Load knows
		if !strings.Contains(string(b), "\n"+name+" = ") {
			t.Fatalf("config.example lacks %s", name)
		}
	}
}

func TestAPIKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TRACKLINES_API_KEY", "")
	if _, err := APIKey(); err == nil {
		t.Fatal("no key, no error")
	}
	os.MkdirAll(Dir(), 0o700)
	os.WriteFile(filepath.Join(Dir(), "key"), []byte("tl_p_file\n"), 0o600)
	if k, _ := APIKey(); k != "tl_p_file" {
		t.Fatalf("from the file: %q", k)
	}
	t.Setenv("TRACKLINES_API_KEY", "tl_p_env")
	if k, _ := APIKey(); k != "tl_p_env" {
		t.Fatalf("the environment first: %q", k)
	}
}
