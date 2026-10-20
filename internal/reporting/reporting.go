// Package reporting sends the TUI's own failures (the API failing or unreachable, a crash) to Bugfixes.
package reporting

import (
	"os"
	"path/filepath"

	bugfixes "github.com/bugfixes/go-bugfixes"
	"github.com/bugfixes/go-bugfixes/logs"
	"github.com/tracklines/tui/internal/config"
)

// The Bugfixes agent every copy of the TUI reports to, so errors arrive from whoever runs it,
// however they built it. ponytail: it ships in the binary like any client-side error reporter's key,
// so anyone can read it and send to this agent; it only takes reports, and this agent is the TUI's
// alone. A build can swap them with -ldflags -X. Not read from the environment, where a developer's
// shell may hold another service's agent (the backend's). TRACKLINES_NO_REPORTS=1 turns reporting off.
var (
	bugfixesKey    = "aa40e763-1957-457b-bd38-1066cccb34ca"
	bugfixesSecret = "0a10b3bd-93d9-4302-981d-24a59e235336"
)

var on bool

// Setup sends errors to Bugfixes as "tracklines-tui", release version. The SDK also writes each one to
// stderr, which would draw over the screen, so stderr goes to ~/.local/state/tracklines/tracklines.log
// while it runs; the returned func puts it back.
func Setup(version string) (restore func()) {
	if bugfixesKey == "" || bugfixesSecret == "" || os.Getenv("TRACKLINES_NO_REPORTS") != "" {
		return func() {}
	}
	bugfixes.SetDefaultConfig(bugfixes.Config{
		Server:      bugfixes.DefaultServer,
		AgentKey:    bugfixesKey,
		AgentSecret: bugfixesSecret,
		Service:     "tracklines-tui",
		Release:     version,
	})
	on = true
	stderr := os.Stderr
	if f, err := logFile(); err == nil {
		os.Stderr = f
		return func() { os.Stderr = stderr; _ = f.Close() }
	}
	if f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil { // nowhere to keep it: drop the local copy
		os.Stderr = f
		return func() { os.Stderr = stderr; _ = f.Close() }
	}
	return func() {}
}

func logFile() (*os.File, error) {
	dir, err := config.StateDir()
	if err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "tracklines.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// Report sends something that went wrong on our side to Bugfixes. Never the API key, and never what
// someone searched for or wrote: callers pass routes, not queries.
func Report(format string, args ...any) {
	if on {
		_ = logs.Errorf(format, args...)
	}
}

// APIFailure is api.Client's OnFailure: "api GET /search: <cause>".
func APIFailure(method, route string, err error) { Report("api %s %s: %v", method, route, err) }
