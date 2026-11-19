package src

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	containerTypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-sdk/client"
	"github.com/muesli/reflow/wordwrap"
)

type logsModel struct {
	dockerClient  client.SDKClient
	containerID   string
	containerName string
	tty           bool
	logs          string
	ready         bool
	viewport      viewport.Model
	width         int
	height        int
	tickID        int
}

// InitLogsModel builds the logs screen. width and height are the terminal
// size known by the previous screen; Bubble Tea only sends a WindowSizeMsg at
// startup and on resize, so the viewport must be built from them here.
func InitLogsModel(dockerClient client.SDKClient, containerID string, containerName string, width int, height int) logsModel {
	l := logsModel{
		dockerClient:  dockerClient,
		containerID:   containerID,
		containerName: containerName,
		tty:           containerHasTTY(dockerClient, containerID),
		tickID:        newTickID(),
	}
	l.logs = GetContainerLogs(dockerClient, containerID, l.tty)
	if width > 0 && height > 0 {
		l.setSize(width, height)
	}
	return l
}

func (l logsModel) Init() tea.Cmd {
	return tea.Batch(tea.SetWindowTitle("Logs: "+l.containerName), tickCmd(l.tickID))
}

// setSize builds the viewport on first call and resizes it afterwards,
// re-wrapping the content to the new width.
func (l *logsModel) setSize(width, height int) {
	l.width = width
	l.height = height

	headerHeight := lipgloss.Height(l.headerView())
	footerHeight := lipgloss.Height(l.footerView())
	vpHeight := max(1, height-headerHeight-footerHeight)

	if !l.ready {
		l.viewport = viewport.New(width, vpHeight)
		l.viewport.YPosition = headerHeight
		l.viewport.Style = lipgloss.NewStyle().PaddingLeft(textIndent)
		l.viewport.SetContent(l.wrapped())
		l.viewport.GotoBottom()
		l.ready = true
		return
	}

	atBottom := l.viewport.AtBottom()
	l.viewport.Width = width
	l.viewport.Height = vpHeight
	l.viewport.SetContent(l.wrapped())
	if atBottom {
		l.viewport.GotoBottom()
	}
}

func (l logsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return l, tea.Quit
		case "esc":
			m := InitListContainersModel(l.dockerClient, l.width, l.height)
			return m, m.Init()
		case "G", "end":
			l.viewport.GotoBottom()
			return l, nil
		}

	case tea.WindowSizeMsg:
		l.setSize(msg.Width, msg.Height)
		return l, nil

	case tickMsg:
		if msg.id != l.tickID {
			return l, nil
		}
		l.logs = GetContainerLogs(l.dockerClient, l.containerID, l.tty)
		if l.ready {
			// Decide whether to follow before the new content shifts the
			// scroll position.
			atBottom := l.viewport.AtBottom()
			l.viewport.SetContent(l.wrapped())
			if atBottom {
				l.viewport.GotoBottom()
			}
		}
		return l, tickCmd(l.tickID)
	}

	// Handle keyboard and mouse events in the viewport
	l.viewport, cmd = l.viewport.Update(msg)

	return l, cmd
}

// wrapped is the log text wrapped to the viewport's text width (inside the
// left padding that lines it up with the header).
func (l logsModel) wrapped() string {
	return wordwrap.String(l.logs, max(1, l.viewport.Width-textIndent))
}

func (l logsModel) View() string {
	if !l.ready {
		return "\n  Initializing..."
	}
	return fmt.Sprintf("%s\n%s\n%s", l.headerView(), l.viewport.View(), l.footerView())
}

// headerView is the shared breadcrumb header over a rule.
func (l logsModel) headerView() string {
	follow := mutedStyle.Render("scrolled back")
	if !l.ready || l.viewport.AtBottom() {
		follow = runningStyle.Render(glyphRunning) + mutedStyle.Render(" following")
	}
	header := renderHeader(l.viewport.Width, follow, "Containers", l.containerName)
	return header + "\n" + borderStyle.Render(strings.Repeat("─", max(0, l.viewport.Width)))
}

// footerView is a rule over the key hints and the scroll position.
func (l logsModel) footerView() string {
	hint := func(k, desc string) string { return crumbStyle.Render(k) + " " + mutedStyle.Render(desc) }
	keys := strings.Repeat(" ", textIndent) + strings.Join([]string{
		hint("↑/↓", "scroll"), hint("G", "jump to end"), hint("esc", "back"), hint("q", "quit"),
	}, "   ")
	pos := mutedStyle.Render(fmt.Sprintf("%3.f%%", l.viewport.ScrollPercent()*100)) + strings.Repeat(" ", textIndent)
	gap := max(2, l.viewport.Width-lipgloss.Width(keys)-lipgloss.Width(pos))
	return borderStyle.Render(strings.Repeat("─", max(0, l.viewport.Width))) + "\n" + keys + strings.Repeat(" ", gap) + pos
}

// containerHasTTY reports whether the container was created with a TTY, which
// decides whether its log stream is multiplexed.
func containerHasTTY(dockerClient client.SDKClient, containerID string) bool {
	info, err := dockerClient.ContainerInspect(context.Background(), containerID)
	if err != nil || info.Config == nil {
		return false
	}
	return info.Config.Tty
}

// GetContainerLogs returns the last 24h of logs. Errors are returned as the
// log text so they show up in the viewport instead of killing the program.
func GetContainerLogs(dockerClient client.SDKClient, containerID string, tty bool) string {
	ctx := context.Background()
	logs, err := dockerClient.ContainerLogs(ctx, containerID, containerTypes.LogsOptions{ShowStdout: true, ShowStderr: true, Since: "24h", Timestamps: true})
	if err != nil {
		return fmt.Sprintf("error fetching logs: %v", err)
	}
	defer logs.Close()

	// Without a TTY, Docker multiplexes stdout and stderr into one stream
	// with an 8-byte header per frame; strip those headers.
	var buf bytes.Buffer
	if tty {
		_, err = io.Copy(&buf, logs)
	} else {
		_, err = stdcopy.StdCopy(&buf, &buf, logs)
	}
	if err != nil {
		buf.WriteString(fmt.Sprintf("\nerror reading logs: %v", err))
	}

	return buf.String()
}
