// Package update checks GitHub for a newer release of the TUI and installs it over the running binary.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/tracklines/tui/internal/i18n"
)

// Updates come from the GitHub releases CI makes for each v* tag: static Linux binaries for amd64 and
// arm64, and SHA256SUMS. Other platforms build it themselves, so they're told how instead.
var (
	latestURL  = "https://api.github.com/repos/TrackLines/tui/releases/latest"
	executable = os.Executable // the binary to replace (a test's own file in tests)
)

type Release struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		URL string `json:"browser_download_url"`
	} `json:"assets"`
}

// asset is the download URL of one of the release's files, or "".
func (r Release) asset(name string) string {
	for _, a := range r.Assets {
		if strings.HasSuffix(a.URL, "/"+name) {
			return a.URL
		}
	}
	return ""
}

// binaryName is this machine's file in a release, e.g. tracklines-linux-arm64.
func binaryName() string { return "tracklines-" + runtime.GOOS + "-" + runtime.GOARCH }

// IsRelease: a release build (vX.Y.Z…), which can check for and install updates; a build of your
// own ("dev") can't. Whether it does is the config's update_check.
func IsRelease(version string) bool {
	_, isRelease := semver(version)
	return isRelease
}

func Latest(ctx context.Context, client *http.Client) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf(i18n.T("checking for updates: GitHub said %s"), res.Status)
	}
	var r Release
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

// Newer: is latest a later release than current?
func Newer(latest, current string) bool {
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

// SelfUpdate replaces the running binary with r's, after checking it against the release's SHA256SUMS.
// The new file goes next to the old one and is renamed over it, so a failed download changes nothing.
func SelfUpdate(ctx context.Context, client *http.Client, r Release) error {
	name := binaryName()
	bin, sums := r.asset(name), r.asset("SHA256SUMS")
	if bin == "" || sums == "" {
		return fmt.Errorf(i18n.T("there’s no %s download: build it yourself with `make install`"), name)
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
		return fmt.Errorf(i18n.T("can’t write next to %s: %w"), exe, err)
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
		return errors.New(i18n.T("the download doesn’t match its checksum, so nothing was changed"))
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
	return "", fmt.Errorf(i18n.T("SHA256SUMS has no %s"), name)
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
		return fmt.Errorf(i18n.T("downloading the update: %s"), res.Status)
	}
	_, err = io.Copy(w, res.Body)
	return err
}
