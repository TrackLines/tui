package ui

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

// TL-597: each kind of failure says why in a few words, whether a change got through, and what to
// do next; a change whose answer never came is never offered as a plain retry.
func TestExplainFailures(t *testing.T) {
	down := &url.Error{Op: "Post", URL: "https://api.tracklin.es/api/tickets", Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}}
	for _, c := range []struct {
		name         string
		err          error
		write        bool
		reason, next string
	}{
		{"network read", down, false, "can’t reach TrackLin.es", "r to try again"},
		{"network write", down, true, "can’t reach TrackLin.es", "may have saved: refresh (r) to check"},
		{"timeout write", context.DeadlineExceeded, true, "can’t reach TrackLin.es", "may have saved"},
		{"validation", &APIError{Status: 400, Msg: "title is required", Code: "title_required"}, true, "title is required", "not saved"},
		{"unprocessable", &APIError{Status: 422, Msg: "user is not a member of this organization"}, true, "not a member", "not saved"},
		{"permission", &APIError{Status: 403, Msg: "only organization admins can do this"}, true, "only organization admins", "not saved"},
		{"permission, no words", &APIError{Status: 403}, true, "not allowed", "not saved"},
		{"busy write", &APIError{Status: 429, Msg: "slow down"}, true, "too many requests", "not saved: try again"},
		{"unavailable write", &APIError{Status: 503}, true, "had a problem (503)", "not saved: try again"},
		{"server write", &APIError{Status: 500, Msg: "internal error"}, true, "had a problem (500)", "may have saved"},
		{"server read", &APIError{Status: 502}, false, "had a problem (502)", "r to try again"},
		{"ours", errors.New("There’s no ticket TL-9."), false, "There’s no ticket TL-9.", ""},
	} {
		reason, next := explain(c.err, c.write)
		if !strings.Contains(reason, c.reason) || (c.next == "" && next != "") || !strings.Contains(next, c.next) {
			t.Errorf("%s: %q · %q, want %q · %q", c.name, reason, next, c.reason, c.next)
		}
		if c.write && strings.Contains(next, "may have saved") && strings.Contains(next, "try again in a moment") {
			t.Errorf("%s: an unknown outcome offered as a plain retry: %q", c.name, next)
		}
	}
	// what failed, why, and a reference for support: status, code, request id; never the key
	got := failure("Couldn’t move TL-1", &APIError{Status: 403, Msg: "this api key is read-only", Code: "read_only_key", RequestID: "req-7"}, true)
	for _, want := range []string{"Couldn’t move TL-1: this api key is read-only", "not saved", "403 read_only_key req-7"} {
		if !strings.Contains(got, want) {
			t.Errorf("failure lacks %q: %q", want, got)
		}
	}
	if strings.Contains(failure("Couldn’t load this", down, false), "tl_") {
		t.Error("a key in the message")
	}
}
