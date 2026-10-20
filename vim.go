package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// vimArea is a text area with vim's two modes, so esc goes to normal mode instead of throwing the text
// away. It opens in insert mode. Normal mode moves (h j k l, w b, 0 $, g G), deletes (x, dd), goes back
// to inserting (i a I A o O), and sends (enter) or cancels (q; twice when there's text).
// ponytail: the everyday keys only, no counts, registers or undo; add them when they're missed.
type vimArea struct {
	textarea.Model
	normal  bool
	pending string // the first key of a pair: d (dd), q (q again to discard)
}

type vimAction int

const (
	vimNone vimAction = iota
	vimSend
	vimCancel
)

func newVimArea(placeholder string) vimArea {
	ta := textarea.New()
	ta.Placeholder = placeholder
	ta.ShowLineNumbers = false
	ta.Focus()
	return vimArea{Model: ta}
}

// keys turns vim's into the text area's own (see textarea.DefaultKeyMap).
var (
	left      = tea.KeyMsg{Type: tea.KeyLeft}
	right     = tea.KeyMsg{Type: tea.KeyRight}
	up        = tea.KeyMsg{Type: tea.KeyUp}
	down      = tea.KeyMsg{Type: tea.KeyDown}
	lineStart = tea.KeyMsg{Type: tea.KeyHome}
	lineEnd   = tea.KeyMsg{Type: tea.KeyEnd}
	wordNext  = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}, Alt: true}
	wordPrev  = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}, Alt: true}
	top       = tea.KeyMsg{Type: tea.KeyCtrlHome}
	bottom    = tea.KeyMsg{Type: tea.KeyCtrlEnd}
	delChar   = tea.KeyMsg{Type: tea.KeyDelete}
	delToEnd  = tea.KeyMsg{Type: tea.KeyCtrlK}
	newline   = tea.KeyMsg{Type: tea.KeyEnter}
)

var normalMoves = map[string][]tea.KeyMsg{
	"h": {left}, "left": {left}, "l": {right}, "right": {right},
	"k": {up}, "up": {up}, "j": {down}, "down": {down},
	"0": {lineStart}, "^": {lineStart}, "$": {lineEnd},
	"w": {wordNext}, "e": {wordNext}, "b": {wordPrev},
	"g": {top}, "G": {bottom},
	"x": {delChar},
}

// insertWith: the keys that start inserting, and where the cursor goes first.
var insertWith = map[string][]tea.KeyMsg{
	"i": nil, "a": {right}, "I": {lineStart}, "A": {lineEnd},
	"o": {lineEnd, newline}, "O": {lineStart, newline, up},
}

func (v *vimArea) send(keys ...tea.KeyMsg) {
	for _, k := range keys {
		v.Model, _ = v.Model.Update(k)
	}
}

// update handles a key in either mode and says whether to send or cancel.
func (v *vimArea) update(msg tea.Msg) (vimAction, tea.Cmd) {
	k, isKey := msg.(tea.KeyMsg)
	if !isKey {
		var cmd tea.Cmd
		v.Model, cmd = v.Model.Update(msg)
		return vimNone, cmd
	}
	if k.String() == "ctrl+s" {
		return vimSend, nil
	}
	if !v.normal {
		if k.String() == "esc" {
			v.normal, v.pending = true, ""
			v.send(left) // vim steps back onto the last character typed
			return vimNone, nil
		}
		var cmd tea.Cmd
		v.Model, cmd = v.Model.Update(k)
		return vimNone, cmd
	}
	key, pending := k.String(), v.pending
	v.pending = ""
	switch {
	case pending == "d" && key == "d": // the whole line, and one of the line breaks around it
		last := v.Line() == v.LineCount()-1
		v.send(lineStart, delToEnd)
		if !last {
			v.send(delChar) // the next line moves up into its place
		} else if v.Line() > 0 {
			v.send(tea.KeyMsg{Type: tea.KeyBackspace}, lineStart) // the last line: back onto the one above
		}
	case pending == "q" && key == "q":
		return vimCancel, nil
	case key == "d":
		v.pending = "d"
	case key == "q":
		if strings.TrimSpace(v.Value()) == "" {
			return vimCancel, nil
		}
		v.pending = "q"
	case key == "enter":
		return vimSend, nil
	case insertWith[key] != nil || key == "i":
		v.send(insertWith[key]...)
		v.normal = false
	case normalMoves[key] != nil:
		v.send(normalMoves[key]...)
	}
	return vimNone, nil
}

// mode is the status line: what mode it's in, and what a half-typed pair is waiting for.
func (v *vimArea) mode() string {
	switch {
	case v.pending == "q":
		return warn.Render("Unsent text: q again to discard it")
	case v.normal:
		return bold.Render("-- NORMAL --")
	}
	return accent.Render("-- INSERT --")
}

const vimHelp = "esc normal mode · i/a/o insert · enter or ctrl+s send · q cancel"
