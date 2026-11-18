# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

StarDocker is a Go terminal UI (Bubble Tea + Bubbles + Lip Gloss) for managing local Docker containers: list containers, start/stop them, group Docker Compose stacks, and tail logs. Module path: `github.com/stardust1405/stardocker` (Go 1.24).

## Commands

```bash
go build .        # produces ./stardocker (binary is gitignored)
go run .          # run directly; needs a reachable Docker daemon
go vet ./...
gofmt -l .
```

There are no tests in the repo yet. If adding them, `go test ./src/... -run TestName` runs a single test.

## Architecture

- `main.go` creates a `github.com/docker/go-sdk/client` `SDKClient`, which is what the whole TUI uses. `client.New` resolves the host from `DOCKER_HOST` or the current Docker context (Colima registers a `colima` context) and runs a health-check ping. If that fails, `main.go` runs `colima start` (blocking; VM settings such as `--vm-type vz` live in the user's Colima config, not here) and retries once. The daemon is expected to be Colima, not Docker Desktop. Container API types still come from `github.com/docker/docker/api/types/container`.
- All screens live in package `src` and are independent `tea.Model`s. **Navigation works by returning a different model** from `Update` (e.g. `return InitLogsModel(...), l.Init()`); there's no router or parent model. Each screen builds its destination with an `Init*Model(dockerClient, ...)` constructor, and `esc` builds a fresh instance of the previous screen. Screen state such as the expanded compose stacks is therefore lost when you navigate away.
  - `index.go`: main menu. It dispatches on the selected row's label text ("Containers", "Images", "Exit").
  - `containers.go`: the containers table. `FetchContainers` groups containers by the `com.docker.compose.project` label into synthetic `compose_stack` rows. These get a random ID from `primitive.NewObjectID()`, which is the only reason the mongo-driver dependency exists. Children are rendered only when the stack name is in `ShowChildrenSet`.
  - `logs.go`: a viewport over `ContainerLogs` (last 24h, with timestamps). It refetches the full log on each tick and auto-scrolls when the view is more than 90% scrolled.
  - `images.go`: a cursor-based list that is still mostly a stub (the image detail view is commented out).
  - `help.go`: the shared `keyMap` (`keys`) used by `help.Model`. `styles.go`: shared Lip Gloss styles.
- **Refresh loop:** `tickCmd()` emits `tickMsg` every 1s. The containers model refetches all containers on every tick. `logsModel.Init()` returns `nil`, so the logs view only refreshes because the tick already scheduled by the containers view is delivered to the new model, which then re-arms it. Keep that in mind if you change how models are swapped.
- **Row-index coupling:** container actions read data back out of the selected table row by column position, using `ContainerNameIndex`, `ContainerIDIndex`, `ContainerStateIndex` and `ContainerTypeIndex`. Child rows are prefixed with two spaces, so values are `strings.TrimSpace`d. If you add, remove or reorder columns in `InitListContainersModel`/`getRows`, update these constants too.
- Models use value receivers, so mutations inside `Init()` (such as `SetRows` in `listContainersModel.Init`) are discarded. The table actually fills on the first tick.
- Docker API errors currently `panic` rather than being surfaced in the UI.

## Keybindings (from README)

`↑/k`, `↓/j` move · `Enter` selects, expands a compose stack or opens logs · `Esc` goes back · `r` starts or stops the selected container (only `running`↔`exited`) · `q` quits · `?` toggles help.
