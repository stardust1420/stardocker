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

type listNetworksModel struct {
	help         help.Model
	keys         listKeyMap
	dockerClient client.SDKClient
	width        int
	height       int
	grid         gridView
	all          []Network
	items        []Network // items[i] is the network on grid card i
	tickID       int
	status       statusLine
	listErr      string
	confirm      *confirmPrompt
	filter       filterBar
}

func InitListNetworksModel(dockerClient client.SDKClient, width, height int) listNetworksModel {
	l := listNetworksModel{
		help:         newHelp(),
		keys:         networksKeys,
		dockerClient: dockerClient,
		width:        width,
		height:       height,
		grid:         gridView{cursor: -1},
		tickID:       newTickID(),
		filter:       newFilterBar(),
	}
	l.layout()
	return l
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

// rebuild fills the grid with the networks that pass the filter. The grid
// keeps the cursor on the same network (by ID).
func (l *listNetworksModel) rebuild() {
	q := l.filter.query()
	var items []Network
	var entries []gridEntry
	for _, n := range l.all {
		subnet := strings.Join(n.Subnets, ", ")
		if !matches(q, n.Name, n.ID, n.Driver, subnet, strings.Join(n.UsedBy, " ")) {
			continue
		}
		glyph := glyphStopped
		if len(n.UsedBy) > 0 {
			glyph = glyphRunning
		}
		body := []string{valueLabel(n.Driver, "driver")}
		if subnet != "" {
			body = append(body, textLine(subnet))
		}
		id := shortContainerID(n.ID)
		switch {
		case builtinNetworks[n.Name]:
			id += "   built in"
		case n.Internal:
			id += "   internal"
		}
		entries = append(entries, gridEntry{
			key:        n.ID,
			selectable: true,
			glyph:      glyphFor(glyph),
			title:      n.Name,
			body:       append(body, mutedStyle.Render(id), usedByLine(n.UsedBy, "No containers attached")),
		})
		items = append(items, n)
	}
	l.items = items
	l.layout()
	l.grid.setEntries(entries)
}

func (l *listNetworksModel) layout() {
	l.grid.resize(pageWidth(l.width), listGridHeight(l.height, lipgloss.Height(l.bottomLeft()), l.confirm != nil))
}

func (l listNetworksModel) bottomLeft() string {
	if l.filter.editing {
		return l.filter.prompt()
	}
	return l.help.View(l.keys)
}

func (l listNetworksModel) selected() *Network {
	i := l.grid.cursor
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
				l.layout()
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
			l.layout()
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
			l.layout()
			return l, nil
		}
		l.grid.handleKey(msg)
		return l, nil

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

	return l, nil
}

func (l listNetworksModel) View() string {
	width := pageWidth(l.width)

	noun := "networks"
	if len(l.all) == 1 {
		noun = "network"
	}
	info := crumbStyle.Render(strconv.Itoa(len(l.all))) + mutedStyle.Render(" "+noun)

	panel := ""
	if l.confirm != nil {
		panel = renderConfirmPanel(width, l.confirm)
	}
	status := l.status.render()
	if l.listErr != "" {
		status = statusLine{text: l.listErr, kind: statusError}.render()
	}
	empty := "No networks."
	if l.filter.query() != "" {
		empty = "Nothing matches /" + l.filter.query() + ". Press esc to clear the filter."
	}
	return renderGridPage(
		renderHeader(width, info, withFilterCrumb(l.filter, "Networks")...),
		l.grid.view(empty),
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
