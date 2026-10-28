package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tracklines/tui/internal/api"
)

// attachView uploads a file to a ticket (TL-660): type or paste its path (tab completes it), enter
// sends it through the backend with its progress shown, esc cancels the upload or leaves.
type attachView struct {
	ticket  Ticket
	files   int // already on the ticket
	input   textinput.Model
	options []string // what tab found, when it found more than one

	cancel context.CancelFunc // set while uploading
	sent   *atomic.Int64
	size   int64
	name   string
}

type (
	attachTick    struct{}
	attachDoneMsg struct {
		name string
		err  error
	}
)

func newAttach(t Ticket, files int) *attachView {
	in := textinput.New()
	in.Placeholder = T("path to a file, or paste/drop one")
	in.Prompt = T("File:") + " "
	in.Focus()
	return &attachView{ticket: t, files: files, input: in}
}

func (v *attachView) load(a *app) tea.Cmd {
	a.loading = false
	v.input.Width = max(20, a.width-len(v.input.Prompt)-2)
	return textinput.Blink
}

// close: leaving mid-upload (ctrl+c) stops it.
func (v *attachView) close(*app) tea.Cmd {
	if v.cancel != nil {
		v.cancel()
	}
	return nil
}

func (v *attachView) update(a *app, msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case attachTick:
		if v.cancel == nil {
			return v, nil
		}
		return v, tickAttach() // the progress redraws with each tick
	case attachDoneMsg:
		v.cancel = nil
		switch {
		case errors.Is(msg.err, context.Canceled):
			a.status = muted.Render(T("Upload cancelled."))
		case msg.err != nil:
			a.status = failure(F("Couldn’t attach %s", msg.name), msg.err, true)
		default:
			return v, msgOf(popMsg{ok.Render(F("Attached %s.", msg.name))})
		}
		return v, nil // the path stays, to try again
	case tea.KeyMsg:
		if v.cancel != nil { // uploading: only esc does anything
			if msg.String() == "esc" {
				v.cancel()
			}
			return v, nil
		}
		switch msg.String() {
		case "esc":
			return v, msgOf(popMsg{})
		case "tab":
			v.input.SetValue(v.complete(v.input.Value()))
			v.input.CursorEnd()
			return v, nil
		case "enter":
			return v, v.start(a)
		}
	}
	var cmd tea.Cmd
	v.input, cmd = v.input.Update(msg)
	v.options = nil
	return v, cmd
}

// start checks what it can here (the backend checks it all again) and sends the file.
func (v *attachView) start(a *app) tea.Cmd {
	path := cleanPath(v.input.Value())
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	switch {
	case err != nil:
		a.status = warn.Render(F("Can’t read %s: %s", path, errors.Unwrap(err)))
		return nil
	case info.IsDir():
		a.status = warn.Render(T("That’s a folder: pick a file in it (tab lists them)."))
		return nil
	case info.Size() > api.MaxFileBytes:
		a.status = warn.Render(F("%s is %s: files can be up to 10 MB.", info.Name(), fileSize(info.Size())))
		return nil
	case v.files >= api.MaxFiles:
		a.status = warn.Render(T("This ticket already has 10 files: remove one to add another."))
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		a.status = warn.Render(F("Can’t read %s: %s", path, errors.Unwrap(err)))
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.cancel, v.sent, v.size, v.name = cancel, &atomic.Int64{}, info.Size(), info.Name()
	a.status = ""
	id, name, size, sent := v.ticket.ID, info.Name(), info.Size(), v.sent
	upload := func() tea.Msg {
		defer f.Close()
		defer cancel()
		_, err := a.api.Upload(ctx, id, name, contentType(name), &counting{f, sent}, size)
		if ctx.Err() != nil {
			err = context.Canceled
		}
		return attachDoneMsg{name, err}
	}
	return tea.Batch(upload, tickAttach())
}

func tickAttach() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return attachTick{} })
}

// counting is the file as it's read for sending, counting how much has gone.
type counting struct {
	r io.Reader
	n *atomic.Int64
}

func (c *counting) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

// progress: "shot.png  [#####-----]  48%  1.2 MB of 2.5 MB"; all sent, the server's still storing it.
func progress(name string, sent, size int64, width int) string {
	pct := 100
	if size > 0 {
		pct = int(sent * 100 / size)
	}
	bar := max(10, min(40, width-40))
	done := bar * pct / 100
	line := fmt.Sprintf("%s  [%s%s]  %d%%", name, strings.Repeat("#", done), strings.Repeat("-", bar-done), pct)
	if sent >= size {
		return line + "  " + T("storing…")
	}
	return line + "  " + F("%s of %s", fileSize(sent), fileSize(size))
}

func (v *attachView) render(a *app) string {
	b := accent.Render(F("Attach a file to %s", v.ticket.Key)) + "  " + v.ticket.Title + "\n\n"
	if v.cancel != nil {
		return b + progress(v.name, v.sent.Load(), v.size, a.width) + "\n"
	}
	b += v.input.View() + "\n"
	if len(v.options) > 0 {
		shown := v.options[:min(len(v.options), max(1, a.rows()-4))]
		b += "\n" + muted.Render(strings.Join(shown, "\n")) + "\n"
	}
	return b + "\n" + muted.Render(F("Up to 10 MB a file, 10 files a ticket (%d on it now).", v.files)) + "\n"
}

func (v *attachView) help() string {
	if v.cancel != nil {
		return T("esc cancel the upload")
	}
	return T("tab complete · enter attach · esc back")
}
func (v *attachView) typing() bool { return true }

// complete fills in as much of the path as the folder agrees on, and lists the choices when there
// are several. Hidden files only when you've typed the dot.
func (v *attachView) complete(raw string) string {
	path := cleanPath(raw)
	dir, base := filepath.Split(path)
	read := dir
	if read == "" {
		read = "."
	}
	entries, err := os.ReadDir(read)
	if err != nil {
		return raw
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, base) || (strings.HasPrefix(n, ".") && !strings.HasPrefix(base, ".")) {
			continue
		}
		if e.IsDir() {
			n += string(filepath.Separator)
		}
		names = append(names, n)
	}
	sort.Strings(names)
	v.options = nil
	switch len(names) {
	case 0:
		return raw
	case 1:
		return dir + names[0]
	}
	v.options = names
	common := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, common) {
			common = common[:len(common)-1]
		}
	}
	return dir + common
}

// cleanPath turns what was typed, pasted or dropped into a path: drops quotes, a file:// URL, the
// backslashes terminals put before spaces, and expands ~.
func cleanPath(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
	}
	if rest, isURL := strings.CutPrefix(s, "file://"); isURL {
		if u, err := url.PathUnescape(rest); err == nil {
			s = u
		}
	} else if runtime.GOOS != "windows" && strings.Contains(s, `\`) {
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			if s[i] == '\\' && i+1 < len(s) {
				i++
			}
			b.WriteByte(s[i])
		}
		s = b.String()
	}
	if s == "~" || strings.HasPrefix(s, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			s = home + s[1:]
		}
	}
	return s
}

// contentType goes by the file's extension; the backend stores unknown files as octet-stream.
func contentType(name string) string {
	if t := mime.TypeByExtension(filepath.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}
