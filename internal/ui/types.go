package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tracklines/tui/internal/api"
	"github.com/tracklines/tui/internal/i18n"
)

// T and F are what the screens show, in the configured language (TL-604): T a text, F a format.
var (
	T = i18n.T
	F = i18n.F
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
	Attachment  = api.Attachment
	APIError    = api.APIError
	Label       = api.Label
	BacklogPage = api.BacklogPage
	Session     = api.Session
	StandupTurn = api.StandupTurn
)

// Settings that come from outside the screens: set by New.
var (
	version     = "dev"
	vimKeys     = true     // writing works like vim (modes, :w, :q); false: standard keys (config keys = standard)
	backlogSort = "manual" // the order a backlog opens in (config backlog_sort)
	updateCheck = true     // look for a newer release on starting (config update_check)
)

// Options is what the TUI is started with.
type Options struct {
	Version     string // this build's, for the ? screen and update checks
	VimKeys     bool   // config: keys = vim (true) or standard
	BacklogSort string // config: backlog_sort = manual, newest or oldest
	UpdateCheck bool   // config: update_check = on or off
	Language    string // config: language = system, en or nl (TL-604)
}

// New is the TUI, ready to run with Bubble Tea.
func New(client *Client, o Options) tea.Model {
	version, vimKeys, updateCheck = o.Version, o.VimKeys, o.UpdateCheck
	i18n.Set(o.Language)
	if o.BacklogSort != "" {
		backlogSort = o.BacklogSort
	}
	return newApp(client)
}
