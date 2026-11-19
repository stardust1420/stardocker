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
docker run -d --name stardocker-test alpine:3.20 sh -c 'i=0; while true; do i=$((i+1)); echo "tick $i"; sleep 2; done'   # test container that keeps logging; remove with docker rm -f stardocker-test
```

Unit tests live in `src/*_test.go` (`go test ./src/...`; one test: `go test ./src/... -run TestName`). They cover only pure helpers; anything that calls Docker needs the daemon.
The TUI needs a real TTY, so Claude can't drive it. Check changes with build + vet, then have the user run `! go run .`.

## Architecture

- `main.go` creates a `github.com/docker/go-sdk/client` `SDKClient`, which is what the whole TUI uses. `client.New` resolves the host from `DOCKER_HOST` or the current Docker context (Colima registers a `colima` context) and runs a health-check ping. If that fails, `main.go` runs `colima start` (blocking; VM settings such as `--vm-type vz` live in the user's Colima config, not here) and retries once. The daemon is expected to be Colima, not Docker Desktop. Container API types still come from `github.com/docker/docker/api/types/container`.
- All screens live in package `src` and are independent `tea.Model`s. **Navigation works by returning a different model** from `Update` (e.g. `return InitLogsModel(...), l.Init()`); there's no router or parent model. Each screen builds its destination with an `Init*Model(dockerClient, ...)` constructor, and `esc` builds a fresh instance of the previous screen. Screen state such as the expanded compose stacks is therefore lost when you navigate away.
  - `index.go`: home screen with one card per entry in `menuCards` (Containers, Images, Exit), indexed by the `card*` constants. It has its own `indexKeys` keymap (←/→/h/l/tab to move, `enter` or `1`–`3` to open) and refreshes the card counts with `fetchSummaryCmd` on its tick loop.
  - `containers.go`: the containers table. `FetchContainers` groups containers by the `com.docker.compose.project` label into synthetic `compose_stack` rows. Stack rows show a summary (service count, all published ports, `n/m running`, state `running`/`partial`/`exited`), a ▸/▾ marker and no ID. Children are rendered only when the stack name is in `ShowChildrenSet`.
  - `logs.go`: a viewport over `ContainerLogs` (last 24h, with timestamps), demuxed with `stdcopy` unless the container has a TTY. It refetches the full log on each tick and keeps following the tail if the view was at the bottom.
  - `images.go`: the images table, refreshed every tick. `FetchImages` lists one row per tag (untagged images show as `<none>`, sorted last) and counts the containers (running or stopped) created from each image.
  - `help.go`: the shared `keyMap` (`keys`) used by `help.Model`, plus `containersKeys`, which adds `enter` and `r` for the containers screen. When you add a key to a screen, add it to that screen's keymap so it appears in the help.
  - `layout.go`: the palette (`color*`: orange is brand and focus, steel blue is the selected row, green is running) and the shared list-screen layout. `renderHeader` draws the breadcrumb and counts, `renderPanel` the details box with its title in the border, and `renderBottomBar` the help and status. `listTableHeight` makes a list screen fill the window exactly, so call `layout()` after anything that changes the help height. Use `newHelp()` instead of `help.New()`. State glyphs are ● running, ◐ partial/paused, ○ stopped.
  - Inside table cells, use glyphs, not colours: per-cell ANSI codes reset the selected row's background, and the table truncates cells without knowing about ANSI. `styles.go`: shared Lip Gloss styles and `tableStyles()` for every table screen.
- **Refresh loop:** each model that refreshes gets an id from `newTickID()`, starts its own loop in `Init()` with `tickCmd(id)`, and ignores any `tickMsg` whose id isn't its own. A tick still in flight from the previous screen therefore dies instead of stacking up a second loop.
- **Window size:** Bubble Tea sends `tea.WindowSizeMsg` only at startup and on resize, so every `Init*Model` constructor takes `width, height` from the screen it came from. Pass them along when you add a screen.
- **Rows vs. data:** table cells are display-only. `getRows` returns the rows plus a parallel `items` slice (the `Container` for each row), and actions use `selected()` (`items[cursor]`). The images screen does the same with `[]Image`. Keep the two slices in step if you change how rows are built.
- **Column widths:** column sets are `[]colSpec` (`containerColumns`, `imageColumns`). `fitColumns` (`src/columns.go`) sizes each column to its content and terminal width, shrinking by `shrinkOrder` (Ports shrinks last). It runs on every refresh and on resize. Spare width goes to columns with `grow`. Cells that still don't fit are truncated with `…`, so the details panel (`detailsBody` in `src/details.go`) under each table shows the selected row in full.
- Models use value receivers, so mutations inside `Init()` are discarded. The containers table fills on an immediate first tick fired from `Init()`.
- Docker errors in the containers and logs screens are shown in the UI (a status line or the log text). Start/stop run as async `tea.Cmd`s that return `containerActionMsg`.

## Keybindings (from README)

`↑/k`, `↓/j` move · `Enter` selects, expands a compose stack or opens logs · `Esc` goes back · `r` starts or stops the selected container (stops `running`, starts `exited`/`created`) · `q`/`ctrl+c` quit · `?` toggles help.
