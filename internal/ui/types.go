package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tracklines/tui/internal/api"
)

// The API's types by their short names, as the screens use them.
type (
	Client      = api.Client
	Ticket      = api.Ticket
	TicketInput = api.TicketInput
	Project     = api.Project
	Mention     = api.Mention
	Column      = api.Column
	Board       = api.Board
	Comment     = api.Comment
	APIError    = api.APIError
	Label       = api.Label
	BacklogPage = api.BacklogPage
	Session     = api.Session
	StandupTurn = api.StandupTurn
)

// Settings that come from outside the screens: set by New.
var (
	version = "dev"
	vimKeys = true // writing works like vim (modes, :w, :q); false: standard keys (config keys = standard)
)

// Options is what the TUI is started with.
type Options struct {
	Version string // this build's, for the ? screen and update checks
	VimKeys bool   // config: keys = vim (true) or standard
}

// New is the TUI, ready to run with Bubble Tea.
func New(client *Client, o Options) tea.Model {
	version, vimKeys = o.Version, o.VimKeys
	return newApp(client)
}
