package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Our failures reach OnFailure (the server erroring or unreachable), the person's don't (a 4xx refusal
// is theirs to act on), and a failure never carries what was searched for, or the key.
func TestOnlyOurFailuresAreReported(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/search" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		http.Error(w, "this api key is read-only", http.StatusForbidden)
	}))
	c := &Client{Base: srv.URL, Key: "tl_p_secret", HTTP: srv.Client(),
		OnFailure: func(method, route string, err error) {
			got = append(got, fmt.Sprintf("api %s %s: %v", method, route, err))
		}}
	c.Search(t.Context(), "payroll leak")
	c.Comment(t.Context(), "t1", "hello")
	srv.Close()
	c.MyWork(t.Context()) // unreachable
	if len(got) != 2 || !strings.HasPrefix(got[0], "api GET /search: boom") || !strings.HasPrefix(got[1], "api GET /search: ") {
		t.Fatalf("reports: %q", got)
	}
	for _, r := range got {
		if strings.Contains(r, "payroll") || strings.Contains(r, "tl_p_secret") || strings.Contains(r, "assignee") {
			t.Fatalf("report leaks: %q", r)
		}
	}
	c.OnFailure = nil // nobody listening: no panic
	c.MyWork(t.Context())
}

// TL-514: the tree fields decode from the single-ticket response.
func TestTicketTreeDecodes(t *testing.T) {
	var got Ticket
	err := json.Unmarshal([]byte(`{"id":"t1","parent":{"id":"p","title":"Parent","done":false},
		"children":[{"id":"c","key":"TL-2","title":"Child","type":"bug","done":true,"assigned_to":null,"estimate":"3","board_name":null}],
		"epic":{"done":1,"total":2,"points":5.5,"unestimated":1}}`), &got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Parent == nil || got.Parent.ID != "p" || len(got.Children) != 1 || *got.Children[0].Key != "TL-2" || !got.Children[0].Done ||
		got.Children[0].BoardName != nil || got.Epic == nil || got.Epic.Points != 5.5 || got.Epic.Unestimated != 1 {
		t.Fatalf("decoded %+v", got)
	}
}
