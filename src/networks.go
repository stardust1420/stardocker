package src

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	containerTypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	networkTypes "github.com/docker/docker/api/types/network"
	"github.com/docker/go-sdk/client"
)

// Network is one row of the networks table.
type Network struct {
	ID       string
	Name     string
	Driver   string
	Scope    string
	Subnets  []string
	Internal bool
	Created  time.Time
	Labels   map[string]string
	UsedBy   []string // containers (running or not) attached to it
}

// builtinNetworks are created by Docker itself and can't be removed.
var builtinNetworks = map[string]bool{"bridge": true, "host": true, "none": true}

var networkColumns = []colSpec{
	{title: " ", min: 1, max: 1},
	{title: "Name", min: 12, max: 40, shrinkOrder: 3, grow: 3},
	{title: "Network ID", min: 12, max: 12, shrinkOrder: 0},
	{title: "Driver", min: 6, max: 10},
	{title: "Subnet", min: 10, max: 36, shrinkOrder: 1, grow: 1},
	{title: "Used by", min: 10, max: 60, shrinkOrder: 2, grow: 2},
}

type listNetworksModel struct {
	help         help.Model
	keys         listKeyMap
	dockerClient client.SDKClient
	width        int
	height       int
	table        table.Model
	all          []Network
	items        []Network
	tickID       int
	status       statusLine
	listErr      string
	confirm      *confirmPrompt
	filter       filterBar
}

func InitListNetworksModel(dockerClient client.SDKClient, width, height int) listNetworksModel {
	t := table.New(
		table.WithColumns(fitColumns(networkColumns, nil, pageWidth(width))),
		table.WithFocused(true),
		table.WithHeight(listTableHeight(height, 1)),
	)
	t.SetStyles(tableStyles())
	return listNetworksModel{
		help:         newHelp(),
		keys:         networksKeys,
		dockerClient: dockerClient,
		width:        width,
		height:       height,
		table:        t,
		tickID:       newTickID(),
		filter:       newFilterBar(),
	}
}

func (l listNetworksModel) Init() tea.Cmd {
	id := l.tickID
	return tea.Batch(tea.SetWindowTitle("Networks"), func() tea.Msg { return tickMsg{id: id, t: time.Now()} })
}

// FetchNetworks lists networks with the containers attached to each. The
// network list endpoint leaves Containers empty, so attachment comes from
// the container list instead.
func FetchNetworks(dockerClient client.SDKClient) ([]Network, error) {
	ctx := context.TODO()
	nets, err := dockerClient.NetworkList(ctx, networkTypes.ListOptions{})
	if err != nil {
		return nil, err
	}
	containers, err := dockerClient.ContainerList(ctx, containerTypes.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	usedBy := map[string][]string{} // network ID → container names
	for _, c := range containers {
		if c.NetworkSettings == nil {
			continue
		}
		for _, ep := range c.NetworkSettings.Networks {
			if ep != nil && ep.NetworkID != "" {
				usedBy[ep.NetworkID] = append(usedBy[ep.NetworkID], containerDisplayName(c.Names, c.ID))
			}
		}
	}

	out := make([]Network, 0, len(nets))
	for _, n := range nets {
		var subnets []string
		for _, cfg := range n.IPAM.Config {
			if cfg.Subnet != "" {
				subnets = append(subnets, cfg.Subnet)
			}
		}
		users := usedBy[n.ID]
		sort.Strings(users)
		out = append(out, Network{
			ID:       n.ID,
			Name:     n.Name,
			Driver:   n.Driver,
			Scope:    n.Scope,
			Subnets:  subnets,
			Internal: n.Internal,
			Created:  n.Created,
			Labels:   n.Labels,
			UsedBy:   users,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (l *listNetworksModel) refresh() {
	nets, err := FetchNetworks(l.dockerClient)
	if err != nil {
		l.listErr = fmt.Sprintf("Can't list networks: %v", err)
		return
	}
	l.listErr = ""
	l.all = nets
	l.rebuild()
}

func (l *listNetworksModel) rebuild() {
	prev := ""
	if n := l.selected(); n != nil {
		prev = n.ID
	}
	q := l.filter.query()
	var items []Network
	var rows []table.Row
	for _, n := range l.all {
		subnet := strings.Join(n.Subnets, ", ")
		if !matches(q, n.Name, n.ID, n.Driver, subnet, strings.Join(n.UsedBy, " ")) {
			continue
		}
		glyph := glyphStopped
		if len(n.UsedBy) > 0 {
			glyph = glyphRunning
		}
		rows = append(rows, table.Row{glyph, n.Name, shortContainerID(n.ID), n.Driver, subnet, strings.Join(n.UsedBy, ", ")})
		items = append(items, n)
	}
	l.items = items
	l.table.SetRows(rows)
	cursor := min(l.table.Cursor(), max(0, len(rows)-1))
	for i, n := range items {
		if prev != "" && n.ID == prev {
			cursor = i
			break
		}
	}
	l.table.SetCursor(cursor)
	l.layout()
}

func (l *listNetworksModel) layout() {
	l.table.SetHeight(listTableHeight(l.height, lipgloss.Height(l.bottomLeft())))
	l.table.SetColumns(fitColumns(networkColumns, l.table.Rows(), pageWidth(l.width)))
}

func (l listNetworksModel) bottomLeft() string {
	if l.filter.editing {
		return l.filter.prompt()
	}
	return l.help.View(l.keys)
}

func (l listNetworksModel) selected() *Network {
	i := l.table.Cursor()
	if i < 0 || i >= len(l.items) {
		return nil
	}
	return &l.items[i]
}

// prunable reports whether `docker network prune` would remove n.
func prunable(n Network) bool {
	return !builtinNetworks[n.Name] && len(n.UsedBy) == 0
}

func (l listNetworksModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width, l.height = msg.Width, msg.Height
		l.layout()
		return l, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return l, tea.Quit
		}
		if l.confirm != nil {
			run, done := handleConfirmKey(l.confirm, msg)
			if done {
				if run != nil {
					l.status.set(l.tickID, statusProgress, l.confirm.progress)
				}
				l.confirm = nil
			}
			return l, run
		}
		if l.filter.editing {
			if l.filter.handle(msg) {
				l.rebuild()
			}
			l.layout()
			return l, nil
		}
		if key.Matches(msg, l.keys.Help) {
			l.help.ShowAll = !l.help.ShowAll
			l.layout()
			return l, nil
		}
		switch msg.String() {
		case "q":
			return l, tea.Quit
		case "esc":
			if l.filter.query() != "" {
				l.filter.clear()
				l.rebuild()
				return l, nil
			}
			m := InitIndexModel(l.dockerClient, l.width, l.height).withSelected(cardNetworks)
			return m, m.Init()
		case "/":
			l.filter.open()
			l.layout()
			return l, nil
		case "d":
			n := l.selected()
			if n == nil {
				return l, nil
			}
			if builtinNetworks[n.Name] {
				return l, l.status.set(l.tickID, statusError, n.Name+" is built into Docker and can't be deleted.")
			}
			if len(n.UsedBy) > 0 {
				return l, l.status.set(l.tickID, statusError,
					fmt.Sprintf("%s is used by %s. Delete or disconnect those containers first.", n.Name, strings.Join(n.UsedBy, ", ")))
			}
			l.confirm = deleteNetworkPrompt(l.dockerClient, *n)
			return l, nil
		case "P":
			unused := 0
			for _, n := range l.all {
				if prunable(n) {
					unused++
				}
			}
			if unused == 0 {
				return l, l.status.set(l.tickID, statusSuccess, "No unused networks to delete")
			}
			l.confirm = pruneNetworksPrompt(l.dockerClient, unused)
			return l, nil
		}

	case actionMsg:
		var clear tea.Cmd
		if msg.err != nil {
			l.status.set(l.tickID, statusError, msg.err.Error())
		} else {
			clear = l.status.set(l.tickID, statusSuccess, msg.done)
		}
		l.refresh()
		return l, clear

	case clearStatusMsg:
		l.status.clear(msg, l.tickID)
		return l, nil

	case tickMsg:
		if msg.id != l.tickID {
			return l, nil
		}
		l.refresh()
		return l, tickCmd(l.tickID)
	}

	var cmd tea.Cmd
	l.table, cmd = l.table.Update(msg)
	return l, cmd
}

func (l listNetworksModel) View() string {
	width := pageWidth(l.width)

	noun := "networks"
	if len(l.all) == 1 {
		noun = "network"
	}
	info := crumbStyle.Render(strconv.Itoa(len(l.all))) + mutedStyle.Render(" "+noun)

	title, lines := "", []detailLine(nil)
	if n := l.selected(); n != nil {
		usedBy := "No containers"
		if len(n.UsedBy) > 0 {
			usedBy = strings.Join(n.UsedBy, ", ")
		}
		subnet := strings.Join(n.Subnets, ", ")
		if subnet == "" {
			subnet = "None"
		}
		kind := n.Driver + ", " + n.Scope + " scope"
		if n.Internal {
			kind += ", internal (no outside access)"
		}
		if builtinNetworks[n.Name] {
			kind += ", built in"
		}
		title = n.Name + "  " + shortContainerID(n.ID)
		lines = []detailLine{
			{"Driver", kind},
			{"Subnet", subnet},
			{"Used by", usedBy},
		}
		if p := n.Labels[composeStackIdentifier]; p != "" {
			lines = append(lines, detailLine{"Project", p})
		}
	}
	panel := renderPanel(width, title, detailsBody(width, lines), detailsLines)
	if l.confirm != nil {
		panel = renderConfirmPanel(width, l.confirm)
	}
	status := l.status.render()
	if l.listErr != "" {
		status = statusLine{text: l.listErr, kind: statusError}.render()
	}
	return renderListPage(
		renderHeader(width, info, withFilterCrumb(l.filter, "Networks")...),
		tableBaseStyle.Render(l.table.View()),
		panel,
		renderBottomBar(width, l.bottomLeft(), status),
	)
}

func deleteNetworkPrompt(dockerClient client.SDKClient, n Network) *confirmPrompt {
	return &confirmPrompt{
		title:    "Delete the network " + n.Name + "?",
		lines:    []string{"No container is attached to it. Compose recreates its networks on the next up."},
		progress: "Deleting " + n.Name + "…",
		run: func() tea.Msg {
			if err := dockerClient.NetworkRemove(context.Background(), n.ID); err != nil {
				return actionMsg{err: fmt.Errorf("couldn't delete %s: %w", n.Name, err)}
			}
			return actionMsg{done: "Deleted " + n.Name}
		},
	}
}

func pruneNetworksPrompt(dockerClient client.SDKClient, unused int) *confirmPrompt {
	noun := "networks"
	if unused == 1 {
		noun = "network"
	}
	return &confirmPrompt{
		title:    fmt.Sprintf("Delete %d unused %s?", unused, noun),
		lines:    []string{"Every network no container is attached to goes. Docker's built-in bridge, host and none stay."},
		progress: "Deleting unused networks…",
		yes:      "prune",
		run: func() tea.Msg {
			report, err := dockerClient.NetworksPrune(context.Background(), filters.Args{})
			if err != nil {
				return actionMsg{err: fmt.Errorf("couldn't prune networks: %w", err)}
			}
			return actionMsg{done: fmt.Sprintf("Deleted %d networks", len(report.NetworksDeleted))}
		},
	}
}
