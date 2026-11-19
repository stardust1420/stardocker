package src

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	imageTypes "github.com/docker/docker/api/types/image"
	"github.com/docker/go-sdk/client"
)

// List Images Model

type listImagesModel struct {
	help         help.Model
	keys         listKeyMap
	dockerClient client.SDKClient
	width        int
	height       int
	grid         gridView
	all          []Image // the latest fetch, before filtering and sorting
	items        []Image // items[i] is the image on grid card i
	tickID       int
	status       statusLine
	listErr      string         // set while listing images fails
	confirm      *confirmPrompt // pending delete or prune, if any
	imageCount   int            // distinct images, for the header
	diskUsage    int64          // their total size
	filter       filterBar
	sortBy       int        // index into imageSorts
	pullInput    *textField // the "pull image" field while it is open
	pulling      bool       // a pull is running; its progress is the status
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

// imageSorts are the orders "o" cycles through. The first keeps the order
// FetchImages returns (by name, untagged last).
var imageSorts = []struct {
	label string
	less  func(a, b Image) bool
}{
	{"name", nil},
	{"size", func(a, b Image) bool { return a.Size > b.Size }},
	{"newest", func(a, b Image) bool { return a.Created.After(b.Created) }},
}

func InitListImagesModel(dockerClient client.SDKClient, width int, height int) listImagesModel {
	l := listImagesModel{
		help:         newHelp(),
		keys:         imagesKeys,
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
	l.all = images

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

	l.rebuild()
}

// visible applies the filter and sort to the last fetch.
func (l listImagesModel) visible() []Image {
	q := l.filter.query()
	out := make([]Image, 0, len(l.all))
	for _, img := range l.all {
		if matches(q, img.Repository, img.Tag, shortImageID(img.ID), strings.Join(img.UsedBy, " ")) {
			out = append(out, img)
		}
	}
	if less := imageSorts[l.sortBy].less; less != nil {
		sort.SliceStable(out, func(i, j int) bool { return less(out[i], out[j]) })
	}
	return out
}

// rebuild fills the grid. The grid keeps the cursor on the same image tag
// (by imageRef), not the same position.
func (l *listImagesModel) rebuild() {
	images := l.visible()
	l.items = images
	l.layout()
	l.grid.setEntries(imageCards(images, time.Now()))
}

// layout fits the grid to the window: full page width, and whatever height
// is left after the header, the help and any confirmation.
func (l *listImagesModel) layout() {
	l.grid.resize(pageWidth(l.width), listGridHeight(l.height, lipgloss.Height(l.bottomLeft()), l.confirm != nil))
}

// selected is the image under the cursor, or nil.
func (l listImagesModel) selected() *Image {
	i := l.grid.cursor
	if i < 0 || i >= len(l.items) {
		return nil
	}
	return &l.items[i]
}

// bottomLeft is the help, or the field being typed in.
func (l listImagesModel) bottomLeft() string {
	switch {
	case l.pullInput != nil:
		hint := func(k, desc string) string { return crumbStyle.Render(k) + " " + mutedStyle.Render(desc) }
		return mutedStyle.Render("Pull ") + l.pullInput.view() + "   " + hint("enter", "pull") + "   " + hint("esc", "cancel")
	case l.filter.editing:
		return l.filter.prompt()
	}
	return l.help.View(l.keys)
}

func (l listImagesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
				l.layout()
			}
			return l, run
		}
		if l.pullInput != nil {
			return l.updatePullInput(msg)
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
			m := InitIndexModel(l.dockerClient, l.width, l.height).withSelected(cardImages)
			return m, m.Init()
		case "/":
			l.filter.open()
			l.layout()
			return l, nil
		case "o":
			l.sortBy = (l.sortBy + 1) % len(imageSorts)
			l.rebuild()
			return l, nil
		case "p":
			if l.pulling {
				return l, nil
			}
			f := textField{limit: 200, accept: func(r rune, _ string) bool { return r != ' ' }}
			if img := l.selected(); img != nil && img.Repository != "<none>" {
				f.value = imageRef(*img)
			}
			l.pullInput = &f
			l.layout()
			return l, nil
		case "P":
			dangling := 0
			for _, img := range l.all {
				if img.Repository == "<none>" {
					dangling++
				}
			}
			prompt, refusal := pruneImagesPrompt(l.dockerClient, dangling)
			if refusal != "" {
				return l, l.status.set(l.tickID, statusSuccess, refusal)
			}
			l.confirm = prompt
			l.layout()
			return l, nil
		case "d":
			img := l.selected()
			if img == nil {
				return l, nil
			}
			prompt, refusal := deleteImagePrompt(l.dockerClient, *img, l.all)
			if refusal != "" {
				return l, l.status.set(l.tickID, statusError, refusal)
			}
			l.confirm = prompt
			l.layout()
			return l, nil
		}
		l.grid.handleKey(msg)
		return l, nil

	case pullProgressMsg:
		l.status.set(l.tickID, statusProgress, msg.text)
		return l, waitPull(msg.ch)

	case imageActionMsg:
		l.pulling = false
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

// updatePullInput handles keys while the "pull image" field is open.
func (l listImagesModel) updatePullInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		l.pullInput = nil
		l.layout()
		return l, nil
	case "enter":
		ref := strings.TrimSpace(l.pullInput.value)
		l.pullInput = nil
		l.layout()
		if ref == "" {
			return l, nil
		}
		l.pulling = true
		l.status.set(l.tickID, statusProgress, "Pulling "+ref+"…")
		return l, pullImageCmd(l.dockerClient, ref)
	}
	l.pullInput.update(msg)
	return l, nil
}

func (l listImagesModel) View() string {
	width := pageWidth(l.width)

	noun := "images"
	if l.imageCount == 1 {
		noun = "image"
	}
	info := crumbStyle.Render(strconv.Itoa(l.imageCount)) + mutedStyle.Render(" "+noun+"   ") +
		crumbStyle.Render(humanSize(l.diskUsage)) + mutedStyle.Render(" on disk")
	if l.sortBy > 0 {
		info = mutedStyle.Render("by "+imageSorts[l.sortBy].label+"   ") + info
	}

	panel := ""
	if l.confirm != nil {
		panel = renderConfirmPanel(width, l.confirm)
	}

	status := l.status.render()
	if l.listErr != "" {
		status = statusLine{text: l.listErr, kind: statusError}.render()
	}

	empty := "No images yet. Press p to pull one."
	if l.filter.query() != "" {
		empty = "Nothing matches /" + l.filter.query() + ". Press esc to clear the filter."
	}

	return renderGridPage(
		renderHeader(width, info, withFilterCrumb(l.filter, "Images")...),
		l.grid.view(empty),
		panel,
		renderBottomBar(width, l.bottomLeft(), status),
	)
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

// imageCards shows each image tag: size, age, the short ID, and the
// containers created from it.
func imageCards(images []Image, now time.Time) []gridEntry {
	entries := make([]gridEntry, 0, len(images))
	for _, img := range images {
		glyph := glyphStopped
		if img.Containers > 0 {
			glyph = glyphRunning
		}
		title := img.Repository + ":" + img.Tag
		if img.Repository == "<none>" {
			title = "untagged"
		}
		entries = append(entries, gridEntry{
			key:        imageRef(img),
			selectable: true,
			glyph:      glyphFor(glyph),
			title:      title,
			body: []string{
				valueLabel(humanSize(img.Size), "on disk"),
				mutedStyle.Render("created " + humanDuration(now.Sub(img.Created)) + " ago"),
				mutedStyle.Render(shortImageID(img.ID)),
				usedByLine(img.UsedBy, "No containers use it"),
			},
		})
	}
	return entries
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

// pullProgressMsg carries the latest pull progress line; ch delivers the
// next one.
type pullProgressMsg struct {
	text string
	ch   pullChannels
}

// pullChannels connect a running pull to the screen. progress holds only
// the latest line and done the result, both buffered, so the pull never
// blocks if the user has left the images screen meanwhile.
type pullChannels struct {
	progress chan string
	done     chan imageActionMsg
}

// pullImageCmd starts pulling ref and returns its first message.
func pullImageCmd(dockerClient client.SDKClient, ref string) tea.Cmd {
	ch := pullChannels{progress: make(chan string, 1), done: make(chan imageActionMsg, 1)}
	go func() {
		ch.done <- runPull(dockerClient, ref, func(line string) {
			// Replace any line the screen hasn't picked up yet.
			select {
			case <-ch.progress:
			default:
			}
			ch.progress <- line
		})
	}()
	return waitPull(ch)
}

// waitPull waits for the next progress line or the result.
func waitPull(ch pullChannels) tea.Cmd {
	return func() tea.Msg {
		select {
		case res := <-ch.done:
			return res
		case line := <-ch.progress:
			return pullProgressMsg{text: line, ch: ch}
		}
	}
}

// pullMessage is the part of a pull progress message that matters here.
type pullMessage struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

// layerStatuses are the statuses the daemon reports per layer; other
// messages (e.g. "Pulling from library/nginx", whose ID is the tag) are
// about the image as a whole.
var layerStatuses = map[string]bool{
	"Pulling fs layer": true, "Waiting": true, "Downloading": true, "Verifying Checksum": true,
	"Download complete": true, "Extracting": true, "Pull complete": true, "Already exists": true,
}

// runPull pulls ref, reporting a summary line such as
// "Pulling nginx:latest… 3 of 7 layers done" as layers complete.
func runPull(dockerClient client.SDKClient, ref string, report func(string)) imageActionMsg {
	if !strings.Contains(ref, ":") && !strings.Contains(ref, "@") {
		ref += ":latest"
	}
	rc, err := dockerClient.ImagePull(context.Background(), ref, imageTypes.PullOptions{})
	if err != nil {
		return imageActionMsg{err: fmt.Errorf("couldn't pull %s: %w", ref, err)}
	}
	defer rc.Close()

	layers := map[string]string{} // layer ID → latest status
	dec := json.NewDecoder(rc)
	for {
		var m pullMessage
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return imageActionMsg{err: fmt.Errorf("couldn't pull %s: %w", ref, err)}
		}
		if m.Error != "" {
			return imageActionMsg{err: fmt.Errorf("couldn't pull %s: %s", ref, m.Error)}
		}
		if m.ID != "" && layerStatuses[m.Status] {
			layers[m.ID] = m.Status
		}
		done := 0
		for _, st := range layers {
			if st == "Pull complete" || st == "Already exists" {
				done++
			}
		}
		line := "Pulling " + ref + "…"
		if len(layers) > 0 {
			line += fmt.Sprintf(" %d of %d layers done", done, len(layers))
		}
		report(line)
	}
	return imageActionMsg{done: "Pulled " + ref}
}

// pruneImagesPrompt builds the confirmation for deleting dangling
// (untagged) images. refusal is set when there are none.
func pruneImagesPrompt(dockerClient client.SDKClient, dangling int) (*confirmPrompt, string) {
	if dangling == 0 {
		return nil, "No dangling images to delete"
	}
	noun := "images"
	if dangling == 1 {
		noun = "image"
	}
	return &confirmPrompt{
		title:    fmt.Sprintf("Delete %d dangling %s?", dangling, noun),
		lines:    []string{"Untagged images that no container uses are removed. Tagged images are kept."},
		progress: "Deleting dangling images…",
		yes:      "prune",
		run: func() tea.Msg {
			report, err := dockerClient.ImagesPrune(context.Background(), filters.NewArgs(filters.Arg("dangling", "true")))
			if err != nil {
				return imageActionMsg{err: fmt.Errorf("couldn't prune images: %w", err)}
			}
			return imageActionMsg{done: fmt.Sprintf("Deleted %d dangling images, freed %s",
				len(report.ImagesDeleted), humanSize(int64(report.SpaceReclaimed)))}
		},
	}, ""
}
