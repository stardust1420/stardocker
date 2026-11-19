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
	volumeTypes "github.com/docker/docker/api/types/volume"
	"github.com/docker/go-sdk/client"
)

// Volume is one row of the volumes table.
type Volume struct {
	Name       string
	Driver     string
	Mountpoint string
	Created    time.Time
	Labels     map[string]string
	UsedBy     []string // containers (running or not) that mount it
}

var volumeColumns = []colSpec{
	{title: " ", min: 1, max: 1},
	{title: "Name", min: 16, max: 66, shrinkOrder: 3, grow: 3},
	{title: "Driver", min: 6, max: 10},
	{title: "Project", min: 8, max: 24, shrinkOrder: 1, grow: 1},
	{title: "Created", min: 10, max: 16, shrinkOrder: 0},
	{title: "Used by", min: 10, max: 60, shrinkOrder: 2, grow: 2},
}

type listVolumesModel struct {
	help         help.Model
	keys         listKeyMap
	dockerClient client.SDKClient
	width        int
	height       int
	table        table.Model
	all          []Volume
	items        []Volume
	tickID       int
	status       statusLine
	listErr      string
	confirm      *confirmPrompt
	filter       filterBar
}

func InitListVolumesModel(dockerClient client.SDKClient, width, height int) listVolumesModel {
	t := table.New(
		table.WithColumns(fitColumns(volumeColumns, nil, pageWidth(width))),
		table.WithFocused(true),
		table.WithHeight(listTableHeight(height, 1)),
	)
	t.SetStyles(tableStyles())
	return listVolumesModel{
		help:         newHelp(),
		keys:         volumesKeys,
		dockerClient: dockerClient,
		width:        width,
		height:       height,
		table:        t,
		tickID:       newTickID(),
		filter:       newFilterBar(),
	}
}

func (l listVolumesModel) Init() tea.Cmd {
	id := l.tickID
	return tea.Batch(tea.SetWindowTitle("Volumes"), func() tea.Msg { return tickMsg{id: id, t: time.Now()} })
}

// FetchVolumes lists volumes with the containers that mount each one.
func FetchVolumes(dockerClient client.SDKClient) ([]Volume, error) {
	ctx := context.TODO()
	resp, err := dockerClient.VolumeList(ctx, volumeTypes.ListOptions{})
	if err != nil {
		return nil, err
	}
	containers, err := dockerClient.ContainerList(ctx, containerTypes.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	usedBy := map[string][]string{}
	for _, c := range containers {
		for _, m := range c.Mounts {
			if m.Type == "volume" && m.Name != "" {
				usedBy[m.Name] = append(usedBy[m.Name], containerDisplayName(c.Names, c.ID))
			}
		}
	}

	out := make([]Volume, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		if v == nil {
			continue
		}
		created, _ := time.Parse(time.RFC3339, v.CreatedAt)
		users := usedBy[v.Name]
		sort.Strings(users)
		out = append(out, Volume{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			Created:    created,
			Labels:     v.Labels,
			UsedBy:     users,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (l *listVolumesModel) refresh() {
	vols, err := FetchVolumes(l.dockerClient)
	if err != nil {
		l.listErr = fmt.Sprintf("Can't list volumes: %v", err)
		return
	}
	l.listErr = ""
	l.all = vols
	l.rebuild()
}

func (l *listVolumesModel) rebuild() {
	prev := ""
	if v := l.selected(); v != nil {
		prev = v.Name
	}
	q := l.filter.query()
	var items []Volume
	var rows []table.Row
	now := time.Now()
	for _, v := range l.all {
		project := v.Labels[composeStackIdentifier]
		if !matches(q, v.Name, v.Driver, project, strings.Join(v.UsedBy, " ")) {
			continue
		}
		glyph := glyphStopped
		if len(v.UsedBy) > 0 {
			glyph = glyphRunning
		}
		created := ""
		if !v.Created.IsZero() {
			created = humanDuration(now.Sub(v.Created)) + " ago"
		}
		rows = append(rows, table.Row{glyph, v.Name, v.Driver, project, created, strings.Join(v.UsedBy, ", ")})
		items = append(items, v)
	}
	l.items = items
	l.table.SetRows(rows)
	cursor := min(l.table.Cursor(), max(0, len(rows)-1))
	for i, v := range items {
		if prev != "" && v.Name == prev {
			cursor = i
			break
		}
	}
	l.table.SetCursor(cursor)
	l.layout()
}

func (l *listVolumesModel) layout() {
	l.table.SetHeight(listTableHeight(l.height, lipgloss.Height(l.bottomLeft())))
	l.table.SetColumns(fitColumns(volumeColumns, l.table.Rows(), pageWidth(l.width)))
}

func (l listVolumesModel) bottomLeft() string {
	if l.filter.editing {
		return l.filter.prompt()
	}
	return l.help.View(l.keys)
}

func (l listVolumesModel) selected() *Volume {
	i := l.table.Cursor()
	if i < 0 || i >= len(l.items) {
		return nil
	}
	return &l.items[i]
}

func (l listVolumesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			m := InitIndexModel(l.dockerClient, l.width, l.height).withSelected(cardVolumes)
			return m, m.Init()
		case "/":
			l.filter.open()
			l.layout()
			return l, nil
		case "d":
			v := l.selected()
			if v == nil {
				return l, nil
			}
			if len(v.UsedBy) > 0 {
				return l, l.status.set(l.tickID, statusError,
					fmt.Sprintf("%s is used by %s. Delete those containers first.", v.Name, strings.Join(v.UsedBy, ", ")))
			}
			l.confirm = deleteVolumePrompt(l.dockerClient, *v)
			return l, nil
		case "P":
			unused := 0
			for _, v := range l.all {
				if len(v.UsedBy) == 0 {
					unused++
				}
			}
			if unused == 0 {
				return l, l.status.set(l.tickID, statusSuccess, "No unused volumes to delete")
			}
			l.confirm = pruneVolumesPrompt(l.dockerClient, unused)
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

func (l listVolumesModel) View() string {
	width := pageWidth(l.width)

	inUse := 0
	for _, v := range l.all {
		if len(v.UsedBy) > 0 {
			inUse++
		}
	}
	info := runningStyle.Render(glyphRunning) + " " + crumbStyle.Render(strconv.Itoa(inUse)) + mutedStyle.Render(" in use") +
		"   " + mutedStyle.Render(glyphStopped+" ") + crumbStyle.Render(strconv.Itoa(len(l.all)-inUse)) + mutedStyle.Render(" unused")

	title, lines := "", []detailLine(nil)
	if v := l.selected(); v != nil {
		usedBy := "No containers"
		if len(v.UsedBy) > 0 {
			usedBy = strings.Join(v.UsedBy, ", ")
		}
		title = v.Name
		lines = []detailLine{
			{"Mountpoint", v.Mountpoint},
			{"Driver", v.Driver},
			{"Used by", usedBy},
		}
		if p := v.Labels[composeStackIdentifier]; p != "" {
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
		renderHeader(width, info, withFilterCrumb(l.filter, "Volumes")...),
		tableBaseStyle.Render(l.table.View()),
		panel,
		renderBottomBar(width, l.bottomLeft(), status),
	)
}

func deleteVolumePrompt(dockerClient client.SDKClient, v Volume) *confirmPrompt {
	return &confirmPrompt{
		title:    "Delete the volume " + v.Name + "?",
		lines:    []string{"Everything stored in it is deleted for good. No container uses it."},
		progress: "Deleting " + v.Name + "…",
		run: func() tea.Msg {
			if err := dockerClient.VolumeRemove(context.Background(), v.Name, false); err != nil {
				return actionMsg{err: fmt.Errorf("couldn't delete %s: %w", v.Name, err)}
			}
			return actionMsg{done: "Deleted " + v.Name}
		},
	}
}

func pruneVolumesPrompt(dockerClient client.SDKClient, unused int) *confirmPrompt {
	noun := "volumes"
	if unused == 1 {
		noun = "volume"
	}
	return &confirmPrompt{
		title: fmt.Sprintf("Delete %d unused %s?", unused, noun),
		lines: []string{
			"Every volume no container uses goes, named ones included.",
			"Everything stored in them is deleted for good.",
		},
		progress: "Deleting unused volumes…",
		yes:      "prune",
		run: func() tea.Msg {
			// all=true: without it, newer daemons only prune anonymous volumes.
			report, err := dockerClient.VolumesPrune(context.Background(), filters.NewArgs(filters.Arg("all", "true")))
			if err != nil {
				return actionMsg{err: fmt.Errorf("couldn't prune volumes: %w", err)}
			}
			return actionMsg{done: fmt.Sprintf("Deleted %d volumes, freed %s",
				len(report.VolumesDeleted), humanSize(int64(report.SpaceReclaimed)))}
		},
	}
}
