package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/stardust1420/stardocker/src"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/docker/go-sdk/client"
)

func main() {
	ctx := context.TODO()

	// client.New resolves the daemon from DOCKER_HOST or the current docker
	// context (Colima registers and selects a "colima" context) and pings it.
	dockerClient, err := client.New(ctx)
	if err != nil {
		fmt.Println("Docker daemon not reachable. Starting Colima...")

		// colima start blocks until the VM and Docker daemon are ready.
		cmd := exec.Command("colima", "start")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("Failed to start Colima: %v\n", err)
			os.Exit(1)
		}

		dockerClient, err = client.New(ctx)
		if err != nil {
			fmt.Printf("Docker daemon still not reachable after starting Colima: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Docker is ready!")
	} else {
		fmt.Println("Docker already running.")
	}

	// Close the docker client when done
	defer dockerClient.Close()

	p := tea.NewProgram(src.InitIndexModel(dockerClient, 0, 0), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error encountered, terminating: %v", err)
		os.Exit(1)
	}
}
