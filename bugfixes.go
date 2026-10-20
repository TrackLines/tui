package main

import (
	"os"
	"path/filepath"

	bugfixes "github.com/bugfixes/go-bugfixes"
	"github.com/bugfixes/go-bugfixes/logs"
)

// The Bugfixes agent every copy of the TUI reports to, so errors arrive from whoever runs it,
// however they built it. ponytail: it ships in the binary like any client-side error reporter's key,
// so anyone can read it and send to this agent; it only takes reports, and this agent is the TUI's
// alone. A build can swap them with -ldflags -X. Not read from the environment, where a developer's
// shell may hold another service's agent (the backend's). TRACKLINES_NO_REPORTS=1 turns reporting off.
var (
	version        = "dev"
	bugfixesKey    = "aa40e763-1957-457b-bd38-1066cccb34ca"
	bugfixesSecret = "0a10b3bd-93d9-4302-981d-24a59e235336"
)

var reporting bool

// setupBugfixes sends the TUI's errors to Bugfixes as "tracklines-tui". The SDK also writes each one to
// stderr, which would draw over the screen, so stderr goes to ~/.local/state/tracklines/tracklines.log
// while it runs; the returned func puts it back.
func setupBugfixes() (restore func()) {
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
	reporting = true
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
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	dir = filepath.Join(dir, "tracklines")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "tracklines.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// report sends something that went wrong on our side (the API failing, a crash) to Bugfixes. Never
// the API key, and never what someone searched for or wrote: callers pass paths, not queries.
var report = func(format string, args ...any) {
	if reporting {
		_ = logs.Errorf(format, args...)
	}
}

// reportable is whether an API failure is ours to fix: the server failing or unreachable. Refusals
// (4xx: a read-only key, a locked sprint) are the person's to act on, and they're shown on screen.
func reportable(err error) bool {
	if e, isAPI := err.(*APIError); isAPI {
		return e.Status >= 500
	}
	return err != nil
}
