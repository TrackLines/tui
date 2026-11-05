package main

import (
	"fmt"
	"io/fs"
	"syscall"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TL-798: started without a terminal, Bubble Tea fails to open /dev/tty; that's said plainly, not reported as a crash.
func TestNoTerminal(t *testing.T) {
	noTTY := fmt.Errorf("could not open a new TTY: %w", &fs.PathError{Op: "open", Path: "/dev/tty", Err: syscall.ENXIO})
	if !noTerminal(noTTY) {
		t.Fatal("no /dev/tty should read as no terminal")
	}
	for _, err := range []error{nil, tea.ErrProgramPanic, tea.ErrProgramKilled, fmt.Errorf("error entering raw mode: %w", syscall.EINVAL)} {
		if noTerminal(err) {
			t.Fatalf("%v read as no terminal: it should still be reported", err)
		}
	}
	if noTerminalText() == "" {
		t.Fatal("no message")
	}
}
