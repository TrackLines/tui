package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSavedFiltersPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tracklines", "filters.json")
	if views, err := LoadFilters(path); err != nil || len(views) != 0 {
		t.Fatalf("missing: %v %v", views, err)
	}
	views := []SavedFilter{{Name: "My urgent bugs", Source: "mywork", Heading: "My work", Filters: TicketFilters{Type: "bug", Priority: "urgent", Labels: []string{"a", "b"}}}}
	if err := SaveFilters(path, views); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFilters(path)
	if err != nil || !reflect.DeepEqual(got, views) {
		t.Fatalf("round trip: %v %v", got, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("saved views not private")
	}
	if err := SaveFilters(path, append(views, views[0])); err == nil {
		t.Fatal("duplicate accepted")
	}
	got, _ = LoadFilters(path)
	if len(got) != 1 {
		t.Fatal("failed save damaged previous views")
	}
	views[0].Name = "Renamed"
	if err := SaveFilters(path, views); err != nil {
		t.Fatal(err)
	}
	got, _ = LoadFilters(path)
	if got[0].Name != "Renamed" {
		t.Fatal(got)
	}
	if err := SaveFilters(path, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = LoadFilters(path)
	if len(got) != 0 {
		t.Fatal(got)
	}
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFilters(path); err == nil {
		t.Fatal("corruption hidden")
	}
}

func TestSavedFiltersWriteFailure(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveFilters(filepath.Join(file, "filters.json"), nil); err == nil {
		t.Fatal("write failure hidden")
	}
}
