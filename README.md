# tracklines

[TrackLin.es](https://tracklin.es) in the terminal: My work, Mentions, projects and their boards and backlogs. You can create, edit, move, assign and comment on tickets without leaving the keyboard. It's built in the spirit of [Omarchy](https://omarchy.org): vim keys, one small binary, and the terminal's own colours, so it follows whatever theme is active.

It's for the everyday work. You can follow a standup or refinement someone's running, but starting and hosting them, settings and API keys stay in the web app.

## Set up

1. Make a **personal** API key at <https://tracklin.es/account/api-keys>. Leave **Read only** unticked if you want to move and comment; a read-only key can still browse.
2. Save it:

   ```sh
   mkdir -p ~/.config/tracklines
   echo 'tl_p_…' > ~/.config/tracklines/key
   chmod 600 ~/.config/tracklines/key
   ```

   Or set `TRACKLINES_API_KEY` instead.
3. Download it from [Releases](https://github.com/tracklines/tui/releases), with `SHA256SUMS` to check it:
   - Linux: `tracklines-linux-amd64` (x86) or `tracklines-linux-arm64` (arm).
   - macOS: `tracklines-darwin-arm64` (Apple silicon) or `tracklines-darwin-amd64` (Intel).

   For example, on Linux x86:

   ```sh
   curl -fLo ~/.local/bin/tracklines https://github.com/tracklines/tui/releases/latest/download/tracklines-linux-amd64
   chmod +x ~/.local/bin/tracklines
   ```

   On macOS, use `tracklines-darwin-arm64` or `-amd64` in that URL, into a folder on your `PATH` such as `/usr/local/bin`. The binary isn't signed: downloaded with `curl` it runs as is, but if you download it in a browser, macOS blocks it until you run `xattr -d com.apple.quarantine` on it.

   On Windows, or to build it yourself (Go 1.26+): `make install`, or `go install github.com/tracklines/tui/cmd/tracklines@latest`.

   **Updates:** a release build checks GitHub for a newer release when it starts. If there is one, the header says so, and `?` then `u` installs it: it downloads the binary, checks it against `SHA256SUMS`, and replaces the one you're running. Restart to use it. `update_check = off` in the config turns the check off. A build of your own (`make install`) doesn't check.

   Errors on our side (the API failing or unreachable, a crash) are reported to [Bugfixes](https://bugfix.es), so they get fixed. Reports never include your API key, what you searched for, or what you wrote. `reports = off` in the config turns reporting off. The local copy of each one goes to `~/.local/state/tracklines/tracklines.log`.

4. On Omarchy, add it to the app launcher:

   ```sh
   omarchy-tui-install TrackLines tracklines float https://tracklin.es/icon.png
   ```

## Keys

Everywhere: `1` My work (with Mentions) · `2` Projects · `3`–`5` your most used projects · `/` search everything · `t` go to a ticket by its key (`TL-308` or `tl-308`) · `D` unsent drafts · `esc` or `q` back (on My work and Projects, `q` quits) · `Q` quit from anywhere · `?` your version, updates and these keys; `h` there opens the help answers.

| Screen | Keys |
|---|---|
| My work | `tab` switches between My work and Mentions, below it |
| Lists (My work, Mentions, search, backlog) | `j`/`k` move · `g`/`G` top/bottom · `h`/`l` slide the selected title along to read the rest (`0` start, `$` end) · `enter` open · `r` reload |
| Backlog | also `m` move the selected ticket to a board · `n` new ticket · `s` sort (backlog order, newest, oldest) · `f` filter by labels |
| Combined filters (`F` on a ticket list) | `tab`/`shift+tab` field · `enter` apply · `ctrl+x` clear fields · `esc` cancel. On a board, `F` opens its tickets as a list; press `F` again to edit filters. |
| Saved views (`V`) | `j`/`k` select · `enter` reopen · `s` save the current list and filters · `r` rename · `d` delete · `esc`/`q` back. `s` is available when opened from a ticket list, My work section or board. |
| Label filter | `j`/`k` · `space` toggle · `c` clear · `enter` apply · `esc` cancel |
| Projects | `j`/`k` · `enter` opens a project: its boards, then its backlog |
| Project page | `j`/`k` select a board or Backlog · `enter` open · `B` (Shift+B) jump straight to Backlog |
| Board | `h`/`l` column · `j`/`k` ticket · `enter` open · `H`/`L` move the ticket a column left/right · `n` new ticket in this column · `r` reload. The selected ticket's full title shows under the board. |
| Ticket | `j`/`k` scroll · `e` edit · `m` move to a column · `c` comment · `a` assign to you (again: unassign) · `tab` pick a sub-ticket or file, `enter` open it · `o` open a picked file in your system's viewer (`xdg-open`; `open` on macOS) · `u` attach a file · `d` remove the picked file (asks first; yours, or any if you're an org admin) · `p` open the parent · `h` its history (everything that happened to it — columns, assignment, edits, labels, comments, files, pull requests — who did it and when; newest first. Edits to text say only that they happened) · `r` reload |
| New / edit ticket | Typing is insert mode; `esc` leaves it. Then `tab` or `j`/`k` field · `i`/`enter` edit · `h`/`l` change type, priority, estimate, and spike timebox · on the due date: `h`/`l` a day, `H`/`L` a week, `x` clears · `q` cancel · `ctrl+s` save (any time) · `ctrl+o` create and attach a file (new tickets) |
| Attach a file (`u`) | type or paste a path, or drop a file on the terminal · `tab` completes it (lists the choices when there are several) · `enter` uploads it with its progress · `esc` cancels the upload, or goes back. Up to 10 MB a file and 10 files a ticket; not on a done ticket. The file goes through the TrackLin.es API, so the TUI never needs the file store's key. |
| Drafts (`D`) | `j`/`k` · `r` send again · `x` discard · `enter` open its ticket (`c` or `e` there picks the draft up to change it first) |
| Board / project | `f` follows its standup / refinement, when one's running (the banner says so) |
| Following | it moves with the host: who's here, whose turn, the ticket they have open · `esc`/`q` leave |
| Help (`?` then `h`) | type to search the answers · `↑`/`↓` pick · `enter` opens its page on the website (for the project or board you came from) · `esc` back |
| Comment | Like vim: `esc` normal mode · `i a I A o O` insert · `h j k l w b 0 $ g G` move · `x`, `dd` delete · `:w`, `:wq`, `enter` or `ctrl+s` send · `:q`, `q` cancel (refused/twice over text) · `:q!` discard. With `keys = standard`: `ctrl+s` send, `esc` cancel |

- **My work** is your open tickets on every board and project, the same list as the web app.
- **Tabs 3–5** are the projects you open most, so you can jump straight in. The TUI keeps the count on your machine only, in `~/.local/state/tracklines/projects.json`, and the tabs reorder when you go back to My work or Projects, never while you're on one.
- **Mentions**, under My work, are tickets that @mention you, leaving out finished ones.
- A search takes words or a ticket key such as `TL-123`. Results say which project, board and column each ticket is in; `/` from the results starts from the same words, and `r` retries a search that failed.
- Boards that use estimates ask for one when you create a ticket there.
- **Due dates** count down by day, in your local day: "due in 3 days", "due tomorrow", "due today", then "2 days overdue" (red). They show in lists and on the ticket, and go once it's done.
- Choose `spike` to set an optional timebox in days (0–60) and hours (0–23); `not set` is available for either. Changing to another type clears the timebox.
- **Drafts.** A comment or an edit that can't be sent (the API down, the connection gone) isn't lost: it's kept in `~/.local/state/tracklines/drafts.json` (under `$XDG_STATE_HOME` if set) until it's sent or you discard it, through restarts too, and the TUI says how many are waiting when it starts. Nothing is resent on its own: `D` lists them and `r` sends one again. A comment that got through before the connection dropped isn't posted twice, and an edit to a ticket someone has changed since isn't sent over their changes: open it and `e` shows your edit with a warning, to compare and save if you still want it.
- **Help answers** are the ones in the web app's help panel, written by the TrackLin.es team. They're built in, so help works offline, and the TUI checks for newer ones when it starts and every minute after, swapping them in without a restart. The latest copy is kept in `~/.local/state/tracklines/help.<language>.json` for the next start.
- `m` on a backlog ticket picks a board, then a column, then an estimate if that board sizes tickets. When a move isn't allowed (a read-only key, a sprint on its last day), the TUI says why.

## Config

### Saved filters

On a ticket list, `F` combines project and board (name or ID), status (`open`, `done`, or a column name), type, priority, assignee (`me`, `unassigned`, or a user ID) and labels. Dimensions combine with AND; every comma-separated label must match. Blank fields leave that dimension unrestricted. The header shows active filters; `ctrl+x` on the list clears both combined filters and the existing backlog label filter. Empty filtered sections remain visible so you can clear them.

Filters narrow the source list's fetched tickets: My work and Created by me contain open tickets, Mentions contains unfinished mentions, search currently returns up to 50 results, and backlog up to 100. They don't turn a list into an organization-wide search. A board opened as a list includes all of its returned columns, including Done.

Press `V`, then `s`, and enter a unique name to save a view. Reopening fetches fresh tickets from the original source, retaining its query, board/project, sort and filters. Presets live in `$XDG_CONFIG_HOME/tracklines/filters.json` (normally `~/.config/tracklines/filters.json`), alongside the existing config. They stay on this machine; credentials aren't stored in presets. Renames and deletions persist immediately, and failed saves are reported without discarding the name you typed.

`?` includes the previous screen's shortcuts alongside global keys. Navigation shortcuts stay inactive while typing in filter fields, preset names, searches and ticket editors, with either Vim or standard editing enabled.

### Settings

`~/.config/tracklines/config`, one `name = value` per line (`#` starts a comment). The repo's [`config.example`](config.example) has every setting at its default, explained:

```sh
mkdir -p ~/.config/tracklines && cp config.example ~/.config/tracklines/config
```


| Name | Values |
|---|---|
| `keys` | `vim` (the default): writing a ticket or comment works like vim. Typing is insert mode, `esc` is normal mode, and `:w` / `:wq` save, `:q` leaves (refused over unsaved text), `:q!` discards. `standard`: no modes; `ctrl+s` saves and `esc` cancels (twice over unsaved text). `ctrl+s` saves with either. |
| `backlog_sort` | The order a backlog opens in: `manual` (the backlog's own order, the default), `newest` or `oldest`. `s` still cycles from there. |
| `reports` | `on` (the default) sends the TUI's own failures to Bugfixes; `off` doesn't. Never your key, searches or what you wrote. |
| `update_check` | `on` (the default): a release build looks for a newer release when it starts. `off` doesn't. |
| `language` | `system` (the default): your locale (`LC_ALL`, `LC_MESSAGES`, `LANG`), when it's a language the TUI has, else English. `en` English, `nl` Nederlands, `de` Deutsch, `es` Español, `pt` Português (Brasil), `fr` Français, `tlh` tlhIngan Hol (Klingon, for fun). Only the TUI's own words change: tickets, comments, names and keys stay as written. |

`?` shows which is in use.

## Environment

Everything else is in the config. These two stay as environment variables: the key is a secret, and the address is for pointing the TUI at another server (a test one).

| Variable | Default |
|---|---|
| `TRACKLINES_API_KEY` | read from `~/.config/tracklines/key` |
| `TRACKLINES_API_URL` | `https://api.tracklin.es` |

## Translations

The TUI's text is English in the code, wrapped in `T` (a text) or `F` (a format); each language has a catalog in `internal/i18n` (`nl.go`, `de.go`, …). Another language is one more catalog file there and a line in `catalogs`. `go test ./internal/i18n` fails on any text that's shown without `T`/`F` or missing from a catalog, so nothing slips through untranslated.

## Development

```sh
go test ./...
```

| Where | What |
|---|---|
| `cmd/tracklines` | `main`: reads the key and config, wires the parts together and runs the app |
| `internal/api` | the TrackLin.es API client and its types |
| `internal/config` | the API key, the config file, and the config and state folders |
| `internal/reporting` | sending the TUI's own failures to Bugfixes |
| `internal/update` | checking for and installing a newer release |
| `internal/usage` | which projects you open most (tabs 3–5) |
| `internal/help` | the help answers: built in from `baked/*.po`, then kept up to date from the API. `go generate ./internal/help` fetches the backend's `.po` export again (`TRACKLINES_API_URL` for staging) |
| `internal/ui` | the screens: lists, boards, tickets, the forms and vim editing, following sessions, `?` |

CI (`.github/workflows/ci.yml`) runs vet and the tests on every push and pull request. A `v*` tag, from making a release on GitHub or `git tag v0.1.0 && git push --tags`, also builds static Linux binaries for amd64 and arm64 and attaches them to the release.
