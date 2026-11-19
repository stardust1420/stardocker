package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"

	"github.com/stardust1420/stardocker/src"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/docker/go-sdk/client"
)

// version is set at build time by GoReleaser (-X main.version=...). For
// `go install ...@vX.Y.Z` it comes from the module's build info instead.
var version = "dev"

func resolvedVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

const usage = `StarDocker: a terminal UI for your local Docker containers, images,
volumes and networks.

Usage:
  stardocker [flags]

Flags:
  -h, --help       Show this help
  -v, --version    Print the version

StarDocker connects to the daemon in DOCKER_HOST, or the current docker
context. If it can't, and Colima is installed, it runs "colima start" first.

Environment:
  DOCKER_HOST                   Docker daemon to use
  STARDOCKER_REDUCED_MOTION=1   Keep the home screen's starfield still

Press ? inside the app for keybindings.
`

func main() {
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	showVersion := flag.Bool("version", false, "")
	flag.BoolVar(showVersion, "v", false, "")
	flag.Parse()

	if *showVersion {
		fmt.Println("stardocker", resolvedVersion())
		return
	}
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "stardocker: unexpected argument %q\n\n", flag.Arg(0))
		flag.Usage()
		os.Exit(2)
	}

	dockerClient, err := connect(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer dockerClient.Close()

	p := tea.NewProgram(src.InitIndexModel(dockerClient, 0, 0), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "stardocker: %v\n", err)
		os.Exit(1)
	}
}

// connect returns a client for the daemon in DOCKER_HOST or the current
// docker context (client.New pings it). If that fails and Colima is
// installed, it starts Colima and tries once more.
func connect(ctx context.Context) (client.SDKClient, error) {
	dockerClient, err := client.New(ctx)
	if err == nil {
		return dockerClient, nil
	}

	colima, lookErr := exec.LookPath("colima")
	if lookErr != nil || os.Getenv("DOCKER_HOST") != "" {
		return nil, fmt.Errorf(`stardocker: can't reach the Docker daemon: %v

Start Docker (Docker Desktop, Colima, OrbStack, or "sudo systemctl start docker"
on Linux) and try again. StarDocker uses DOCKER_HOST, or the current
"docker context" if DOCKER_HOST isn't set.`, err)
	}

	fmt.Println("Docker isn't reachable. Starting Colima…")
	// colima start blocks until the VM and Docker daemon are ready.
	cmd := exec.Command(colima, "start")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("stardocker: couldn't start Colima: %v", err)
	}

	dockerClient, err = client.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("stardocker: Docker still isn't reachable after starting Colima: %v", err)
	}
	return dockerClient, nil
}
