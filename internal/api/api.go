// Package api is the TrackLin.es API client the TUI uses, signed in with a personal key (tl_p_…).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to the TrackLin.es API with a personal key (tl_p_…), so "my work" is the key owner's.
type Client struct {
	Base string // https://api.tracklin.es
	Key  string
	HTTP *http.Client
	// OnFailure hears our failures (the server erroring or unreachable), to report them: the route
	// without its query (what someone searched for), and the cause without the URL. nil: nobody.
	OnFailure func(method, route string, err error)
}

type Ticket struct {
	ID          string   `json:"id"`
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Priority    string   `json:"priority"`
	Estimate    *string  `json:"estimate"`
	BoardID     *string  `json:"board_id"` // null in the backlog
	BoardName   *string  `json:"board_name"`
	ColumnID    *string  `json:"column_id"`
	ColumnName  *string  `json:"column_name"`
	ProjectName string   `json:"project_name"`
	Blocked     bool     `json:"blocked"`
	Done        bool     `json:"done"`
	AssignedTo  *string  `json:"assigned_to"`
	Labels      []string `json:"labels"`
	ProjectID   string   `json:"project_id"`
	// a spike's timebox (TL-502): days + hours as entered; nil when not a spike or not yet timed
	TimeboxDays  *int    `json:"timebox_days"`
	TimeboxHours *int    `json:"timebox_hours"`
	DueDate      *string `json:"due_date"`   // YYYY-MM-DD, or null (TL-640)
	StartedAt    *string `json:"started_at"` // first move into In Progress; drives the timebox countdown (TL-506)
	DoneAt       *string `json:"done_at"`
	UpdatedAt    string  `json:"updated_at"` // an edit draft checks it before resending (TL-599)
	// where it sits in the tree (TL-514): the ticket it's a sub-ticket of, its own sub-tickets, and
	// an epic's progress over them; only the single-ticket endpoint fills these
	Parent   *Parent       `json:"parent"`
	Children []Child       `json:"children"`
	Epic     *EpicProgress `json:"epic"`
}

// Parent is the ticket a sub-ticket belongs to (the API gives no key for it).
type Parent struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// Child is one sub-ticket, as a parent or epic lists it.
type Child struct {
	ID         string  `json:"id"`
	Key        *string `json:"key"`
	Title      string  `json:"title"`
	Type       string  `json:"type"`
	Done       bool    `json:"done"`
	AssignedTo *string `json:"assigned_to"`
	Estimate   *string `json:"estimate"`
	BoardName  *string `json:"board_name"` // nil: in the backlog
}

// EpicProgress is an epic's progress over its direct sub-tickets.
type EpicProgress struct {
	Done        int     `json:"done"`
	Total       int     `json:"total"`
	Points      float64 `json:"points"`
	Unestimated int     `json:"unestimated"`
}

// TicketInput is what creating or editing a ticket sends. Estimate is on the board's scale.
type TicketInput struct {
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	Type         string  `json:"type"`
	Priority     string  `json:"priority"`
	Estimate     *string `json:"estimate,omitempty"`
	TimeboxDays  *int    `json:"timebox_days,omitempty"`
	TimeboxHours *int    `json:"timebox_hours,omitempty"`
	DueDate      *string `json:"due_date,omitempty"` // "" clears it; nil leaves it (TL-640)
}

type Project struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	KeyPrefix   string  `json:"key_prefix"`
	Boards      []Board `json:"boards"`
}

type Mention struct {
	TicketID    string `json:"ticket_id"`
	Key         string `json:"key"`
	Title       string `json:"title"`
	ProjectName string `json:"project_name"`
	Actor       string `json:"actor"`
	MentionedAt string `json:"mentioned_at"`
}

type Column struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Tickets []Ticket `json:"tickets"`
}

type Board struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ProjectID     string   `json:"project_id"`
	Style         string   `json:"style"`          // sprints | kanban
	EstimateScale string   `json:"estimate_scale"` // none, or the scale tickets are sized on
	Columns       []Column `json:"columns"`
}

// Scales mirrors the backend's boards.Scales: the estimates each scale takes.
var Scales = map[string][]string{
	"fibonacci": {"1", "2", "3", "5", "8", "13", "21"},
	"tshirt":    {"XS", "S", "M", "L", "XL"},
	"powers":    {"1", "2", "4", "8", "16"},
	"linear":    {"1", "2", "3", "4", "5"},
}

type Comment struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	Author    string `json:"author"` // a person's id or an API key's name
	CreatedAt string `json:"created_at"`
}

type person struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Name  string `json:"name"`
}

// APIError is the API refusing something, with its reason ("this api key is read-only").
type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string {
	if e.Status == http.StatusUnauthorized {
		return "the API key was refused: check it in ~/.config/tracklines/key"
	}
	return e.Msg
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	err := c.call(ctx, method, path, in, out)
	if reportable(err) {
		route, _, _ := strings.Cut(path, "?") // never the query: it's what someone searched for
		cause := err
		if u, isURL := err.(*url.Error); isURL {
			cause = u.Err // its message repeats the whole URL, query and all
		}
		if c.OnFailure != nil {
			c.OnFailure(method, route, cause)
		}
	}
	return err
}

func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Base, "/")+"/api"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return &APIError{Status: res.StatusCode, Msg: strings.TrimSpace(string(msg))}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// MyWork is the key owner's open tickets on every board and project, as the web app's My work.
func (c *Client) MyWork(ctx context.Context) ([]Ticket, error) {
	return c.search(ctx, url.Values{"assignee": {"me"}, "status": {"open"}, "limit": {"50"}})
}

// MyCreated is the key owner's open tickets that they raised ("Created by me", TL-490).
func (c *Client) MyCreated(ctx context.Context) ([]Ticket, error) {
	return c.search(ctx, url.Values{"creator": {"me"}, "status": {"open"}, "limit": {"50"}})
}

// Search is full-text over titles, descriptions and comments; a ticket key comes first.
func (c *Client) Search(ctx context.Context, q string) ([]Ticket, error) {
	return c.search(ctx, url.Values{"q": {q}, "limit": {"50"}})
}

func (c *Client) search(ctx context.Context, q url.Values) ([]Ticket, error) {
	var out []Ticket
	err := c.do(ctx, http.MethodGet, "/search?"+q.Encode(), nil, &out)
	return out, err
}

func (c *Client) Ticket(ctx context.Context, id string) (Ticket, error) {
	var t Ticket
	err := c.do(ctx, http.MethodGet, "/tickets/"+url.PathEscape(id), nil, &t)
	return t, err
}

// TicketByKey finds a ticket by its key, e.g. TL-308 (any case).
func (c *Client) TicketByKey(ctx context.Context, key string) (Ticket, error) {
	var t Ticket
	err := c.do(ctx, http.MethodGet, "/ticket-keys/"+url.PathEscape(strings.ToUpper(key)), nil, &t)
	return t, err
}

func (c *Client) Board(ctx context.Context, id string) (Board, error) {
	var b Board
	err := c.do(ctx, http.MethodGet, "/boards/"+url.PathEscape(id), nil, &b)
	return b, err
}

// HistoryEntry is one step in a ticket's life (TL-598): a stage it entered or skipped, by whom, when.
type HistoryEntry struct {
	At         string  `json:"at"`      // UTC, RFC 3339
	Stage      string  `json:"stage"`   // backlog | column | resolved
	Outcome    string  `json:"outcome"` // entered | skipped
	BoardName  *string `json:"board_name"`
	ColumnName *string `json:"column_name"`
	Done       bool    `json:"done"`       // the board's last column
	Resolution *string `json:"resolution"` // done | wont_do | duplicate
	Actor      *string `json:"actor"`      // nil: the system did it
}

// History is a ticket's history, oldest first.
func (c *Client) History(ctx context.Context, ticket string) ([]HistoryEntry, error) {
	var out []HistoryEntry
	err := c.do(ctx, http.MethodGet, "/tickets/"+url.PathEscape(ticket)+"/history", nil, &out)
	return out, err
}

func (c *Client) Comments(ctx context.Context, ticket string) ([]Comment, error) {
	var out []Comment
	err := c.do(ctx, http.MethodGet, "/tickets/"+url.PathEscape(ticket)+"/comments", nil, &out)
	return out, err
}

// Attachment is a file on a ticket (uploaded in the web app); URL is where it's served.
type Attachment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	CreatedBy   string `json:"created_by"`
}

// Attachments are a ticket's files (TL-522).
func (c *Client) Attachments(ctx context.Context, ticket string) ([]Attachment, error) {
	var out []Attachment
	err := c.do(ctx, http.MethodGet, "/tickets/"+url.PathEscape(ticket)+"/attachments", nil, &out)
	return out, err
}

func (c *Client) Comment(ctx context.Context, ticket, body string) error {
	return c.do(ctx, http.MethodPost, "/tickets/"+url.PathEscape(ticket)+"/comments", map[string]string{"body": body}, nil)
}

// Move puts the ticket at the bottom of a column, as dragging it there would.
func (c *Client) Move(ctx context.Context, ticket string, col Column) error {
	return c.MoveSized(ctx, ticket, col, "")
}

// MoveSized moves a ticket and, unless estimate is "", sizes it on the way: a backlog ticket going
// onto a board with an estimate scale needs one.
func (c *Client) MoveSized(ctx context.Context, ticket string, col Column, estimate string) error {
	body := map[string]any{"column_id": col.ID, "position": len(col.Tickets)}
	if estimate != "" {
		body["estimate"] = estimate
	}
	return c.do(ctx, http.MethodPost, "/tickets/"+url.PathEscape(ticket)+"/move", body, nil)
}

// Resolution is how a backlog ticket closes in place: done (finished) or wont_do (decided against).
const (
	ResolutionDone   = "done"
	ResolutionWontDo = "wont_do"
)

// Resolve closes a backlog ticket in place as done or wont_do, with an optional reason (a comment the
// creator is told about). Moving it onto a board reopens it.
func (c *Client) Resolve(ctx context.Context, ticket, resolution, reason string) error {
	return c.do(ctx, http.MethodPost, "/tickets/"+url.PathEscape(ticket)+"/resolve", map[string]string{"resolution": resolution, "reason": reason}, nil)
}

func (c *Client) Projects(ctx context.Context) ([]Project, error) {
	var out []Project
	err := c.do(ctx, http.MethodGet, "/projects", nil, &out)
	return out, err
}

// Project is one project with its boards.
func (c *Client) Project(ctx context.Context, id string) (Project, error) {
	var p Project
	err := c.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(id), nil, &p)
	return p, err
}

// Label is one in use in a project's backlog, with how many tickets have it.
type Label struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// BacklogPage is a project's open backlog, sorted and filtered, with the labels in use (for the filter).
type BacklogPage struct {
	Tickets []Ticket `json:"tickets"`
	Labels  []Label  `json:"labels"`
}

// Backlog is the project's open tickets that aren't on a board. sort: manual (the backlog's own
// order), newest or oldest; labels: tickets with any of them. ponytail: the first 100, no paging.
func (c *Client) Backlog(ctx context.Context, project, sort string, labels []string) (BacklogPage, error) {
	q := url.Values{"per_page": {"100"}}
	if sort != "" && sort != "manual" {
		q.Set("sort", sort)
	}
	for _, l := range labels {
		q.Add("label", l)
	}
	var out BacklogPage
	err := c.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(project)+"/backlog/page?"+q.Encode(), nil, &out)
	return out, err
}

// Mentions are tickets that @mention you, newest first, leaving out finished ones.
func (c *Client) Mentions(ctx context.Context) ([]Mention, error) {
	var out struct {
		Mentions []Mention `json:"mentions"`
	}
	err := c.do(ctx, http.MethodGet, "/mentions/page?page=1&per_page=50&hide_done=true", nil, &out)
	return out.Mentions, err
}

// Create makes a ticket at the bottom of a column (joining the board's sprint), or, with no column,
// in the project's backlog.
func (c *Client) Create(ctx context.Context, project, column string, in TicketInput) (Ticket, error) {
	path := "/projects/" + url.PathEscape(project) + "/backlog"
	if column != "" {
		path = "/columns/" + url.PathEscape(column) + "/tickets"
	}
	var t Ticket
	err := c.do(ctx, http.MethodPost, path, in, &t)
	return t, err
}

func (c *Client) Update(ctx context.Context, ticket string, in TicketInput) error {
	return c.do(ctx, http.MethodPatch, "/tickets/"+url.PathEscape(ticket), in, nil)
}

// Assign gives the ticket to someone (an id), or nobody (nil).
func (c *Client) Assign(ctx context.Context, ticket string, to *string) error {
	return c.do(ctx, http.MethodPut, "/tickets/"+url.PathEscape(ticket)+"/assignee", map[string]any{"assignee": to}, nil)
}

// Session is a standup or refinement someone is hosting: who, who's there, and what they have open.
type Session struct {
	Host  string `json:"host"`
	Focus struct {
		TicketID *string `json:"ticketId"`
	} `json:"focus"`
	People []string `json:"people"`
}

// StandupTurn is a running standup's order and whose turn it is.
type StandupTurn struct {
	People []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	} `json:"people"`
	Index        int    `json:"index"`
	RunningSince *int64 `json:"runningSince"` // nil: paused
}

// sessionBase is where a session lives: a board's standup, or a project's refinement.
func sessionBase(kind, id string) string {
	if kind == "standup" {
		return "/boards/" + url.PathEscape(id) + "/live/standup"
	}
	return "/projects/" + url.PathEscape(id) + "/live/refinement"
}

// Session is the one running, or an APIError 404 when there's none.
func (c *Client) Session(ctx context.Context, kind, id string) (Session, error) {
	path := sessionBase(kind, id)
	if kind == "standup" {
		path += "/session"
	}
	var s Session
	err := c.call(ctx, http.MethodGet, path, nil, &s) // not reported: "none running" is the usual answer
	return s, err
}

// Join joins the session, and keeps you in it: the server drops people it hasn't heard from in a minute.
func (c *Client) Join(ctx context.Context, kind, id string) (Session, error) {
	var s Session
	err := c.do(ctx, http.MethodPost, sessionBase(kind, id)+"/join", nil, &s)
	return s, err
}

func (c *Client) Leave(ctx context.Context, kind, id string) error {
	return c.call(ctx, http.MethodPost, sessionBase(kind, id)+"/leave", nil, nil)
}

func (c *Client) Turn(ctx context.Context, board string) (StandupTurn, error) {
	var t StandupTurn
	err := c.call(ctx, http.MethodGet, "/boards/"+url.PathEscape(board)+"/live/standup", nil, &t)
	return t, err
}

// People maps ids to names, so comments show who wrote them, and says which is you (the list's "You").
func (c *Client) People(ctx context.Context) (names map[string]string, me string, err error) {
	var list []person
	if err := c.do(ctx, http.MethodGet, "/assignees", nil, &list); err != nil {
		return nil, "", err
	}
	names = map[string]string{}
	for _, p := range list {
		names[p.ID] = p.Label
		if p.Name != "" {
			names[p.ID] = p.Name
		}
		if p.Label == "You" {
			me = p.ID
		}
	}
	return names, me, nil
}

// reportable is whether an API failure is ours to fix: the server failing or unreachable. Refusals
// (4xx: a read-only key, a locked sprint) are the person's to act on, and they're shown on screen.
func reportable(err error) bool {
	if e, isAPI := err.(*APIError); isAPI {
		return e.Status >= 500
	}
	return err != nil
}

// RunningSession is a standup or refinement being hosted somewhere in the organization.
type RunningSession struct {
	Kind        string `json:"kind"` // standup (on a board) | refinement (on a project)
	ID          string `json:"id"`   // the board's or project's
	Name        string `json:"name"`
	ProjectName string `json:"project_name"`
	Host        string `json:"host"`
}

// RunningSessions is every standup and refinement running in the organization, in one request (TL-466).
func (c *Client) RunningSessions(ctx context.Context) ([]RunningSession, error) {
	var out []RunningSession
	err := c.do(ctx, http.MethodGet, "/live/sessions", nil, &out)
	return out, err
}
