<div align="center">

```
███████╗████████╗ █████╗ ██████╗ ██████╗  ██████╗  ██████╗██╗  ██╗███████╗██████╗
██╔════╝╚══██╔══╝██╔══██╗██╔══██╗██╔══██╗██╔═══██╗██╔════╝██║ ██╔╝██╔════╝██╔══██╗
███████╗   ██║   ███████║██████╔╝██║  ██║██║   ██║██║     █████╔╝ █████╗  ██████╔╝
╚════██║   ██║   ██╔══██║██╔══██╗██║  ██║██║   ██║██║     ██╔═██╗ ██╔══╝  ██╔══██╗
███████║   ██║   ██║  ██║██║  ██║██████╔╝╚██████╔╝╚██████╗██║  ██╗███████╗██║  ██║
╚══════╝   ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝  ╚═════╝  ╚═════╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝
```

**A sleek terminal UI for your Docker containers, images, volumes and networks**

[![CI](https://github.com/stardust1420/stardocker/actions/workflows/ci.yml/badge.svg)](https://github.com/stardust1420/stardocker/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/stardust1420/stardocker?style=flat-square)](https://github.com/stardust1420/stardocker/releases/latest)
[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![Docker](https://img.shields.io/badge/Docker-Engine-2496ED?style=flat-square&logo=docker&logoColor=white)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)

</div>

---

## ✨ Features

- 📦 **Containers** — running and stopped containers at a glance, with live **CPU and memory** use
- ▶️ **Start, stop, restart, pause** — single containers or whole Compose stacks
- 🐚 **Shell in** — open `bash` (or `sh`) inside a running container without leaving the app
- 📜 **Live logs** — follows new output as it's written, with filter and highlight, line-wrap toggle and save to file
- 🔍 **Inspect** — environment, mounts, networks, ports, labels and restart policy
- 🗂️ **Compose-aware** — groups Compose stacks with expandable services
- 🖼️ **Images** — sizes, which containers use them, pull new ones with progress, prune dangling ones
- 💾 **Volumes & networks** — see what uses them, delete or prune the unused ones
- 🔎 **Filter and sort** — `/` to filter any list, `o` to sort by CPU, memory, size or age
- 🗑️ **Safe deletes** — everything destructive asks first
- 🚀 **Starts Colima for you** — if the daemon is down and Colima is installed
- ⌨️ **Keyboard-driven** — vim-style keys, `?` shows every binding

---

## 🛠️ Installation

StarDocker needs a running Docker daemon: Docker Desktop, Colima, OrbStack, Rancher Desktop or Docker Engine on Linux. It connects through `DOCKER_HOST`, or the current `docker context`. Opening a shell in a container (`e`) also needs the `docker` CLI on your `PATH`.

### Prebuilt binaries

Download the archive for your OS and CPU from the [latest release](https://github.com/stardust1420/stardocker/releases/latest), extract it, and put `stardocker` on your `PATH`.

### With Go

```bash
go install github.com/stardust1420/stardocker@latest
```

### From source

```bash
git clone https://github.com/stardust1420/stardocker.git
cd stardocker
go build .
./stardocker
```

---

## 🚀 Usage

```bash
stardocker            # start the app
stardocker --version  # print the version
stardocker --help     # flags and environment variables
```

If the daemon isn't reachable and [Colima](https://github.com/abiosoft/colima) is installed (and `DOCKER_HOST` isn't set), StarDocker runs `colima start` first. Configure the VM (type, CPUs, memory) in your Colima config.

### Home

Cards for Containers, Images, Volumes and Networks, each with a live count, over a slowly drifting starfield. Move with `←/→` or `h/l`, open with `Enter` or `1`–`5`. Set `STARDOCKER_REDUCED_MOTION=1` to keep the stars still.

### Keys on every list

| Key | Action |
|:----|:-------|
| `↑`/`k`, `↓`/`j` | Move |
| `/` | Filter the list as you type (`Enter` keeps it, `Esc` clears it) |
| `d` | Delete the selected item (asks first) |
| `P` | Prune: delete everything unused on this screen (asks first) |
| `Esc` | Clear the filter, or go back |
| `?` | Show all keys |
| `q` | Quit |

### Containers

| Key | Action |
|:----|:-------|
| `Enter` | Open logs, or expand/collapse a Compose stack |
| `s` | Start or stop. On a stack: stop every running service, or start them all if none is running |
| `r` | Restart (a stack: every service that isn't paused) |
| `p` | Pause or unpause |
| `e` | Open a shell in the container |
| `i` | Inspect: environment, mounts, networks, ports, labels, restart policy |
| `o` | Sort by name, CPU, memory or newest |
| `a` | Show running containers only |
| `P` | Delete all stopped containers |

- `●` running, `◐` paused or a partly running stack, `○` stopped
- CPU and memory update every second for running containers. CPU is per core, so 200% means two full cores
- Deleting a running container stops it first. Deleting a stack row deletes all its containers. Volumes are kept
- The panel under the table shows the selected row in full: ports, image, command and status

### Logs

| Key | Action |
|:----|:-------|
| `/` | Show only matching lines, highlighted |
| `t` | How many hours back to show (decimals work: `0.5` is 30 minutes) |
| `c` / `a` | Clear the view to see only new output / bring the time range back |
| `w` | Toggle line wrapping (`←/→` scroll sideways when off) |
| `o` | Save the shown lines to `<container>-<time>.log` in the current directory |
| `s` | Start or stop the container |
| `e` | Open a shell in the container |
| `G` | Jump to the end |

New output streams in as it's written. The view follows the end unless you've scrolled up. The header shows whether the container is running, and for how long, or how and when it exited.

### Images

| Key | Action |
|:----|:-------|
| `p` | Pull an image (pre-filled with the selected one, so `p` `Enter` updates it) |
| `o` | Sort by name, size or newest |
| `d` | Delete the image, or only the selected tag if it has others |
| `P` | Delete dangling (untagged, unused) images |

An image that containers still use can't be deleted. Delete those containers first.

### Volumes and networks

Each row shows which containers use the volume or network. `d` deletes one that nothing uses, and `P` deletes every unused one. Pruning volumes deletes their data for good, named volumes included. Docker's built-in `bridge`, `host` and `none` networks can't be deleted.

---

## 🏗️ Project Structure

```
stardocker/
├── main.go                # Flags, Docker connection (with Colima fallback), TUI bootstrap
└── src/
    ├── index.go           # Home screen cards
    ├── starfield.go       # Animated home background
    ├── containers.go      # Container list, Compose grouping, filter and sort
    ├── container_actions.go # Start/stop/restart/pause, shell, prune
    ├── stats.go           # Live CPU and memory
    ├── logs.go            # Log viewer
    ├── logstream.go       # Following log stream
    ├── inspect.go         # Container inspect view
    ├── images.go          # Images: list, pull, delete, prune
    ├── volumes.go         # Volumes
    ├── networks.go        # Networks
    ├── input.go           # Text field and "/" filter
    ├── confirm.go         # Confirmation prompts and status messages
    ├── layout.go          # Palette and shared screen layout
    ├── columns.go         # Fits table columns to the terminal width
    ├── details.go         # Details panel for the selected row
    ├── styles.go          # Table styles
    └── help.go            # Keybindings and help
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

