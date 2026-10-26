// Package help is the help answers (TL-657): the ones staff write in the admin panel (TL-575), baked
// into the binary from the backend's .po export so help works offline, then kept up to date from the
// API while the TUI runs, without a restart. The latest copy is cached for the next start.
package help

//go:generate go run ./gen

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Answer is one answer in one language, as GET /api/help returns it.
type Answer struct {
	Slug         string   `json:"slug"`
	Question     string   `json:"question"`
	Answer       string   `json:"answer"`
	Href         string   `json:"href"`
	FallbackHref string   `json:"fallback_href"`
	Pages        []string `json:"pages"`
	Words        string   `json:"words"`
}

//go:embed baked/*.po
var baked embed.FS

// Baked is the answers built in for lang (English where there's no file or no translation).
func Baked(lang string) []Answer {
	b, err := baked.ReadFile("baked/help." + lang + ".po")
	if err != nil {
		b, _ = baked.ReadFile("baked/help.en.po")
	}
	return Parse(b)
}

// Parse reads the backend's .po export: msgctxt "<slug>:question" / "<slug>:answer", msgid the English,
// msgstr the translation ("" for none: the English), and the question's #. comments the rest.
func Parse(po []byte) []Answer {
	var out []Answer
	index := map[string]int{}
	var meta map[string]string
	var ctx, id, str string
	field := ""
	flush := func() {
		slug, part, ok := strings.Cut(ctx, ":")
		if ok && slug != "" {
			i, seen := index[slug]
			if !seen {
				i = len(out)
				index[slug] = i
				out = append(out, Answer{Slug: slug, Pages: []string{}})
			}
			text := str
			if text == "" {
				text = id
			}
			switch part {
			case "question":
				out[i].Question = text
				if meta != nil {
					out[i].Href, out[i].FallbackHref, out[i].Words = meta["href"], meta["fallback_href"], meta["words"]
					out[i].Pages = strings.Fields(meta["pages"])
				}
			case "answer":
				out[i].Answer = text
			}
		}
		meta, ctx, id, str, field = nil, "", "", "", ""
	}
	sc := bufio.NewScanner(bytes.NewReader(po))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "#. "):
			k, v, _ := strings.Cut(strings.TrimPrefix(line, "#. "), ":")
			if meta == nil {
				meta = map[string]string{}
			}
			meta[k] = strings.TrimSpace(v)
		case strings.HasPrefix(line, "msgctxt "):
			field, ctx = "ctx", unquote(line[len("msgctxt "):])
		case strings.HasPrefix(line, "msgid "):
			field, id = "id", unquote(line[len("msgid "):])
		case strings.HasPrefix(line, "msgstr "):
			field, str = "str", unquote(line[len("msgstr "):])
		case strings.HasPrefix(line, `"`): // a continued string
			switch field {
			case "ctx":
				ctx += unquote(line)
			case "id":
				id += unquote(line)
			case "str":
				str += unquote(line)
			}
		}
	}
	flush()
	return out
}

func unquote(s string) string {
	u, err := strconv.Unquote(s)
	if err != nil {
		return ""
	}
	return u
}

// Store is one language's answers: the baked ones, the cached ones, or the latest from the API.
type Store struct {
	mu      sync.RWMutex
	lang    string
	answers []Answer
	etag    string
	cache   string // the file the latest copy is kept in; "" for none
}

type cached struct {
	ETag    string   `json:"etag"`
	Answers []Answer `json:"answers"`
}

// Open is lang's answers: the cached copy in dir if there is one, else the baked ones.
func Open(lang, dir string) *Store {
	s := &Store{lang: lang, answers: Baked(lang)}
	if dir != "" {
		s.cache = filepath.Join(dir, "help."+lang+".json")
		var c cached
		if b, err := os.ReadFile(s.cache); err == nil && json.Unmarshal(b, &c) == nil && len(c.Answers) > 0 {
			s.answers, s.etag = c.Answers, c.ETag
		}
	}
	return s
}

func (s *Store) Lang() string { return s.lang }

// Answers is what there is now; safe while an update arrives.
func (s *Store) Answers() []Answer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.answers
}

// ETag is the version held, for If-None-Match; "" for the baked ones.
func (s *Store) ETag() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.etag
}

// Replace swaps in newer answers and keeps them for the next start.
func (s *Store) Replace(answers []Answer, etag string) {
	s.mu.Lock()
	s.answers, s.etag = answers, etag
	s.mu.Unlock()
	if s.cache == "" {
		return
	}
	if b, err := json.Marshal(cached{etag, answers}); err == nil {
		_ = os.WriteFile(s.cache, b, 0o600) // ponytail: a cache; failing to write it only costs a fetch
	}
}

// Fetch asks the API for newer answers and swaps them in; true when they changed. A 304 or a failure
// keeps what's held.
func (s *Store) Fetch(ctx context.Context, get func(ctx context.Context, locale, etag string) ([]byte, string, error)) (bool, error) {
	b, etag, err := get(ctx, s.lang, s.ETag())
	if err != nil || b == nil {
		return false, err
	}
	var answers []Answer
	if err := json.Unmarshal(b, &answers); err != nil {
		return false, err
	}
	if len(answers) == 0 {
		return false, errors.New("help: no answers") // never swap working help for none
	}
	s.Replace(answers, etag)
	return true, nil
}

// normalise: lower case, no accents, so "Overzicht" finds "overzicht" and "café" finds "cafe".
func normalise(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Search is the answers with the most of the query's words, the question's counting double (as the
// web app's help panel); no query: all of them, in order.
func Search(answers []Answer, query string) []Answer {
	words := strings.FieldsFunc(normalise(query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	words = slices.DeleteFunc(words, func(w string) bool { return len([]rune(w)) < 2 })
	if len(words) == 0 {
		return answers
	}
	type scored struct {
		a     Answer
		score int
	}
	var hits []scored
	for _, a := range answers {
		q, rest := normalise(a.Question), normalise(a.Answer+" "+a.Words)
		score := 0
		for _, w := range words {
			switch {
			case strings.Contains(q, w):
				score += 2
			case strings.Contains(rest, w):
				score++
			}
		}
		if score > 0 {
			hits = append(hits, scored{a, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := make([]Answer, len(hits))
	for i, h := range hits {
		out[i] = h.a
	}
	return out
}

// Link is the answer's page on the website, with the project or board you're on filled in (else its
// fallback, else the dashboard); "" when it has none.
func Link(site string, a Answer, project, board string) string {
	h := a.Href
	switch {
	case h == "":
		return ""
	case strings.Contains(h, "[project]") && project != "":
		h = strings.ReplaceAll(h, "[project]", project)
	case strings.Contains(h, "[board]") && board != "":
		h = strings.ReplaceAll(h, "[board]", board)
	case strings.Contains(h, "[project]") || strings.Contains(h, "[board]"):
		h = a.FallbackHref
		if h == "" {
			h = "/dashboard"
		}
	}
	return strings.TrimRight(site, "/") + h
}
