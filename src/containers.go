package src

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	containerTypes "github.com/docker/docker/api/types/container"
	"github.com/docker/go-sdk/client"
)

// tickMsg carries the id of the model that scheduled it. A tick that is still
// in flight when the screen changes is delivered to the new model, which
// ignores it because the id doesn't match; that stops refresh loops piling up.
type tickMsg struct {
	id int
	t  time.Time
}

var lastTickID atomic.Int64

func newTickID() int {
	return int(lastTickID.Add(1))
}

// containerActionMsg reports the result of an async start or stop.
type containerActionMsg struct {
	err error
}

// List Containers Model

type listContainersModel struct {
	help            help.Model
	keys            containersKeyMap
	dockerClient    client.SDKClient
	width           int
	height          int
	table           table.Model
	items           []Container // items[i] is the container or stack shown in table row i
	ShowChildrenSet StringSet
	tickID          int
	status          string
	running         int // header counts, across stacks and standalone containers
	stopped         int
}

const composeStackIdentifier = "com.docker.compose.project"

type ContainerType string

const (
	TypeContainer    ContainerType = "container"
	TypeComposeStack ContainerType = "compose_stack"
)

type Container struct {
	ID       string
	Name     string
	Type     ContainerType
	Image    string
	Command  string
	Created  time.Time
	Ports    []containerTypes.Port
	Status   string
	State    containerTypes.ContainerState
	Children []Container
}

// containerColumns: Ports gives up width last so published ports stay
// readable; the details panel below the table always shows them in full.
var containerColumns = []colSpec{
	{title: " ", min: 1, max: 1},
	{title: "Name", min: 12, max: 40, shrinkOrder: 3, grow: 3},
	{title: "Container ID", min: 12, max: 12},
	{title: "Image", min: 10, max: 35, shrinkOrder: 2, grow: 2},
	{title: "Ports", min: 12, max: 70, shrinkOrder: 4, grow: 3},
	{title: "Status", min: 10, max: 28, shrinkOrder: 1, grow: 1},
	{title: "State", min: 7, max: 10, shrinkOrder: 0},
}

func InitListContainersModel(dockerClient client.SDKClient, width int, height int) listContainersModel {
	t := table.New(
		table.WithColumns(fitColumns(containerColumns, nil, pageWidth(width))),
		table.WithFocused(true),
		table.WithHeight(listTableHeight(height, 1)),
	)

	t.SetStyles(tableStyles())

	return listContainersModel{
		help:            newHelp(),
		keys:            containersKeys,
		dockerClient:    dockerClient,
		width:           width,
		height:          height,
		table:           t,
		ShowChildrenSet: make(StringSet),
		tickID:          newTickID(),
	}
}

func (l listContainersModel) Init() tea.Cmd {
	// Fire the first tick right away so the table fills without waiting a
	// second; each tick then re-arms itself.
	id := l.tickID
	return tea.Batch(
		tea.SetWindowTitle("Containers"),
		func() tea.Msg { return tickMsg{id: id, t: time.Now()} },
	)
}

func (l *listContainersModel) refresh() {
	allContainers, err := FetchContainers(l.dockerClient)
	if err != nil {
		l.status = fmt.Sprintf("error listing containers: %v", err)
		return
	}
	rows, items := l.getRows(allContainers)
	l.items = items
	l.table.SetRows(rows)
	// SetRows doesn't clamp the cursor when rows disappear.
	if n := len(rows); l.table.Cursor() >= n {
		l.table.SetCursor(max(0, n-1))
	}

	l.running, l.stopped = 0, 0
	for _, c := range allContainers {
		for _, x := range append([]Container{c}, c.Children...) {
			if x.Type != TypeContainer {
				continue
			}
			if x.State == containerTypes.StateRunning {
				l.running++
			} else {
				l.stopped++
			}
		}
	}

	l.layout()
}

// layout fits the table to the window: full page width, and whatever height
// is left after the header, details panel and help.
func (l *listContainersModel) layout() {
	l.table.SetHeight(listTableHeight(l.height, lipgloss.Height(l.help.View(l.keys))))
	l.table.SetColumns(fitColumns(containerColumns, l.table.Rows(), pageWidth(l.width)))
}

// selected returns the container or stack under the cursor, or nil when the
// table is empty.
func (l listContainersModel) selected() *Container {
	i := l.table.Cursor()
	if i < 0 || i >= len(l.items) {
		return nil
	}
	return &l.items[i]
}

func (l listContainersModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		l.width = msg.Width
		l.height = msg.Height
		l.layout()
		return l, nil

	case tea.KeyMsg:
		// Handle help
		switch {
		case key.Matches(msg, l.keys.Help):
			l.help.ShowAll = !l.help.ShowAll
			l.layout()
		}

		switch msg.String() {

		case "esc":
			m := InitIndexModel(l.dockerClient, l.width, l.height)
			return m, m.Init()

		case "enter":
			c := l.selected()
			if c == nil {
				return l, nil
			}

			switch c.Type {

			case TypeContainer:
				lm := InitLogsModel(l.dockerClient, c.ID, c.Name, l.width, l.height)
				return lm, lm.Init()

			case TypeComposeStack:
				if l.ShowChildrenSet.Contains(c.Name) {
					l.ShowChildrenSet.Remove(c.Name)
				} else {
					l.ShowChildrenSet.Add(c.Name)
				}
				l.refresh()
			}

		case "ctrl+c", "q":
			return l, tea.Quit

		case "r":
			c := l.selected()
			if c == nil || c.Type != TypeContainer {
				return l, nil
			}
			switch c.State {
			case containerTypes.StateRunning:
				l.status = "stopping " + c.Name + "..."
				return l, stopContainerCmd(l.dockerClient, c.ID)
			case containerTypes.StateExited, containerTypes.StateCreated:
				l.status = "starting " + c.Name + "..."
				return l, startContainerCmd(l.dockerClient, c.ID)
			}
		}

	case containerActionMsg:
		if msg.err != nil {
			l.status = msg.err.Error()
		} else {
			l.status = ""
		}
		l.refresh()
		return l, nil

	case tickMsg:
		if msg.id != l.tickID {
			return l, nil
		}
		l.refresh()
		return l, tickCmd(l.tickID)
	}

	l.table, cmd = l.table.Update(msg)

	return l, cmd
}

func (l listContainersModel) View() string {
	width := pageWidth(l.width)
	title, lines := l.details()

	return renderListPage(
		renderHeader(width, stateCounts(l.running, l.stopped), "Containers"),
		tableBaseStyle.Render(l.table.View()),
		renderPanel(width, title, detailsBody(width, lines), detailsLines),
		renderBottomBar(width, l.help.View(l.keys), l.status),
	)
}

// details describes the selected row in full, so nothing is lost when table
// columns are truncated. It returns the panel title and its lines.
func (l listContainersModel) details() (string, []detailLine) {
	c := l.selected()
	if c == nil {
		return "", nil
	}

	ports := formatPorts(c.Ports)
	if ports == "" {
		ports = "None published"
		if c.State != containerTypes.StateRunning {
			ports = "None while stopped (Docker reports ports for running containers only)"
		}
	}

	if c.Type == TypeComposeStack {
		running := 0
		services := make([]string, len(c.Children))
		for i, child := range c.Children {
			services[i] = stateGlyph(child.State) + " " + child.Name
			if child.State == containerTypes.StateRunning {
				running++
			}
		}
		return c.Name + "  compose stack", []detailLine{
			{"Status", fmt.Sprintf("%d of %d services running", running, len(c.Children))},
			{"Ports", ports},
			{"Services", strings.Join(services, "   ")},
		}
	}

	return c.Name + "  " + shortContainerID(c.ID), []detailLine{
		{"Image", c.Image},
		{"Status", fmt.Sprintf("%s, created %s ago", c.Status, humanDuration(time.Since(c.Created)))},
		{"Ports", ports},
		{"Command", c.Command},
	}
}

func containerDisplayName(names []string, id string) string {
	if len(names) == 0 {
		return shortContainerID(id)
	}
	return strings.TrimLeft(names[0], "/")
}

func FetchContainers(dockerClient client.SDKClient) ([]Container, error) {
	ctx := context.TODO()

	containers, err := dockerClient.ContainerList(ctx, containerTypes.ListOptions{All: true})
	if err != nil {
		return nil, err
	}

	// List to store all containers
	allContainers := make([]Container, 0)

	// Map to store compose containers
	composeContainers := make(map[string][]Container)

	for _, container := range containers {
		c := Container{
			ID:      container.ID,
			Name:    containerDisplayName(container.Names, container.ID),
			Type:    TypeContainer,
			Image:   container.Image,
			Command: container.Command,
			Created: time.Unix(container.Created, 0),
			Ports:   container.Ports,
			Status:  container.Status,
			State:   container.State,
		}
		// Check if container is part of a compose stack
		if composeProjectName, ok := container.Labels[composeStackIdentifier]; ok {
			composeContainers[composeProjectName] = append(composeContainers[composeProjectName], c)
		} else {
			allContainers = append(allContainers, c)
		}
	}

	// Add compose containers to all containers. Stacks have no ID of their
	// own; leave it empty so the column doesn't change on every refresh.
	for composeStackName, containers := range composeContainers {
		sort.Slice(containers, func(i, j int) bool { return containers[i].Name < containers[j].Name })
		allContainers = append(allContainers, composeStackSummary(composeStackName, containers))
	}

	sort.Slice(allContainers, func(i, j int) bool { return allContainers[i].Name < allContainers[j].Name })

	return allContainers, nil
}

// composeStackSummary builds the parent row of a compose stack, filling its
// columns with a summary of its services so the collapsed row is useful.
func composeStackSummary(name string, children []Container) Container {
	running := 0
	var ports []containerTypes.Port
	for _, c := range children {
		if c.State == containerTypes.StateRunning {
			running++
		}
		ports = append(ports, c.Ports...)
	}

	state := "partial"
	switch running {
	case len(children):
		state = containerTypes.StateRunning
	case 0:
		state = containerTypes.StateExited
	}

	services := fmt.Sprintf("%d services", len(children))
	if len(children) == 1 {
		services = "1 service"
	}

	return Container{
		Name:     name,
		Type:     TypeComposeStack,
		Image:    services,
		Ports:    ports,
		Status:   fmt.Sprintf("%d/%d running", running, len(children)),
		State:    state,
		Children: children,
	}
}

// formatPorts renders ports the way `docker ps` does, e.g.
// "8080->80/tcp, 127.0.0.1:5432->5432/tcp, 9000/tcp". The wildcard host IP
// is left out, and the IPv6 binding Docker reports next to each IPv4 one is
// collapsed into it. Docker only reports ports for running containers.
func formatPorts(ports []containerTypes.Port) string {
	sorted := append([]containerTypes.Port(nil), ports...)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.PublicPort != b.PublicPort {
			return a.PublicPort < b.PublicPort
		}
		if a.PrivatePort != b.PrivatePort {
			return a.PrivatePort < b.PrivatePort
		}
		return a.Type < b.Type
	})

	seen := make(map[string]bool)
	parts := make([]string, 0, len(sorted))
	for _, p := range sorted {
		var s string
		switch {
		case p.PublicPort == 0:
			s = fmt.Sprintf("%d/%s", p.PrivatePort, p.Type)
		case p.IP == "" || p.IP == "0.0.0.0" || p.IP == "::":
			s = fmt.Sprintf("%d->%d/%s", p.PublicPort, p.PrivatePort, p.Type)
		default:
			s = fmt.Sprintf("%s:%d->%d/%s", p.IP, p.PublicPort, p.PrivatePort, p.Type)
		}
		if !seen[s] {
			seen[s] = true
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// shortContainerID is the 12-character ID `docker ps` shows; the Docker API
// accepts it anywhere a full ID is expected.
func shortContainerID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// stateGlyph maps a container or stack state to ● running, ◐ partly
// running (a stack) or paused, ○ anything else.
func stateGlyph(state string) string {
	switch state {
	case containerTypes.StateRunning:
		return glyphRunning
	case "partial", containerTypes.StatePaused:
		return glyphPartial
	default:
		return glyphStopped
	}
}

// getRows returns the table rows and, in the same order, the container or
// stack each row shows. Actions read from the latter, never from the cells.
func (l listContainersModel) getRows(containers []Container) ([]table.Row, []Container) {
	rows := []table.Row{}
	items := []Container{}

	for _, container := range containers {
		name := container.Name
		if container.Type == TypeComposeStack {
			if l.ShowChildrenSet.Contains(container.Name) {
				name = "▾ " + name
			} else {
				name = "▸ " + name
			}
		}

		rows = append(rows, table.Row{
			stateGlyph(container.State),
			name,
			shortContainerID(container.ID),
			container.Image,
			formatPorts(container.Ports),
			container.Status,
			container.State,
		})
		items = append(items, container)

		if container.Type == TypeComposeStack && l.ShowChildrenSet.Contains(container.Name) {
			for i, child := range container.Children {
				branch := "├ "
				if i == len(container.Children)-1 {
					branch = "└ "
				}
				rows = append(rows, table.Row{
					stateGlyph(child.State),
					"  " + branch + child.Name,
					shortContainerID(child.ID),
					child.Image,
					formatPorts(child.Ports),
					child.Status,
					child.State,
				})
				items = append(items, child)
			}
		}
	}

	return rows, items
}

// startContainerCmd and stopContainerCmd run off the UI goroutine so a slow
// stop (Docker waits up to 10s before killing) doesn't freeze the screen.
func startContainerCmd(dockerClient client.SDKClient, containerID string) tea.Cmd {
	return func() tea.Msg {
		err := dockerClient.ContainerStart(context.Background(), containerID, containerTypes.StartOptions{})
		if err != nil {
			err = fmt.Errorf("error starting container: %w", err)
		}
		return containerActionMsg{err: err}
	}
}

func stopContainerCmd(dockerClient client.SDKClient, containerID string) tea.Cmd {
	return func() tea.Msg {
		err := dockerClient.ContainerStop(context.Background(), containerID, containerTypes.StopOptions{})
		if err != nil {
			err = fmt.Errorf("error stopping container: %w", err)
		}
		return containerActionMsg{err: err}
	}
}

func tickCmd(id int) tea.Cmd {
	return tea.Tick(time.Second*1, func(t time.Time) tea.Msg {
		return tickMsg{id: id, t: t}
	})
}

// Define a set type using a map
type StringSet map[string]struct{}

// Function to add an element to the set
func (s StringSet) Add(value string) {
	s[value] = struct{}{}
}

// Function to remove an element from the set
func (s StringSet) Remove(value string) {
	delete(s, value)
}

// Function to check if an element is in the set
func (s StringSet) Contains(value string) bool {
	_, exists := s[value]
	return exists
}
