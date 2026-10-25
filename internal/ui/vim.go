package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// vimArea is a text area with vim's two modes, so esc goes to normal mode instead of throwing the text
// away. It opens in insert mode. Normal mode moves (h j k l, w b, 0 $, g G), deletes (x, dd), goes back
// to inserting (i a I A o O), and sends (enter, :w, :wq) or cancels (q, :q; :q! with unsaved text).
// With keys = standard (config.go) there are no modes: typing types, esc cancels, ctrl+s saves.
// ponytail: the everyday keys only, no counts, registers or undo; add them when they're missed.
type vimArea struct {
	textarea.Model
	normal  bool
	pending string // the first key of a pair: d (dd), q (q again to discard), esc (standard: again to discard)
	cmd     cmdLine
	unsaved func() bool // is there something to lose (:q refuses then)? nil: any text
}

// cmdLine is vim's : prompt (TL-475): :w, :wq and :x save; :q leaves, but not over unsaved text
// (vim's E37); :q! leaves anyway.
type cmdLine struct {
	on        bool
	text, msg string // what's typed after :, and what the last command said
}

func (c *cmdLine) open() { c.on, c.text, c.msg = true, "", "" }

// key is one key typed at the prompt. unsaved: there's something :q would throw away.
func (c *cmdLine) key(k tea.KeyMsg, unsaved bool) vimAction {
	switch k.Type {
	case tea.KeyEsc:
		c.on = false
	case tea.KeyBackspace:
		if c.text == "" {
			c.on = false
		} else {
			r := []rune(c.text)
			c.text = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		c.text += " "
	case tea.KeyRunes:
		c.text += string(k.Runes)
	case tea.KeyEnter:
		c.on = false
		switch cmd := strings.TrimSpace(c.text); cmd {
		case "":
		case "w", "wq", "x", "w!", "wq!", "x!":
			return vimSend
		case "q":
			if !unsaved {
				return vimCancel
			}
			c.msg = T("E37: No write since last change (add ! to override)")
		case "q!":
			return vimCancel
		default:
			c.msg = F("E492: Not an editor command: %s", cmd)
		}
	}
	return vimNone
}

// view is the prompt while it's open, or what the last command said.
func (c *cmdLine) view() string {
	switch {
	case c.on:
		return ":" + c.text
	case c.msg != "":
		return warn.Render(c.msg)
	}
	return ""
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
	if !vimKeys { // standard keys: no modes; esc cancels, twice over unsaved text
		if k.String() != "esc" {
			v.pending = ""
			var cmd tea.Cmd
			v.Model, cmd = v.Model.Update(k)
			return vimNone, cmd
		}
		if v.pending == "esc" || !v.hasUnsaved() {
			return vimCancel, nil
		}
		v.pending = "esc"
		return vimNone, nil
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
	if v.cmd.on {
		return v.cmd.key(k, v.hasUnsaved()), nil
	}
	key, pending := k.String(), v.pending
	v.pending, v.cmd.msg = "", ""
	switch {
	case key == ":":
		v.cmd.open()
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
		if !v.hasUnsaved() {
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

func (v *vimArea) hasUnsaved() bool {
	if v.unsaved != nil {
		return v.unsaved()
	}
	return strings.TrimSpace(v.Value()) != ""
}

// mode is the status line: what mode it's in, the : prompt, and what a half-typed pair is waiting for.
func (v *vimArea) mode() string {
	switch {
	case v.pending == "esc":
		return warn.Render(T("Unsaved text: esc again to discard it"))
	case !vimKeys:
		return ""
	case v.cmd.on || v.cmd.msg != "":
		return v.cmd.view()
	case v.pending == "q":
		return warn.Render(T("Unsent text: q again to discard it"))
	case v.normal:
		return bold.Render(T("-- NORMAL --"))
	}
	return accent.Render(T("-- INSERT --"))
}

// editHelp is the keys for writing: vim's, or standard ones (keys = standard in the config).
func editHelp() string {
	if !vimKeys {
		return T("ctrl+s save · esc cancel")
	}
	return T("esc normal mode · i/a/o insert · :w save · :q cancel (:q! discards) · ctrl+s save")
}
