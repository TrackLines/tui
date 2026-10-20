package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersions(t *testing.T) {
	for _, c := range []struct {
		latest, current string
		newer           bool
	}{
		{"v0.2.0", "v0.1.0", true},
		{"v0.1.1", "v0.1.0", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.10.0", "v0.9.0", true}, // numbers, not text
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"v0.2.0", "v0.1.0-3-gabc123-dirty", true}, // a build after a tag is still that tag
		{"v0.1.0", "dev", false},                   // development builds don't update
		{"nightly", "v0.1.0", false},
	} {
		if got := newer(c.latest, c.current); got != c.newer {
			t.Errorf("newer(%q, %q) = %v", c.latest, c.current, got)
		}
	}
}

// releaseServer is a GitHub release of tag with this machine's binary (body) and SHA256SUMS (sum).
func releaseServer(t *testing.T, tag, body, sum string) release {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
			w.Write([]byte(sum + "  " + binaryName() + "\n" + strings.Repeat("0", 64) + "  tracklines-plan9-mips\n"))
		case strings.HasSuffix(r.URL.Path, "/"+binaryName()):
			w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	r := release{Tag: tag}
	for _, name := range []string{binaryName(), "SHA256SUMS"} {
		r.Assets = append(r.Assets, struct {
			URL string `json:"browser_download_url"`
		}{srv.URL + "/download/" + tag + "/" + name})
	}
	return r
}

func TestSelfUpdate(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tracklines")
	os.WriteFile(exe, []byte("old"), 0o755)
	executable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { executable = os.Executable })

	h := sha256.Sum256([]byte("new binary"))
	good := hex.EncodeToString(h[:])
	// a download that doesn't match its checksum changes nothing
	if err := selfUpdate(t.Context(), http.DefaultClient, releaseServer(t, "v0.2.0", "tampered", good)); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("bad checksum: %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatalf("binary changed by a bad download: %q", b)
	}
	// a good one replaces it, executable, with no temp file left behind
	if err := selfUpdate(t.Context(), http.DefaultClient, releaseServer(t, "v0.2.0", "new binary", good)); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new binary" {
		t.Fatalf("not replaced: %q", b)
	}
	if fi, _ := os.Stat(exe); fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("not executable: %v", fi.Mode())
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".tracklines-update-*")); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
	// a platform with no download (macOS, Windows) is told to build it
	if err := selfUpdate(t.Context(), http.DefaultClient, release{Tag: "v0.2.0"}); err == nil || !strings.Contains(err.Error(), "make install") {
		t.Fatalf("no download: %v", err)
	}
}

func TestAboutAndUpdateNotice(t *testing.T) {
	a, _ := start(t)
	press(a, "?")
	expect(t, a, "TrackLin.es for the terminal", "Version  dev", "A development build: it doesn’t check for updates.", "t        go to a ticket")
	press(a, "esc")
	// a release build that's behind: the header says so, and ? offers it
	r := release{Tag: "v0.2.0"}
	a.latest = &r
	expect(t, a, "v0.2.0 is out · ? to update")
	press(a, "?")
	expect(t, a, "v0.2.0 is out.", "u to update")
}
