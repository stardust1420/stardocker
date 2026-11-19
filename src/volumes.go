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

type listVolumesModel struct {
	help         help.Model
	keys         listKeyMap
	dockerClient client.SDKClient
	width        int
	height       int
	grid         gridView
	all          []Volume
	items        []Volume // items[i] is the volume on grid card i
	tickID       int
	status       statusLine
	listErr      string
	confirm      *confirmPrompt
	filter       filterBar
}

func InitListVolumesModel(dockerClient client.SDKClient, width, height int) listVolumesModel {
	l := listVolumesModel{
		help:         newHelp(),
		keys:         volumesKeys,
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

// rebuild fills the grid with the volumes that pass the filter. The grid
// keeps the cursor on the same volume (by name).
func (l *listVolumesModel) rebuild() {
	q := l.filter.query()
	var items []Volume
	var entries []gridEntry
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
		body := []string{valueLabel(v.Driver, "driver")}
		if project != "" {
			body = append(body, mutedStyle.Render("project ")+textLine(project))
		}
		if !v.Created.IsZero() {
			body = append(body, mutedStyle.Render("created "+humanDuration(now.Sub(v.Created))+" ago"))
		}
		body = append(body, usedByLine(v.UsedBy, "No containers use it"))
		entries = append(entries, gridEntry{
			key:        v.Name,
			selectable: true,
			glyph:      glyphFor(glyph),
			title:      v.Name,
			body:       body,
		})
		items = append(items, v)
	}
	l.items = items
	l.layout()
	l.grid.setEntries(entries)
}

func (l *listVolumesModel) layout() {
	l.grid.resize(pageWidth(l.width), listGridHeight(l.height, lipgloss.Height(l.bottomLeft()), l.confirm != nil))
}

func (l listVolumesModel) bottomLeft() string {
	if l.filter.editing {
		return l.filter.prompt()
	}
	return l.help.View(l.keys)
}

func (l listVolumesModel) selected() *Volume {
	i := l.grid.cursor
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
			l.layout()
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

	panel := ""
	if l.confirm != nil {
		panel = renderConfirmPanel(width, l.confirm)
	}
	status := l.status.render()
	if l.listErr != "" {
		status = statusLine{text: l.listErr, kind: statusError}.render()
	}
	empty := "No volumes. Compose and docker run -v create them."
	if l.filter.query() != "" {
		empty = "Nothing matches /" + l.filter.query() + ". Press esc to clear the filter."
	}
	return renderGridPage(
		renderHeader(width, info, withFilterCrumb(l.filter, "Volumes")...),
		l.grid.view(empty),
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
