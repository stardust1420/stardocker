<div align="center">

```
███████╗████████╗ █████╗ ██████╗ ██████╗  ██████╗  ██████╗██╗  ██╗███████╗██████╗
██╔════╝╚══██╔══╝██╔══██╗██╔══██╗██╔══██╗██╔═══██╗██╔════╝██║ ██╔╝██╔════╝██╔══██╗
███████╗   ██║   ███████║██████╔╝██║  ██║██║   ██║██║     █████╔╝ █████╗  ██████╔╝
╚════██║   ██║   ██╔══██║██╔══██╗██║  ██║██║   ██║██║     ██╔═██╗ ██╔══╝  ██╔══██╗
███████║   ██║   ██║  ██║██║  ██║██████╔╝╚██████╔╝╚██████╗██║  ██╗███████╗██║  ██║
╚══════╝   ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝  ╚═════╝  ╚═════╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝
```

**A sleek terminal UI to manage your Docker containers**

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![Docker](https://img.shields.io/badge/Docker-Engine-2496ED?style=flat-square&logo=docker&logoColor=white)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)

</div>

---

## ✨ Features

- 📦 **List all containers** — view both running and stopped containers at a glance
- ▶️ **Start & Stop** — toggle container state directly from the TUI
- 🗑️ **Delete** — remove containers, whole Compose stacks, images or image tags, always behind a confirmation
- 📜 **Live Logs** — stream container logs in real-time with a scrollable viewport
- 🗂️ **Compose-aware** — automatically groups Docker Compose stacks with expandable children
- 🖼️ **Browse Images** — see local images with their size, age and which containers use them
- 🚀 **Auto-starts Docker** — detects if the Docker daemon is down and runs `colima start` for you
- ⌨️ **Keyboard-driven** — fast, intuitive navigation with vim-style keybindings

---

## 📸 How It Works

StarDocker connects directly to your local Docker daemon via the Docker SDK. It renders an interactive table of your containers with live state indicators (`●` running, `◐` partly running, `○` stopped) so you can see what's running at a glance — then lets you drill into logs or control containers without ever leaving the terminal.

---

## 🛠️ Installation

### Prerequisites

- [Go 1.24+](https://go.dev/dl/)
- A Docker daemon reachable via `DOCKER_HOST` or the current Docker context. On macOS this is [Colima](https://github.com/abiosoft/colima) — if the daemon is down, StarDocker runs `colima start` for you (configure VM type, CPUs, etc. in your Colima config)

### Build from source

```bash
git clone https://github.com/stardust1420/stardocker.git
cd stardocker
go build .
```

This produces a `stardocker` binary in the current directory.

### Run

```bash
./stardocker
```

Or move it somewhere on your `$PATH` for global access:

```bash
sudo mv stardocker /usr/local/bin/
stardocker
```

---

## 🚀 Usage

Just type `stardocker` in your terminal. That's it — no config files, no flags, no setup.

### Main Menu

The home screen sits in a slowly drifting, twinkling starfield. Set `STARDOCKER_REDUCED_MOTION=1` to keep the stars but stop the motion.

| Option       | Description                              |
|:-------------|:-----------------------------------------|
| Containers   | View and manage all Docker containers    |
| Images       | Browse and delete local Docker images    |
| Exit         | Quit StarDocker                          |

### Keybindings

| Key          | Action                                    |
|:-------------|:------------------------------------------|
| `↑` / `k`   | Move up                                  |
| `↓` / `j`   | Move down                                |
| `Enter`      | Select / Expand compose stack / View logs |
| `Esc`        | Go back                                  |
| `s`          | Start or stop the selected container or compose stack (also in the logs view) |
| `d`          | Delete the selected container, stack or image (asks first) |
| `y` / `n`    | Confirm or cancel a delete                |
| `t`          | Logs view: choose how many hours back to show |
| `c` / `a`    | Logs view: clear the view / show the time range again |
| `q`          | Quit                                     |
| `?`          | Toggle help                              |

### Containers View

- Containers are marked `●` running or `○` stopped; a Compose stack with only some services up is marked `◐`
- **Docker Compose stacks** are automatically detected and grouped — press `Enter` to expand/collapse child containers
- Press `s` on a stopped container to **start** it, or on a running container to **stop** it
- Press `s` on a compose stack row to stop every running service in it, or, if none is running, to start them all
- Press `d` to **delete** a container. A running one is stopped first. On a stack row, `d` deletes every container in the stack. Volumes and networks are always kept
- Press `Enter` on a container to view its **live logs**
- The panel under the table shows everything about the selected row: full ports, image, command and status

### Images View

- One row per tag, with size, age and the containers that use each image
- Press `d` to **delete** an image. If the image has other tags, only the selected tag is removed
- An image that containers still use can't be deleted; delete those containers first

### Logs View

- Logs from the last hour are shown, with timestamps. Press `t` and type a number of hours (decimals work, so `0.5` is 30 minutes) to look further back or narrow it down
- The header shows whether the container is running (and for how long) or how and when it exited
- Press `s` to start or stop the container without leaving its logs
- Press `c` to clear the view and see only new output from then on, and `a` to bring back the chosen time range. This hides lines in StarDocker only; Docker keeps the container's logs
- The viewport auto-scrolls to the bottom for new output
- Scroll freely with arrow keys or vim bindings; auto-scroll resumes when you're near the bottom

---

## 🏗️ Project Structure

```
stardocker/
├── main.go            # Entry point — Docker client init & TUI bootstrap
└── src/
    ├── index.go       # Home screen with menu cards
    ├── containers.go  # Container listing, start/stop/delete, Compose grouping
    ├── logs.go        # Live container log viewer
    ├── images.go      # Image listing and delete
    ├── confirm.go     # Delete confirmation prompt and status messages
    ├── layout.go      # Palette and shared screen layout
    ├── columns.go     # Fits table columns to the terminal width
    ├── details.go     # Details panel for the selected row
    ├── styles.go      # Table styles
    └── help.go        # Keybinding definitions & help overlay
```

---

## 📚 Built With

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — the Elm-inspired TUI framework for Go
- [Bubbles](https://github.com/charmbracelet/bubbles) — pre-built TUI components (tables, viewports, progress bars)
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — terminal styling and layout
- [Docker Go SDK](https://pkg.go.dev/github.com/docker/docker/client) — Docker Engine API client

---

## 📄 License

This project is open-source and available under the [MIT License](LICENSE).

---

<div align="center">

Made with ☕ and Go

</div>

