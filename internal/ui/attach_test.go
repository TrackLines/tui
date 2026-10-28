package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TL-660: what's typed, pasted or dropped becomes a path.
func TestCleanPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	for in, want := range map[string]string{
		"  /tmp/a.txt \n":           "/tmp/a.txt",
		"'/tmp/my file.png'":        "/tmp/my file.png",
		`"/tmp/my file.png"`:        "/tmp/my file.png",
		`/tmp/my\ file\ \(1\).png`:  "/tmp/my file (1).png",
		"file:///tmp/my%20file.png": "/tmp/my file.png",
		"~/notes.md":                home + "/notes.md",
		"~":                         home,
		"":                          "",
		"/tmp/it's.txt":             "/tmp/it's.txt",
		"'/tmp/half":                "'/tmp/half",
	} {
		if got := cleanPath(in); got != want {
			t.Errorf("cleanPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// tab fills in what the folder agrees on and lists the choices; folders end in /, hidden files wait for a dot.
func TestCompletePath(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"report.txt", "report-2.txt", "notes.md", ".hidden"} {
		os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600)
	}
	os.Mkdir(filepath.Join(dir, "logs"), 0o700)
	v := newAttach(Ticket{}, 0)
	for in, want := range map[string]string{
		dir + "/re":       dir + "/report",
		dir + "/n":        dir + "/notes.md",
		dir + "/l":        dir + "/logs/",
		dir + "/.h":       dir + "/.hidden",
		dir + "/zzz":      dir + "/zzz",
		"'" + dir + "/n'": dir + "/notes.md",
	} {
		if got := v.complete(in); got != want {
			t.Errorf("complete(%q) = %q, want %q", in, got, want)
		}
	}
	v.complete(dir + "/")
	if strings.Join(v.options, ",") != "logs/,notes.md,report-2.txt,report.txt" {
		t.Fatalf("choices: %v", v.options)
	}
}

func TestUploadProgress(t *testing.T) {
	if p := progress("a.zip", 512*1024, 1024*1024, 120); !strings.Contains(p, "50%") || !strings.Contains(p, "512.0 KB of 1.0 MB") || !strings.Contains(p, "[####") {
		t.Fatalf("half: %q", p)
	}
	if p := progress("a.zip", 10, 10, 120); !strings.Contains(p, "100%") || !strings.Contains(p, "storing…") {
		t.Fatalf("all sent: %q", p)
	}
	if contentType("shot.png") != "image/png" || contentType("run.weird") != "application/octet-stream" {
		t.Fatal("content types")
	}
}

// u attaches a file through the backend; what can't be sent says why and leaves you where you were;
// d removes the picked file once you say y, and the API decides whose files you may remove.
func TestAttachAndRemoveFiles(t *testing.T) {
	a, calls := start(t)
	t.Cleanup(func() { uploadFails, removeForbidden = false, false })
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.txt")
	os.WriteFile(file, []byte("hello"), 0o600)
	big := filepath.Join(dir, "big.bin")
	f, _ := os.Create(big)
	f.Truncate(11 << 20)
	f.Close()

	a.stack = append(a.stack, &ticketView{id: "t1"})
	pump(a, a.top().load(a), 0)
	expect(t, a, "u attach file")
	press(a, "u")
	expect(t, a, "Attach a file to TL-1", "File:", "10 files a ticket (1 on it now)", "tab complete · enter attach · esc back")

	uploads := func() int { return strings.Count(strings.Join(*calls, "\n"), "/attachments/upload") }
	for path, says := range map[string]string{
		filepath.Join(dir, "gone.txt"): "Can’t read",
		dir:                            "That’s a folder",
		big:                            "big.bin is 11.0 MB: files can be up to 10 MB.",
	} {
		a.top().(*attachView).input.SetValue(path)
		press(a, "enter")
		expect(t, a, says)
	}
	if uploads() != 0 {
		t.Fatalf("sent a file it should have refused: %v", *calls)
	}

	uploadFails = true
	a.top().(*attachView).input.SetValue("'" + file + "'") // as a drop pastes it
	press(a, "enter")
	expect(t, a, "Couldn’t attach notes.txt: this ticket already has as many files", "Attach a file to TL-1") // still here, to try again
	uploadFails = false
	press(a, "enter")
	expect(t, a, "Attached notes.txt.", "Fix login") // back on the ticket, reloaded
	if got := find(*calls, "POST /api/tickets/t1/attachments/upload"); got != "POST /api/tickets/t1/attachments/upload?name=notes.txt text/plain; charset=utf-8 5" {
		t.Fatalf("upload: %q", got)
	}

	// removing: pick it, d asks, anything but y keeps it
	press(a, "tab")
	expect(t, a, "d remove file")
	press(a, "d")
	expect(t, a, "Remove shot.png? The file is deleted too.")
	press(a, "n")
	expect(t, a, "Kept it.")
	if find(*calls, "DELETE") != "" {
		t.Fatal("removed without a yes")
	}
	removeForbidden = true
	press(a, "d", "y")
	expect(t, a, "Only an organization admin or whoever uploaded it can remove this file.")
	removeForbidden = false
	press(a, "tab", "d", "y")
	expect(t, a, "Removed shot.png.")
	if find(*calls, "DELETE /api/attachments/f1") == "" {
		t.Fatalf("no delete: %v", *calls)
	}

	// a done ticket's files stay as they are
	done := Ticket{ID: "t1", Key: "TL-1", Title: "Fix login", Done: true}
	pump(a, msgOf(loadedMsg{"ticket:t1", ticketData{t: done, files: []Attachment{{ID: "f1", Name: "shot.png"}}}, nil}), 0)
	press(a, "u")
	expect(t, a, "This ticket is done: move it out of Done to add or remove files.")
	if _, attaching := a.top().(*attachView); attaching {
		t.Fatal("attaching to a done ticket")
	}
}

// esc while a file is going up stops it, says so, and leaves the screen usable.
func TestCancelAnUpload(t *testing.T) {
	a, calls := start(t)
	uploadHold = make(chan struct{})
	t.Cleanup(func() { uploadHold = nil })
	file := filepath.Join(t.TempDir(), "slow.log")
	os.WriteFile(file, []byte("lines"), 0o600)

	a.stack = append(a.stack, &ticketView{id: "t1"})
	pump(a, a.top().load(a), 0)
	press(a, "u")
	v := a.top().(*attachView)
	v.input.SetValue(file)
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	batch, _ := cmd().(tea.BatchMsg)
	if len(batch) != 2 {
		t.Fatalf("enter didn't start an upload and its progress: %v", batch)
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- batch[0]() }()
	expect(t, a, "slow.log", "esc cancel the upload")
	for i := 0; i < 200 && uploadArrived(calls) == ""; i++ {
		time.Sleep(10 * time.Millisecond) // until it's at the server, waiting there
	}
	press(a, "x") // typing does nothing mid-upload
	press(a, "esc")
	pump(a, msgOf(<-got), 0)
	expect(t, a, "Upload cancelled.", "Attach a file to TL-1", "tab complete")
	if v.input.Value() != file {
		t.Fatalf("lost the path: %q", v.input.Value())
	}
	if uploadArrived(calls) == "" {
		t.Fatalf("never started: %v", *calls)
	}
	press(a, "esc")
	expect(t, a, "Fix login")
}

// uploadArrived: the upload the fake API has heard, read under its lock (it's still going).
func uploadArrived(calls *[]string) string {
	callsMu.Lock()
	defer callsMu.Unlock()
	return find(*calls, "POST /api/tickets/t1/attachments/upload")
}

// TL-713: ctrl+o on a new ticket creates it and goes straight to attaching a file to it; editing has no such key.
func TestAttachAtCreate(t *testing.T) {
	a, calls := start(t)
	file := filepath.Join(t.TempDir(), "shot.png")
	os.WriteFile(file, []byte("png"), 0o600)
	press(a, "2", "enter", "enter", "n") // a new ticket on the Web board
	expect(t, a, "ctrl+o save and attach a file")
	press(a, "Login fails", "tab", "tab", "tab", "right", "ctrl+o")
	expect(t, a, "Created TL-9.", "Attach a file to TL-9", "(0 on it now)")
	a.top().(*attachView).input.SetValue(file)
	press(a, "enter")
	expect(t, a, "Attached shot.png.")
	if find(*calls, "POST /api/tickets/t9/attachments/upload") == "" {
		t.Fatalf("no upload to the new ticket: %v", *calls)
	}

	a.stack = append(a.stack, newForm(&Ticket{ID: "t1", Key: "TL-1", Title: "Fix login"}, "p1", "", "none"))
	pump(a, a.top().load(a), 0)
	if strings.Contains(a.View(), "ctrl+o") {
		t.Fatal("the edit form offers ctrl+o")
	}
}
