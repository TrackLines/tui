# tracklines

[TrackLin.es](https://tracklin.es) in the terminal: My work, Mentions, projects and their boards and backlogs. You can create, edit, move, assign and comment on tickets without leaving the keyboard. It's built in the spirit of [Omarchy](https://omarchy.org): vim keys, one small binary, and the terminal's own colours, so it follows whatever theme is active.

It's for the everyday work. Settings, API keys, standups and refinement stay in the web app.

## Set up

1. Make a **personal** API key at <https://tracklin.es/account/api-keys>. Leave **Read only** unticked if you want to move and comment; a read-only key can still browse.
2. Save it:

   ```sh
   mkdir -p ~/.config/tracklines
   echo 'tl_p_…' > ~/.config/tracklines/key
   chmod 600 ~/.config/tracklines/key
   ```

   Or set `TRACKLINES_API_KEY` instead.
3. Download it from [Releases](https://github.com/tracklines/tui/releases): `tracklines-linux-amd64` (x86) or `tracklines-linux-arm64` (arm), with `SHA256SUMS` to check it:

   ```sh
   curl -fLo ~/.local/bin/tracklines https://github.com/tracklines/tui/releases/latest/download/tracklines-linux-amd64
   chmod +x ~/.local/bin/tracklines
   ```

   On Windows or macOS, or to build it yourself (Go 1.26+): `make install`.

   Errors on our side (the API failing or unreachable, a crash) are reported to [Bugfixes](https://bugfix.es), so they get fixed. Reports never include your API key, what you searched for, or what you wrote. Set `TRACKLINES_NO_REPORTS=1` to turn reporting off. The local copy of each one goes to `~/.local/state/tracklines/tracklines.log`.

4. On Omarchy, add it to the app launcher:

   ```sh
   omarchy-tui-install TrackLines tracklines float https://tracklin.es/icon.png
   ```

## Keys

Everywhere: `1` My work (with Mentions) · `2` Projects · `/` search everything · `t` go to a ticket by its key (`TL-308` or `tl-308`) · `esc` or `q` back (on My work and Projects, `q` quits) · `Q` quit from anywhere.

| Screen | Keys |
|---|---|
| My work | `tab` switches between My work and Mentions, below it |
| Lists (My work, Mentions, search, backlog) | `j`/`k` move · `g`/`G` top/bottom · `enter` open · `r` reload |
| Backlog | also `n` new ticket · `s` sort (backlog order, newest, oldest) · `f` filter by labels |
| Label filter | `j`/`k` · `space` toggle · `c` clear · `enter` apply · `esc` cancel |
| Projects | `j`/`k` · `enter` opens a project: its boards, then its backlog |
| Board | `h`/`l` column · `j`/`k` ticket · `enter` open · `H`/`L` move the ticket a column left/right · `n` new ticket in this column · `r` reload |
| Ticket | `j`/`k` scroll · `e` edit · `m` move to a column · `c` comment · `a` assign to you (again: unassign) · `r` reload |
| New / edit ticket | Typing is insert mode; `esc` leaves it. Then `tab` or `j`/`k` field · `i`/`enter` edit · `h`/`l` change type, priority, estimate · `q` cancel · `ctrl+s` save (any time) |
| Comment | Like vim: `esc` normal mode · `i a I A o O` insert · `h j k l w b 0 $ g G` move · `x`, `dd` delete · `enter` or `ctrl+s` send · `q` cancel (twice if there's text) |

- **My work** is your open tickets on every board and project, the same list as the web app.
- **Mentions**, under My work, are tickets that @mention you, leaving out finished ones.
- A search takes words or a ticket key such as `TL-123`.
- Boards that use estimates ask for one when you create a ticket there.
- Backlog tickets are put on a board in the web app, where the sprint capacity checks are.

## Settings

| Variable | Default |
|---|---|
| `TRACKLINES_API_KEY` | read from `~/.config/tracklines/key` |
| `TRACKLINES_API_URL` | `https://api.tracklin.es` |

## Development

```sh
go test ./...
```

CI (`.github/workflows/ci.yml`) runs vet and the tests on every push, and builds static Linux binaries for amd64 and arm64. Pushing a `v*` tag (e.g. `git tag v0.1.0 && git push --tags`) publishes them as a GitHub release.
