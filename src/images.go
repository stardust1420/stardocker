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
	imageTypes "github.com/docker/docker/api/types/image"
	"github.com/docker/go-sdk/client"
)

// List Images Model

type listImagesModel struct {
	help         help.Model
	keys         imagesKeyMap
	dockerClient client.SDKClient
	width        int
	height       int
	table        table.Model
	items        []Image // items[i] is the image shown in table row i
	tickID       int
	status       statusLine
	listErr      string         // set while listing images fails
	confirm      *confirmPrompt // pending delete, if any
	imageCount   int            // distinct images, for the header
	diskUsage    int64          // their total size
}

// Image is one row of the images table. An image with several tags appears
// once per tag, like `docker images`.
type Image struct {
	ID         string
	Repository string
	Tag        string
	Size       int64
	Created    time.Time
	Containers int
	UsedBy     []string // names of the containers created from this image
}

// imageColumns: Repository and Used by keep their width longest.
var imageColumns = []colSpec{
	{title: " ", min: 1, max: 1},
	{title: "Repository", min: 15, max: 50, shrinkOrder: 3, grow: 3},
	{title: "Tag", min: 8, max: 25, shrinkOrder: 2, grow: 1},
	{title: "Image ID", min: 12, max: 12},
	{title: "Size", min: 7, max: 8},
	{title: "Created", min: 10, max: 16, shrinkOrder: 1},
	{title: "Containers", min: 10, max: 10},
	{title: "Used by", min: 10, max: 60, shrinkOrder: 2, grow: 3},
}

func InitListImagesModel(dockerClient client.SDKClient, width int, height int) listImagesModel {
	t := table.New(
		table.WithColumns(fitColumns(imageColumns, nil, pageWidth(width))),
		table.WithFocused(true),
		table.WithHeight(listTableHeight(height, 1)),
	)
	t.SetStyles(tableStyles())

	return listImagesModel{
		help:         newHelp(),
		keys:         imagesKeys,
		dockerClient: dockerClient,
		width:        width,
		height:       height,
		table:        t,
		tickID:       newTickID(),
	}
}

func (l listImagesModel) Init() tea.Cmd {
	// Fire the first tick right away so the table fills immediately; each
	// tick then re-arms itself.
	id := l.tickID
	return tea.Batch(
		tea.SetWindowTitle("Images"),
		func() tea.Msg { return tickMsg{id: id, t: time.Now()} },
	)
}

func (l *listImagesModel) refresh() {
	images, err := FetchImages(l.dockerClient)
	if err != nil {
		l.listErr = fmt.Sprintf("Can't list images: %v", err)
		return
	}
	l.listErr = ""

	// Keep the cursor on the same image tag, not the same row number.
	prev := ""
	if i := l.table.Cursor(); i >= 0 && i < len(l.items) {
		prev = imageRef(l.items[i])
	}

	rows := imageRows(images, time.Now())
	l.items = images
	l.table.SetRows(rows)
	cursor := min(l.table.Cursor(), max(0, len(rows)-1)) // SetRows doesn't clamp
	for i, img := range images {
		if prev != "" && imageRef(img) == prev {
			cursor = i
			break
		}
	}
	l.table.SetCursor(cursor)

	// An image with several tags has several rows; count it once.
	seen := make(map[string]bool)
	l.imageCount, l.diskUsage = 0, 0
	for _, img := range images {
		if !seen[img.ID] {
			seen[img.ID] = true
			l.imageCount++
			l.diskUsage += img.Size
		}
	}

	l.layout()
}

// layout fits the table to the window: full page width, and whatever height
// is left after the header, details panel and help.
func (l *listImagesModel) layout() {
	l.table.SetHeight(listTableHeight(l.height, lipgloss.Height(l.help.View(l.keys))))
	l.table.SetColumns(fitColumns(imageColumns, l.table.Rows(), pageWidth(l.width)))
}

func (l listImagesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		l.width = msg.Width
		l.height = msg.Height
		l.layout()
		return l, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return l, tea.Quit
		}
		// While a delete is waiting for confirmation, only y / n / esc count.
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

		// Handle help
		switch {
		case key.Matches(msg, l.keys.Help):
			l.help.ShowAll = !l.help.ShowAll
			l.layout()
		}

		switch msg.String() {
		case "q":
			return l, tea.Quit
		case "esc":
			m := InitIndexModel(l.dockerClient, l.width, l.height).withSelected(cardImages)
			return m, m.Init()
		case "d":
			i := l.table.Cursor()
			if i < 0 || i >= len(l.items) {
				return l, nil
			}
			prompt, refusal := deleteImagePrompt(l.dockerClient, l.items[i], l.items)
			if refusal != "" {
				return l, l.status.set(l.tickID, statusError, refusal)
			}
			l.confirm = prompt
			return l, nil
		}

	case imageActionMsg:
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

	l.table, cmd = l.table.Update(msg)

	return l, cmd
}

func (l listImagesModel) View() string {
	width := pageWidth(l.width)
	title, lines := l.details()

	noun := "images"
	if l.imageCount == 1 {
		noun = "image"
	}
	info := crumbStyle.Render(strconv.Itoa(l.imageCount)) + mutedStyle.Render(" "+noun+"   ") +
		crumbStyle.Render(humanSize(l.diskUsage)) + mutedStyle.Render(" on disk")

	panel := renderPanel(width, title, detailsBody(width, lines), detailsLines)
	if l.confirm != nil {
		panel = renderConfirmPanel(width, l.confirm)
	}

	status := l.status.render()
	if l.listErr != "" {
		status = statusLine{text: l.listErr, kind: statusError}.render()
	}

	return renderListPage(
		renderHeader(width, info, "Images"),
		tableBaseStyle.Render(l.table.View()),
		panel,
		renderBottomBar(width, l.help.View(l.keys), status),
	)
}

// details describes the selected image in full, so nothing is lost when
// table columns are truncated. It returns the panel title and its lines.
func (l listImagesModel) details() (string, []detailLine) {
	i := l.table.Cursor()
	if i < 0 || i >= len(l.items) {
		return "", nil
	}
	img := l.items[i]

	usedBy := "No containers"
	if len(img.UsedBy) > 0 {
		usedBy = strings.Join(img.UsedBy, ", ")
	}

	return img.Repository + ":" + img.Tag, []detailLine{
		{"ID", img.ID},
		{"Size", humanSize(img.Size)},
		{"Created", fmt.Sprintf("%s, %s ago", img.Created.Format("2 Jan 2006 15:04"), humanDuration(time.Since(img.Created)))},
		{"Used by", usedBy},
	}
}

// imageActionMsg reports the result of an async image delete.
type imageActionMsg struct {
	err  error
	done string
}

// imageRef is what to pass to Docker to delete this row: the repo:tag, or
// the ID for an untagged image.
func imageRef(img Image) string {
	if img.Repository == "<none>" {
		return img.ID
	}
	return img.Repository + ":" + img.Tag
}

// imageLabel is how the image is named to the user.
func imageLabel(img Image) string {
	if img.Repository == "<none>" {
		return "untagged image " + shortImageID(img.ID)
	}
	return img.Repository + ":" + img.Tag
}

// deleteImagePrompt builds the confirmation for deleting the image in this
// row. It refuses (returning a message instead) to delete an image that
// containers still use, because Docker would refuse too unless forced, and
// forcing would leave those containers pointing at a missing image.
func deleteImagePrompt(dockerClient client.SDKClient, img Image, all []Image) (*confirmPrompt, string) {
	label := imageLabel(img)

	// Other rows with the same ID are other tags of the same image.
	var otherTags []string
	for _, other := range all {
		if other.ID == img.ID && imageRef(other) != imageRef(img) {
			otherTags = append(otherTags, imageLabel(other))
		}
	}

	// Removing one of several tags never deletes the image, so it's safe
	// even while containers use it. Deleting the image itself is not.
	if len(otherTags) == 0 && img.Containers > 0 {
		noun := "containers"
		if img.Containers == 1 {
			noun = "container"
		}
		return nil, fmt.Sprintf("%s is in use by %d %s. Delete those first.", label, img.Containers, noun)
	}

	if len(otherTags) > 0 {
		return &confirmPrompt{
			title:    "Remove the tag " + label + "?",
			lines:    []string{"Only this tag goes. The image stays, because it's also tagged " + strings.Join(otherTags, ", ") + "."},
			progress: "Removing " + label + "…",
			run:      deleteImageCmd(dockerClient, imageRef(img), "Removed the tag "+label),
		}, ""
	}

	return &confirmPrompt{
		title: "Delete " + label + "?",
		lines: []string{
			"Frees " + humanSize(img.Size) + ". You'd need to pull or build it again to use it.",
		},
		progress: "Deleting " + label + "…",
		run:      deleteImageCmd(dockerClient, imageRef(img), "Deleted "+label),
	}, ""
}

func deleteImageCmd(dockerClient client.SDKClient, ref, done string) tea.Cmd {
	return func() tea.Msg {
		_, err := dockerClient.ImageRemove(context.Background(), ref, imageTypes.RemoveOptions{PruneChildren: true})
		if err != nil {
			return imageActionMsg{err: fmt.Errorf("couldn't delete %s: %w", ref, err)}
		}
		return imageActionMsg{done: done}
	}
}

// FetchImages lists local images, one entry per tag, with the number of
// containers (running or not) created from each image.
func FetchImages(dockerClient client.SDKClient) ([]Image, error) {
	ctx := context.TODO()

	summaries, err := dockerClient.ImageList(ctx, imageTypes.ListOptions{})
	if err != nil {
		return nil, err
	}

	containers, err := dockerClient.ContainerList(ctx, containerTypes.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	usedBy := make(map[string][]string)
	for _, c := range containers {
		usedBy[c.ImageID] = append(usedBy[c.ImageID], containerDisplayName(c.Names, c.ID))
	}
	for _, names := range usedBy {
		sort.Strings(names)
	}

	images := make([]Image, 0, len(summaries))
	for _, s := range summaries {
		tags := s.RepoTags
		if len(tags) == 0 {
			tags = []string{"<none>:<none>"}
		}
		for _, ref := range tags {
			repo, tag := splitImageRef(ref)
			images = append(images, Image{
				ID:         s.ID,
				Repository: repo,
				Tag:        tag,
				Size:       s.Size,
				Created:    time.Unix(s.Created, 0),
				Containers: len(usedBy[s.ID]),
				UsedBy:     usedBy[s.ID],
			})
		}
	}

	// Named images first, alphabetically; dangling "<none>" images last.
	sort.SliceStable(images, func(i, j int) bool {
		a, b := images[i], images[j]
		if (a.Repository == "<none>") != (b.Repository == "<none>") {
			return b.Repository == "<none>"
		}
		if a.Repository != b.Repository {
			return a.Repository < b.Repository
		}
		return a.Tag < b.Tag
	})

	return images, nil
}

func imageRows(images []Image, now time.Time) []table.Row {
	rows := make([]table.Row, 0, len(images))
	for _, img := range images {
		indicator := glyphStopped
		if img.Containers > 0 {
			indicator = glyphRunning
		}
		rows = append(rows, table.Row{
			indicator,
			img.Repository,
			img.Tag,
			shortImageID(img.ID),
			humanSize(img.Size),
			humanDuration(now.Sub(img.Created)) + " ago",
			strconv.Itoa(img.Containers),
			strings.Join(img.UsedBy, ", "),
		})
	}
	return rows
}

// splitImageRef splits "registry:5000/repo:tag" into repo and tag. The tag
// is after the last colon, but only if that colon comes after the last slash
// (otherwise it is a registry port).
func splitImageRef(ref string) (string, string) {
	i := strings.LastIndex(ref, ":")
	if i < 0 || i < strings.LastIndex(ref, "/") {
		return ref, "<none>"
	}
	return ref[:i], ref[i+1:]
}

func shortImageID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// humanSize formats bytes using decimal units, as the docker CLI does.
func humanSize(b int64) string {
	units := []string{"B", "kB", "MB", "GB", "TB"}
	v := float64(b)
	i := 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%dB", b)
	}
	return fmt.Sprintf("%.3g%s", v, units[i])
}

func humanDuration(d time.Duration) string {
	plural := func(n int, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return fmt.Sprintf("%d %ss", n, unit)
	}
	switch {
	case d < time.Minute:
		return "a few seconds"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 48*time.Hour:
		return plural(int(d.Hours()), "hour")
	case d < 14*24*time.Hour:
		return plural(int(d.Hours()/24), "day")
	case d < 60*24*time.Hour:
		return plural(int(d.Hours()/24/7), "week")
	case d < 365*24*time.Hour:
		return plural(int(d.Hours()/24/30), "month")
	default:
		return plural(int(d.Hours()/24/365), "year")
	}
}
