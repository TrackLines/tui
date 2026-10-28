package ui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// failure says what failed and why in a few words, whether a change got through, and what to do
// next (TL-597). write: it was a change, so a retry could do it twice. The API key is never in it;
// the status, error code and request id are, for support.
func failure(what string, err error, write bool) string {
	reason, next := explain(err, write)
	out := warn.Render(what + ": " + reason)
	if next != "" {
		out += muted.Render(" · " + next)
	}
	if ref := reference(err); ref != "" {
		out += muted.Render(" (" + ref + ")")
	}
	return out
}

// explain is the reason, and the next step: a read can always be tried again; a change only when
// it's known not to have happened. One whose answer never came may have, so check before resending.
func explain(err error, write bool) (reason, next string) {
	unknown := T("it may have saved: refresh (r) to check before trying again")
	var e *APIError
	if !errors.As(err, &e) {
		var ne net.Error
		if !errors.As(err, &ne) && !errors.Is(err, context.DeadlineExceeded) {
			return err.Error(), "" // ours, already in words ("There’s no ticket TL-9.")
		}
		if write {
			return T("can’t reach TrackLin.es"), unknown
		}
		return T("can’t reach TrackLin.es"), T("check your connection, then r to try again")
	}
	switch {
	case e.Status == http.StatusUnauthorized:
		return e.Error(), ""
	case e.Status == http.StatusTooManyRequests:
		reason = T("too many requests")
	case e.Status >= 500:
		reason = F("TrackLin.es had a problem (%d)", e.Status)
	case e.Msg != "":
		reason = e.Msg // the API's own reason: what's invalid, or who may do it
	case e.Status == http.StatusForbidden:
		reason = T("not allowed")
	default:
		reason = T("refused")
	}
	switch {
	case !write && (e.Status == http.StatusTooManyRequests || e.Status >= 500):
		next = T("r to try again")
	case !write:
	case e.Status == http.StatusTooManyRequests || e.Status == http.StatusServiceUnavailable:
		next = T("not saved: try again in a moment") // refused before it was done
	case e.Status >= 500:
		next = unknown
	default:
		next = T("not saved")
	}
	return reason, next
}

// reference is what support needs to find the request: never the key, never the body.
func reference(err error) string {
	var e *APIError
	if !errors.As(err, &e) {
		return ""
	}
	ref := []string{fmt.Sprint(e.Status)}
	if e.Code != "" {
		ref = append(ref, e.Code)
	}
	if e.RequestID != "" {
		ref = append(ref, e.RequestID)
	}
	return strings.Join(ref, " ")
}

// failureText is explain's reason and next step as one plain line, for a kept draft.
func failureText(err error, write bool) string {
	reason, next := explain(err, write)
	if next == "" {
		return reason
	}
	return reason + " · " + next
}
