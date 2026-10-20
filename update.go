package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Updates come from the GitHub releases CI makes for each v* tag: static Linux binaries for amd64 and
// arm64, and SHA256SUMS. Other platforms build it themselves, so they're told how instead.
var (
	latestURL  = "https://api.github.com/repos/TrackLines/tui/releases/latest"
	executable = os.Executable // the binary to replace (a test's own file in tests)
)

type release struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		URL string `json:"browser_download_url"`
	} `json:"assets"`
}

// asset is the download URL of one of the release's files, or "".
func (r release) asset(name string) string {
	for _, a := range r.Assets {
		if strings.HasSuffix(a.URL, "/"+name) {
			return a.URL
		}
	}
	return ""
}

// binaryName is this machine's file in a release, e.g. tracklines-linux-arm64.
func binaryName() string { return "tracklines-" + runtime.GOOS + "-" + runtime.GOARCH }

// checksUpdates: a release build (vX.Y.Z…) that hasn't been told not to.
func checksUpdates() bool {
	_, isRelease := semver(version)
	return isRelease && os.Getenv("TRACKLINES_NO_UPDATE_CHECK") == ""
}

func latestRelease(ctx context.Context, client *http.Client) (release, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := client.Do(req)
	if err != nil {
		return release{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("checking for updates: GitHub said %s", res.Status)
	}
	var r release
	return r, json.NewDecoder(res.Body).Decode(&r)
}

// semver reads v1.2.3 from the start of a version (git describe adds "-4-gabc123-dirty" after it).
func semver(v string) ([3]int, bool) {
	var out [3]int
	core, _, _ := strings.Cut(strings.TrimPrefix(v, "v"), "-")
	parts := strings.Split(core, ".")
	if !strings.HasPrefix(v, "v") || len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// newer: is latest a later release than current?
func newer(latest, current string) bool {
	l, okL := semver(latest)
	c, okC := semver(current)
	if !okL || !okC {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// selfUpdate replaces the running binary with r's, after checking it against the release's SHA256SUMS.
// The new file goes next to the old one and is renamed over it, so a failed download changes nothing.
func selfUpdate(ctx context.Context, client *http.Client, r release) error {
	name := binaryName()
	bin, sums := r.asset(name), r.asset("SHA256SUMS")
	if bin == "" || sums == "" {
		return fmt.Errorf("there’s no %s download: build it yourself with `make install`", name)
	}
	want, err := checksum(ctx, client, sums, name)
	if err != nil {
		return err
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".tracklines-update-*")
	if err != nil {
		return fmt.Errorf("can’t write next to %s: %w", exe, err)
	}
	defer os.Remove(tmp.Name()) // gone once renamed; cleans up if anything fails
	h := sha256.New()
	if err := download(ctx, client, bin, io.MultiWriter(tmp, h)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("the download doesn’t match its checksum, so nothing was changed")
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), exe)
}

// checksum is name's line in a SHA256SUMS file.
func checksum(ctx context.Context, client *http.Client, url, name string) (string, error) {
	var b strings.Builder
	if err := download(ctx, client, url, &b); err != nil {
		return "", err
	}
	sc := bufio.NewScanner(strings.NewReader(b.String()))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("SHA256SUMS has no %s", name)
}

func download(ctx context.Context, client *http.Client, url string, w io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading the update: %s", res.Status)
	}
	_, err = io.Copy(w, res.Body)
	return err
}

// aboutView is ? : the version you're running, whether there's a newer one (u installs it), and the
// keys that work everywhere.
type aboutView struct{ updating bool }

type updatedMsg struct {
	tag string
	err error
}

func (v *aboutView) load(a *app) tea.Cmd {
	a.loading = false
	return nil
}

func (v *aboutView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case updatedMsg:
		v.updating = false
		if msg.err != nil {
			a.status = warn.Render(msg.err.Error())
			return v, nil
		}
		a.updated, a.latest = msg.tag, nil
		a.status = ok.Render("Updated to " + msg.tag + ". Quit and start tracklines again to use it.")
	case tea.KeyMsg:
		if msg.String() != "u" || a.latest == nil || v.updating {
			return v, nil
		}
		v.updating, a.status = true, muted.Render("Downloading "+a.latest.Tag+"…")
		r := *a.latest
		return v, func() tea.Msg { return updatedMsg{r.Tag, selfUpdate(context.Background(), http.DefaultClient, r)} }
	}
	return v, nil
}

func (v *aboutView) render(a *app) string {
	var b strings.Builder
	b.WriteString(accent.Render("TrackLin.es for the terminal") + "\n\n")
	b.WriteString("Version  " + bold.Render(version) + muted.Render("  ("+runtime.GOOS+"/"+runtime.GOARCH+")") + "\n")
	switch {
	case a.updated != "":
		b.WriteString(ok.Render("Updated to "+a.updated+": restart to use it.") + "\n")
	case a.latest != nil:
		b.WriteString(ok.Render(a.latest.Tag+" is out.") + "  " + bold.Render("u") + " to update\n")
	case !checksUpdates():
		b.WriteString(muted.Render("A development build: it doesn’t check for updates.") + "\n")
	default:
		b.WriteString(muted.Render("Up to date.") + "\n")
	}
	b.WriteString("\n" + bold.Render("Everywhere") + "\n")
	for _, k := range [][2]string{
		{"1 / 2", "My work (and Mentions) / Projects"},
		{"/", "search everything"},
		{"t", "go to a ticket by its key (TL-308)"},
		{"esc / q", "back (q quits on My work and Projects)"},
		{"Q", "quit from anywhere"},
		{"?", "this screen"},
	} {
		b.WriteString(fmt.Sprintf("  %-8s %s\n", k[0], k[1]))
	}
	b.WriteString("\n" + muted.Render("Each screen lists its own keys at the bottom. More: github.com/TrackLines/tui") + "\n")
	return b.String()
}

func (v *aboutView) help() string {
	if v.updating {
		return "updating…"
	}
	return "u update (when there's one) · esc/q back"
}
func (v *aboutView) typing() bool { return false }
