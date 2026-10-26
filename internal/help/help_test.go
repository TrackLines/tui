package help

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Built in: every language's answers work with no network (TL-657).
func TestBakedAnswersWorkOffline(t *testing.T) {
	en, nl := Baked("en"), Baked("nl")
	if len(en) != 37 || len(nl) != 37 {
		t.Fatalf("%d English, %d Dutch", len(en), len(nl))
	}
	if en[0].Slug != "add-board" || en[0].Question != "How do I add another board?" || en[0].Href != "/projects/[project]/edit" || strings.Join(en[0].Pages, ",") != "project" || en[0].Words == "" {
		t.Errorf("en: %+v", en[0])
	}
	if nl[0].Question != "Hoe voeg ik nog een bord toe?" || nl[0].Answer == en[0].Answer {
		t.Errorf("nl: %+v", nl[0])
	}
	if got := Baked("xx"); len(got) != 37 || got[0].Question != en[0].Question {
		t.Error("an unknown language isn't English")
	}
	if hits := Search(nl, "bord toevoegen"); len(hits) == 0 || hits[0].Slug != "add-board" {
		t.Errorf("Dutch search: %v", hits)
	}
}

func TestParse(t *testing.T) {
	po := `msgid ""
msgstr ""
"Language: nl\n"

#. href: /x
#. fallback_href: /y
#. pages: board project
#. words: w1 w2
msgctxt "a:question"
msgid "Say \"hi\"\nok?"
msgstr ""
"Zeg "
"\"hoi\"?"

msgctxt "a:answer"
msgid "back\\slash"
msgstr ""
`
	got := Parse([]byte(po))
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	a := got[0]
	if a.Question != `Zeg "hoi"?` || a.Answer != `back\slash` || a.Href != "/x" || a.FallbackHref != "/y" || strings.Join(a.Pages, " ") != "board project" || a.Words != "w1 w2" {
		t.Errorf("%+v", a)
	}
}

func TestLiveUpdatesAndTheCache(t *testing.T) {
	dir := t.TempDir()
	s := Open("en", dir)
	if len(s.Answers()) != 37 || s.ETag() != "" {
		t.Fatal("doesn't start from the baked answers")
	}
	var sent []string
	reply := func(b []byte, etag string, err error) func(context.Context, string, string) ([]byte, string, error) {
		return func(_ context.Context, locale, have string) ([]byte, string, error) {
			sent = append(sent, locale+" "+have)
			if b == nil && err == nil {
				return nil, have, nil // 304
			}
			return b, etag, err
		}
	}
	// a 200 swaps them in, no restart
	changed, err := s.Fetch(context.Background(), reply([]byte(`[{"slug":"new","question":"New?","answer":"Yes","pages":[]}]`), `"v2"`, nil))
	if !changed || err != nil || len(s.Answers()) != 1 || s.Answers()[0].Question != "New?" || s.ETag() != `"v2"` {
		t.Fatalf("200: %v %v %+v", changed, err, s.Answers())
	}
	// a 304, a failure and an empty list keep them
	for _, f := range []func(context.Context, string, string) ([]byte, string, error){
		reply(nil, "", nil), reply(nil, "", errors.New("offline")), reply([]byte(`[]`), `"v3"`, nil), reply([]byte(`nope`), `"v3"`, nil),
	} {
		if changed, _ := s.Fetch(context.Background(), f); changed || s.Answers()[0].Question != "New?" || s.ETag() != `"v2"` {
			t.Fatalf("kept: %v %+v", changed, s.Answers())
		}
	}
	if sent[1] != `en "v2"` {
		t.Errorf("didn't send what it has: %v", sent)
	}
	// the next start uses the cached copy, not the baked one
	again := Open("en", dir)
	if len(again.Answers()) != 1 || again.ETag() != `"v2"` {
		t.Fatalf("cache: %+v", again.Answers())
	}
	if _, err := os.Stat(filepath.Join(dir, "help.en.json")); err != nil {
		t.Fatal(err)
	}
	if other := Open("nl", dir); len(other.Answers()) != 37 {
		t.Error("another language used English's cache")
	}
}

func TestLink(t *testing.T) {
	a := Answer{Href: "/projects/[project]/edit"}
	for _, c := range []struct{ project, board, want string }{
		{"p1", "", "https://tracklin.es/projects/p1/edit"},
		{"", "", "https://tracklin.es/dashboard"},
	} {
		if got := Link("https://tracklin.es/", a, c.project, c.board); got != c.want {
			t.Errorf("%+v: %s", c, got)
		}
	}
	if got := Link("https://tracklin.es", Answer{Href: "/boards/[board]", FallbackHref: "/settings"}, "", ""); got != "https://tracklin.es/settings" {
		t.Errorf("fallback: %s", got)
	}
	if got := Link("https://tracklin.es", Answer{Href: "/boards/[board]?view=next"}, "p1", "b1"); got != "https://tracklin.es/boards/b1?view=next" {
		t.Errorf("board: %s", got)
	}
	if Link("https://tracklin.es", Answer{}, "p1", "b1") != "" {
		t.Error("no href, no link")
	}
}
