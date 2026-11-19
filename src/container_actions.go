package src

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	containerTypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/go-sdk/client"
)

// containerPlan is what s, r or p will do for the selected row.
type containerPlan struct {
	verb     string      // "start", "stop", "restart", "pause" or "unpause"
	targets  []Container // containers to act on
	progress string      // status while it runs
	done     string      // status when it succeeded
}

// pastTense of the plan verbs, for status messages.
var pastTense = map[string]string{
	"start": "Started", "stop": "Stopped", "restart": "Restarted",
	"pause": "Paused", "unpause": "Unpaused",
}

var progressTense = map[string]string{
	"start": "Starting", "stop": "Stopping", "restart": "Restarting",
	"pause": "Pausing", "unpause": "Unpausing",
}

// newPlan words the messages for acting on targets; stack is the stack's
// name, or "" for a single container.
func newPlan(verb string, targets []Container, stack string) containerPlan {
	what := ""
	if stack == "" {
		what = targets[0].Name
	} else {
		n := fmt.Sprintf("%d containers", len(targets))
		if len(targets) == 1 {
			n = "1 container"
		}
		what = n + " in " + stack
	}
	return containerPlan{
		verb:     verb,
		targets:  targets,
		progress: progressTense[verb] + " " + what + "…",
		done:     pastTense[verb] + " " + what,
	}
}

// canStart reports whether "s" starts a container in this state.
func canStart(state string) bool {
	return state == containerTypes.StateExited || state == containerTypes.StateCreated
}

// partition splits a stack's services by state.
func partition(children []Container) (running, startable, paused []Container) {
	for _, c := range children {
		switch {
		case c.State == containerTypes.StateRunning:
			running = append(running, c)
		case c.State == containerTypes.StatePaused:
			paused = append(paused, c)
		case canStart(c.State):
			startable = append(startable, c)
		}
	}
	return
}

// planStartStop decides what "s" does:
//   - a container: stop it if running, start it if exited or created;
//   - a compose stack: if any service is running, stop every running one;
//     if none is, start them all. (Starting the rest of a partly running
//     stack instead would make a stack with a one-shot service, which exits
//     on its own, impossible to stop with "s".)
//
// ok is false when there is nothing to do (e.g. a paused container).
func planStartStop(c Container) (plan containerPlan, ok bool) {
	if c.Type != TypeComposeStack {
		switch {
		case c.State == containerTypes.StateRunning:
			return newPlan("stop", []Container{c}, ""), true
		case canStart(c.State):
			return newPlan("start", []Container{c}, ""), true
		}
		return containerPlan{}, false
	}

	running, startable, _ := partition(c.Children)
	if len(running) > 0 {
		return newPlan("stop", running, c.Name), true
	}
	if len(startable) > 0 {
		return newPlan("start", startable, c.Name), true
	}
	return containerPlan{}, false
}

// planRestart decides what "r" does: restart a running or stopped
// container, or every running and stopped service of a stack. Docker
// restarts a stopped container by starting it. Paused ones are left alone.
func planRestart(c Container) (containerPlan, bool) {
	if c.Type != TypeComposeStack {
		if c.State == containerTypes.StateRunning || canStart(c.State) {
			return newPlan("restart", []Container{c}, ""), true
		}
		return containerPlan{}, false
	}
	running, startable, _ := partition(c.Children)
	targets := append(append([]Container{}, running...), startable...)
	if len(targets) == 0 {
		return containerPlan{}, false
	}
	return newPlan("restart", targets, c.Name), true
}

// planPause decides what "p" does: pause a running container, unpause a
// paused one. On a stack, pause every running service, or if none is
// running, unpause the paused ones.
func planPause(c Container) (containerPlan, bool) {
	if c.Type != TypeComposeStack {
		switch c.State {
		case containerTypes.StateRunning:
			return newPlan("pause", []Container{c}, ""), true
		case containerTypes.StatePaused:
			return newPlan("unpause", []Container{c}, ""), true
		}
		return containerPlan{}, false
	}
	running, _, paused := partition(c.Children)
	if len(running) > 0 {
		return newPlan("pause", running, c.Name), true
	}
	if len(paused) > 0 {
		return newPlan("unpause", paused, c.Name), true
	}
	return containerPlan{}, false
}

// containerPlanCmd carries out a plan off the UI goroutine, acting on all
// targets in parallel: a stop or restart can take up to 10s per container
// while Docker waits before killing it, which would freeze the screen or
// add up.
func containerPlanCmd(dockerClient client.SDKClient, plan containerPlan) tea.Cmd {
	return func() tea.Msg {
		var (
			wg     sync.WaitGroup
			mu     sync.Mutex
			failed []string
		)
		for _, t := range plan.targets {
			wg.Add(1)
			go func(t Container) {
				defer wg.Done()
				ctx := context.Background()
				var err error
				switch plan.verb {
				case "start":
					err = dockerClient.ContainerStart(ctx, t.ID, containerTypes.StartOptions{})
				case "stop":
					err = dockerClient.ContainerStop(ctx, t.ID, containerTypes.StopOptions{})
				case "restart":
					err = dockerClient.ContainerRestart(ctx, t.ID, containerTypes.StopOptions{})
				case "pause":
					err = dockerClient.ContainerPause(ctx, t.ID)
				case "unpause":
					err = dockerClient.ContainerUnpause(ctx, t.ID)
				default:
					err = fmt.Errorf("unknown action %q", plan.verb)
				}
				if err != nil {
					mu.Lock()
					failed = append(failed, fmt.Sprintf("%s: %v", t.Name, err))
					mu.Unlock()
				}
			}(t)
		}
		wg.Wait()

		if len(failed) > 0 {
			sort.Strings(failed)
			return containerActionMsg{err: fmt.Errorf("couldn't %s %s", plan.verb, strings.Join(failed, "; "))}
		}
		return containerActionMsg{done: plan.done}
	}
}

// shellScript runs bash if the image has it, sh otherwise.
const shellScript = "if command -v bash >/dev/null 2>&1; then exec bash; else exec sh; fi"

// execShellCmd suspends the UI and opens an interactive shell in the
// container through the docker CLI, which handles the terminal (raw mode,
// resizing) properly. The CLI resolves the daemon the same way StarDocker
// does (DOCKER_HOST or the current context). refusal explains why it can't.
func execShellCmd(c Container) (run tea.Cmd, refusal string) {
	if c.State != containerTypes.StateRunning {
		return nil, c.Name + " isn't running. Start it with s first."
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		return nil, "Opening a shell needs the docker CLI on your PATH."
	}
	args := []string{"exec", "-it"}
	if term := os.Getenv("TERM"); term != "" {
		args = append(args, "-e", "TERM="+term)
	}
	args = append(args, c.ID, "sh", "-c", shellScript)
	cmd := exec.Command(docker, args...)
	name := c.Name
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return containerActionMsg{err: shellError(name, err)}
	}), ""
}

// shellError keeps only errors that mean the shell couldn't start. The
// shell's own exit status (e.g. after "exit 1") isn't a failure to report.
func shellError(name string, err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case 126, 127:
			return fmt.Errorf("%s has no shell (sh) to open", name)
		default:
			return nil
		}
	}
	return fmt.Errorf("couldn't open a shell in %s: %v", name, err)
}

// pruneContainersPrompt builds the confirmation for deleting every stopped
// container. refusal is set when there is nothing to prune.
func pruneContainersPrompt(dockerClient client.SDKClient, stopped int) (*confirmPrompt, string) {
	if stopped == 0 {
		return nil, "No stopped containers to delete"
	}
	noun := "containers"
	if stopped == 1 {
		noun = "container"
	}
	return &confirmPrompt{
		title: fmt.Sprintf("Delete all %d stopped %s?", stopped, noun),
		lines: []string{
			"Every container that isn't running goes, including ones in compose stacks.",
			"Their logs and any files changed inside them are lost. Volumes are kept.",
		},
		progress: "Deleting stopped containers…",
		yes:      "prune",
		run: func() tea.Msg {
			report, err := dockerClient.ContainersPrune(context.Background(), filters.Args{})
			if err != nil {
				return containerActionMsg{err: fmt.Errorf("couldn't prune containers: %w", err)}
			}
			return containerActionMsg{done: fmt.Sprintf("Deleted %d stopped containers, freed %s",
				len(report.ContainersDeleted), humanSize(int64(report.SpaceReclaimed)))}
		},
	}, ""
}
