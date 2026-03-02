<![CDATA[<div align="center">

```
███████╗████████╗ █████╗ ██████╗ ██████╗  ██████╗  ██████╗██╗  ██╗███████╗██████╗
██╔════╝╚══██╔══╝██╔══██╗██╔══██╗██╔══██╗██╔═══██╗██╔════╝██║ ██╔╝██╔════╝██╔══██╗
███████╗   ██║   ███████║██████╔╝██║  ██║██║   ██║██║     █████╔╝ █████╗  ██████╔╝
╚════██║   ██║   ██╔══██║██╔══██╗██║  ██║██║   ██║██║     ██╔═██╗ ██╔══╝  ██╔══██╗
███████║   ██║   ██║  ██║██║  ██║██████╔╝╚██████╔╝╚██████╗██║  ██╗███████╗██║  ██║
╚══════╝   ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝  ╚═════╝  ╚═════╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝
```

**A sleek terminal UI to manage your Docker containers — no browser required.**

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![Docker](https://img.shields.io/badge/Docker-Engine-2496ED?style=flat-square&logo=docker&logoColor=white)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)

</div>

---

## ✨ Features

- 📦 **List all containers** — view both running and stopped containers at a glance
- ▶️ **Start & Stop** — toggle container state directly from the TUI
- 📜 **Live Logs** — stream container logs in real-time with a scrollable viewport
- 🗂️ **Compose-aware** — automatically groups Docker Compose stacks with expandable children
- 🖼️ **Browse Images** — view available Docker images and their download status
- 🚀 **Auto-starts Docker** — detects if the Docker daemon is down and launches Docker Desktop for you (macOS)
- ⌨️ **Keyboard-driven** — fast, intuitive navigation with vim-style keybindings

---

## 📸 How It Works

StarDocker connects directly to your local Docker daemon via the Docker SDK. It renders an interactive table of your containers with live state indicators (`⏺`) so you can see what's running at a glance — then lets you drill into logs or control containers without ever leaving the terminal.

---

## 🛠️ Installation

### Prerequisites

- [Go 1.24+](https://go.dev/dl/)
- [Docker](https://docs.docker.com/get-docker/) installed and the daemon running (or Docker Desktop on macOS — StarDocker will start it for you)

### Build from source

```bash
git clone https://github.com/stardust1405/stardocker.git
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

| Option       | Description                              |
|:-------------|:-----------------------------------------|
| Containers   | View and manage all Docker containers    |
| Images       | Browse available Docker images           |
| Exit         | Quit StarDocker                          |

### Keybindings

| Key          | Action                                    |
|:-------------|:------------------------------------------|
| `↑` / `k`   | Move up                                  |
| `↓` / `j`   | Move down                                |
| `Enter`      | Select / Expand compose stack / View logs |
| `Esc`        | Go back                                  |
| `r`          | Start or stop the selected container      |
| `q`          | Quit                                     |
| `?`          | Toggle help                              |

### Containers View

- Running containers are marked with a `⏺` indicator
- **Docker Compose stacks** are automatically detected and grouped — press `Enter` to expand/collapse child containers
- Press `r` on a stopped container to **start** it, or on a running container to **stop** it
- Press `Enter` on a container to view its **live logs**

### Logs View

- Logs are fetched from the last 24 hours with timestamps
- The viewport auto-scrolls to the bottom for new output
- Scroll freely with arrow keys or vim bindings; auto-scroll resumes when you're near the bottom

---

## 🏗️ Project Structure

```
stardocker/
├── main.go            # Entry point — Docker client init & TUI bootstrap
└── src/
    ├── index.go       # Main menu model
    ├── containers.go  # Container listing, start/stop, Compose grouping
    ├── logs.go        # Live container log viewer
    ├── images.go      # Image browser
    ├── styles.go      # Lipgloss styles and theming
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
]]>
