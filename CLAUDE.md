# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

StarDocker is a Go terminal UI (Bubble Tea + Bubbles + Lip Gloss) for managing local Docker containers: list containers, start/stop them, group Docker Compose stacks, and tail logs. Module path: `github.com/stardust1420/stardocker` (Go 1.24).

## Commands

```bash
go build .        # produces ./stardocker (binary is gitignored)
go run .          # run directly; needs a reachable Docker daemon
go vet ./...
gofmt -l .
docker run -d --name stardocker-test alpine:3.20 sh -c 'i=0; while true; do i=$((i+1)); echo "tick $i"; sleep 2; done'   # test container that keeps logging; remove with docker rm -f stardocker-test. `sh` ignores SIGTERM, so stopping it takes ~10s and it exits with 137
```

Unit tests live in `src/*_test.go` (`go test ./src/...`; one test: `go test ./src/... -run TestName`). They cover only pure helpers; anything that calls Docker needs the daemon.
The TUI needs a real TTY, so Claude can't drive it. Check changes with build + vet, then have the user run `! go run .`.
To see a screen without a TTY, add a temporary `src/zz_*_tmp_test.go` that builds the model with a real `client.New`, sends it `tea.KeyMsg`s, runs any returned `tea.Cmd` and feeds the result back, then prints `View()` with ANSI codes stripped (`sed 's/\x1b\[[0-9;]*m//g'`). Delete it afterwards.

## Architecture

- `main.go` creates a `github.com/docker/go-sdk/client` `SDKClient`, which is what the whole TUI uses. `client.New` resolves the host from `DOCKER_HOST` or the current Docker context (Colima registers a `colima` context) and runs a health-check ping. If that fails, `main.go` runs `colima start` (blocking; VM settings such as `--vm-type vz` live in the user's Colima config, not here) and retries once. The daemon is expected to be Colima, not Docker Desktop. Container API types still come from `github.com/docker/docker/api/types/container`.
- All screens live in package `src` and are independent `tea.Model`s. **Navigation works by returning a different model** from `Update` (e.g. `return InitLogsModel(...), l.Init()`); there's no router or parent model. Each screen builds its destination with an `Init*Model(dockerClient, ...)` constructor, and `esc` usually builds a fresh instance of the previous screen, losing its state. Going back to the home screen selects the card you came from (`withSelected`). The other exception is the logs view: it keeps the containers screen it came from (`logsModel.back`) and returns it on `esc` through `resume()`, which gives it a new tick id and the current size, so the cursor and expanded stacks survive. List screens keep the cursor on the same item across refreshes (`itemKey` / `imageRef`), not on the same row number.
  - `index.go`: home screen with one card per entry in `menuCards` (Containers, Images, Exit), indexed by the `card*` constants. It has its own `indexKeys` keymap (←/→/h/l/tab to move, `enter` or `1`–`3` to open) and refreshes the card counts with `fetchSummaryCmd` on its tick loop.
  - `containers.go`: the containers table. `FetchContainers` groups containers by the `com.docker.compose.project` label into synthetic `compose_stack` rows. Stack rows show a summary (service count, all published ports, `n/m running`, state `running`/`partial`/`exited`), a ▸/▾ marker and no ID. Children are rendered only when the stack name is in `ShowChildrenSet`.
  - `logs.go`: a viewport over `ContainerLogs` (last `hours`, default 1, which `t` changes through the small `numberField`; it isn't bubbles `textinput`, which would pull in a clipboard dependency), with timestamps, demuxed with `stdcopy` unless the container has a TTY. It refetches the full log on each tick and keeps following the tail if the view was at the bottom. Each tick also inspects the container to show its state in the header (`describeContainerState`). If the container is deleted while open, the last logs stay on screen. `c` clears the view by setting `clearedAfter` to the timestamp of the last line shown, and later fetches pass it as `Since`. It uses the daemon's timestamp rather than the local clock because the Colima VM's clock can drift; `a` resets it. `s` starts or stops the container through the same `planStartStop` / `startStopCmd` as the containers screen.
  - `images.go`: the images table, refreshed every tick. `FetchImages` lists one row per tag (untagged images show as `<none>`, sorted last) and counts the containers (running or stopped) created from each image.
  - `help.go`: the shared `keyMap` (`keys`) used by `help.Model`, plus `containersKeys`, which adds `enter`, `s` and `d` for the containers screen. When you add a key to a screen, add it to that screen's keymap so it appears in the help.
  - `layout.go`: the palette (`color*`: orange is brand and focus, steel blue is the selected row, green is running) and the shared list-screen layout. `renderHeader` draws the breadcrumb and counts, `renderPanel` the details box with its title in the border, and `renderBottomBar` the help and status. `listTableHeight` makes a list screen fill the window exactly, so call `layout()` after anything that changes the help height. Use `newHelp()` instead of `help.New()`. State glyphs are ● running, ◐ partial/paused, ○ stopped.
  - `starfield.go`: the animated sky behind the home screen. Three parallax layers drift left, each star twinkles on its own phase, and a shooting star comes every 8–20 s. It runs at about 10 fps on `frameMsg` ticks tagged with the home screen's `tickID`, so it stops when you leave. `compose` keeps a clear margin around the centred content and returns "" if the page doesn't fit, in which case the screen falls back to `lipgloss.Place`. Setting `STARDOCKER_REDUCED_MOTION=1` freezes it.
  - `confirm.go`: `confirmPrompt` holds a pending destructive action. The action is built when the prompt opens, so later refreshes can't change its target. While a prompt is open the screen handles only `y`/`n`/`esc`, and the details panel is replaced by a red confirmation box. `statusLine` is the bottom-right message; successes clear themselves via `clearStatusMsg`. Delete rules: running containers are force-removed after the prompt warns about it, and volumes are never removed. An image that containers still use can't be deleted, but one of its extra tags can be removed.
  - Inside table cells, use glyphs, not colours: per-cell ANSI codes reset the selected row's background, and the table truncates cells without knowing about ANSI. `styles.go`: shared Lip Gloss styles and `tableStyles()` for every table screen.
- **Refresh loop:** each model that refreshes gets an id from `newTickID()`, starts its own loop in `Init()` with `tickCmd(id)`, and ignores any `tickMsg` whose id isn't its own. A tick still in flight from the previous screen therefore dies instead of stacking up a second loop.
- **Window size:** Bubble Tea sends `tea.WindowSizeMsg` only at startup and on resize, so every `Init*Model` constructor takes `width, height` from the screen it came from. Pass them along when you add a screen.
- **Rows vs. data:** table cells are display-only. `getRows` returns the rows plus a parallel `items` slice (the `Container` for each row), and actions use `selected()` (`items[cursor]`). The images screen does the same with `[]Image`. Keep the two slices in step if you change how rows are built.
- **Column widths:** column sets are `[]colSpec` (`containerColumns`, `imageColumns`). `fitColumns` (`src/columns.go`) sizes each column to its content and terminal width, shrinking by `shrinkOrder` (Ports shrinks last). It runs on every refresh and on resize. Spare width goes to columns with `grow`. Cells that still don't fit are truncated with `…`, so the details panel (`detailsBody` in `src/details.go`) under each table shows the selected row in full.
- Bubbles' table and viewport keymaps already bind `d`/`u` (half page), `f`/`b`/`space` (page) and `j`/`k`. A screen that adds one of these keys must handle it and `return` before passing the message to `table.Update` or `viewport.Update`.
- Models use value receivers, so mutations inside `Init()` are discarded. The containers table fills on an immediate first tick fired from `Init()`.
- Docker errors in the containers and logs screens are shown in the UI (a status line or the log text). Start/stop run as async `tea.Cmd`s that return `containerActionMsg`.

## Keybindings (from README)

`↑/k`, `↓/j` move · `Enter` selects, expands a compose stack or opens logs · `Esc` goes back · `s` starts or stops the selected container (stops `running`, starts `exited`/`created`); on a compose stack row it stops every running service, or starts them all if none is running (`planStartStop`) · `d` deletes the selected container, stack, image or tag after a `y`/`n` confirmation · `q`/`ctrl+c` quit · `?` toggles help.
