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

Everywhere: `1` My work (with Mentions) · `2` Projects · `3`–`5` your most used projects · `/` search everything · `t` go to a ticket by its key (`TL-308` or `tl-308`) · `esc` or `q` back (on My work and Projects, `q` quits) · `Q` quit from anywhere · `?` your version, updates and these keys.

| Screen | Keys |
|---|---|
| My work | `tab` switches between My work and Mentions, below it |
| Lists (My work, Mentions, search, backlog) | `j`/`k` move · `g`/`G` top/bottom · `h`/`l` slide the selected title along to read the rest (`0` start, `$` end) · `enter` open · `r` reload |
| Backlog | also `n` new ticket · `s` sort (backlog order, newest, oldest) · `f` filter by labels |
| Label filter | `j`/`k` · `space` toggle · `c` clear · `enter` apply · `esc` cancel |
| Projects | `j`/`k` · `enter` opens a project: its boards, then its backlog |
| Board | `h`/`l` column · `j`/`k` ticket · `enter` open · `H`/`L` move the ticket a column left/right · `n` new ticket in this column · `r` reload |
| Ticket | `j`/`k` scroll · `e` edit · `m` move to a column · `c` comment · `a` assign to you (again: unassign) · `tab` pick a sub-ticket, `enter` open it · `p` open the parent · `r` reload |
| New / edit ticket | Typing is insert mode; `esc` leaves it. Then `tab` or `j`/`k` field · `i`/`enter` edit · `h`/`l` change type, priority, estimate · `q` cancel · `ctrl+s` save (any time) |
| Board / project | `f` follows its standup / refinement, when one's running (the banner says so) |
| Following | it moves with the host: who's here, whose turn, the ticket they have open · `esc`/`q` leave |
| Comment | Like vim: `esc` normal mode · `i a I A o O` insert · `h j k l w b 0 $ g G` move · `x`, `dd` delete · `:w`, `:wq`, `enter` or `ctrl+s` send · `:q`, `q` cancel (refused/twice over text) · `:q!` discard. With `keys = standard`: `ctrl+s` send, `esc` cancel |

- **My work** is your open tickets on every board and project, the same list as the web app.
- **Tabs 3–5** are the projects you open most, so you can jump straight in. The TUI keeps the count on your machine only, in `~/.local/state/tracklines/projects.json`, and the tabs reorder when you go back to My work or Projects, never while you're on one.
- **Mentions**, under My work, are tickets that @mention you, leaving out finished ones.
- A search takes words or a ticket key such as `TL-123`.
- Boards that use estimates ask for one when you create a ticket there.
- Backlog tickets are put on a board in the web app, where the sprint capacity checks are.

## Config

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

`?` shows which is in use.

## Environment

Everything else is in the config. These two stay as environment variables: the key is a secret, and the address is for pointing the TUI at another server (a test one).

| Variable | Default |
|---|---|
| `TRACKLINES_API_KEY` | read from `~/.config/tracklines/key` |
| `TRACKLINES_API_URL` | `https://api.tracklin.es` |

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
| `internal/ui` | the screens: lists, boards, tickets, the forms and vim editing, following sessions, `?` |

CI (`.github/workflows/ci.yml`) runs vet and the tests on every push and pull request. A `v*` tag, from making a release on GitHub or `git tag v0.1.0 && git push --tags`, also builds static Linux binaries for amd64 and arm64 and attaches them to the release.
