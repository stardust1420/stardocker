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

// containerActionMsg reports the result of an async start, stop or delete.
// done is the success message to show; empty means just clear the status.
type containerActionMsg struct {
	err  error
	done string
}

// List Containers Model

type listContainersModel struct {
	help         help.Model
	keys         containersKeyMap
	dockerClient client.SDKClient
	width        int
	height       int
	grid         gridView
	all          []Container // the latest fetch, before filtering and sorting
	items        []Container // items[i] is what grid entry i shows (zero for a plain heading)
	Collapsed    StringSet   // stacks whose services are hidden
	tickID       int
	status       statusLine
	listErr      string         // set while listing containers fails
	confirm      *confirmPrompt // pending delete or prune, if any
	running      int            // header counts, across stacks and standalone containers
	stopped      int
	filter       filterBar
	sortBy       int  // index into containerSorts
	runningOnly  bool // "a" hides containers that aren't running
	// stats are the CPU and memory figures by container ID; samples are the
	// raw readings they were computed from, kept for the next CPU delta.
	stats         map[string]containerStats
	samples       map[string]cpuSample
	statsInFlight bool
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

// containerSorts are the orders "o" cycles through.
var containerSorts = []struct {
	label string
	less  func(l *listContainersModel, a, b Container) bool
}{
	{"name", func(_ *listContainersModel, a, b Container) bool { return a.Name < b.Name }},
	{"CPU", func(l *listContainersModel, a, b Container) bool { return l.cpuOf(a) > l.cpuOf(b) }},
	{"memory", func(l *listContainersModel, a, b Container) bool { return l.memOf(a) > l.memOf(b) }},
	{"newest", func(_ *listContainersModel, a, b Container) bool { return createdOf(a).After(createdOf(b)) }},
}

func InitListContainersModel(dockerClient client.SDKClient, width int, height int) listContainersModel {
	l := listContainersModel{
		help:         newHelp(),
		keys:         containersKeys,
		dockerClient: dockerClient,
		width:        width,
		height:       height,
		grid:         gridView{cursor: -1},
		Collapsed:    make(StringSet),
		tickID:       newTickID(),
		filter:       newFilterBar(),
	}
	l.layout()
	return l
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

// refresh fetches the containers and rebuilds the table.
func (l *listContainersModel) refresh() {
	allContainers, err := FetchContainers(l.dockerClient)
	if err != nil {
		l.listErr = fmt.Sprintf("Can't list containers: %v", err)
		return
	}
	l.listErr = ""
	l.all = allContainers

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

	l.rebuild()
}

// rebuild fills the grid from the last fetch with the current filter and
// sort. The grid keeps the cursor on the same container or stack (by
// itemKey), not the same position, so cards coming and going don't move it.
func (l *listContainersModel) rebuild() {
	entries, items := l.entries(l.visible(l.all))
	l.items = items
	l.layout()
	l.grid.setEntries(entries)
}

// itemKey identifies a row's container or stack across refreshes.
func itemKey(c Container) string {
	if c.Type == TypeComposeStack {
		return "stack:" + c.Name
	}
	return "container:" + c.ID
}

// resume prepares a containers screen that is being returned to (from the
// logs or inspect view) with its cursor and expanded stacks intact. It gets
// a new tick id so a tick left over from before can't start a second
// refresh loop, and the current window size in case the terminal was
// resized meanwhile.
func (l listContainersModel) resume(width, height int) listContainersModel {
	l.width, l.height = width, height
	l.tickID = newTickID()
	l.confirm = nil
	l.statsInFlight = false // a fetch from before belongs to the old tick id
	l.layout()
	return l
}

// layout fits the grid to the window: full page width, and whatever height
// is left after the header, the help and any confirmation.
func (l *listContainersModel) layout() {
	l.grid.resize(pageWidth(l.width), listGridHeight(l.height, lipgloss.Height(l.bottomLeft()), l.confirm != nil))
}

// bottomLeft is the help, or the filter field while it is being edited.
func (l listContainersModel) bottomLeft() string {
	if l.filter.editing {
		return l.filter.prompt()
	}
	return l.help.View(l.keys)
}

// selected returns the container or stack under the cursor, or nil when
// there is none.
func (l listContainersModel) selected() *Container {
	i := l.grid.cursor
	if i < 0 || i >= len(l.items) || !l.grid.entries[i].selectable {
		return nil
	}
	return &l.items[i]
}

// passes reports whether a single container survives the running-only
// toggle and the filter.
func (l listContainersModel) passes(c Container, q string) bool {
	if l.runningOnly && c.State != containerTypes.StateRunning {
		return false
	}
	return matches(q, c.Name, c.Image, c.ID, c.Status, formatPorts(c.Ports))
}

// visibleChildren are the services of a stack to list under it: all of
// them if the stack's own name matches the filter, otherwise the ones that
// match. The running-only toggle applies either way.
func (l listContainersModel) visibleChildren(stack Container) []Container {
	q := l.filter.query()
	nameMatches := matches(q, stack.Name)
	var out []Container
	for _, c := range stack.Children {
		if nameMatches {
			if !l.runningOnly || c.State == containerTypes.StateRunning {
				out = append(out, c)
			}
		} else if l.passes(c, q) {
			out = append(out, c)
		}
	}
	l.sortContainers(out)
	return out
}

// expanded reports whether a stack's services are shown under it: unless
// the user collapsed it, or always when the filter matched services inside.
func (l listContainersModel) expanded(stack Container) bool {
	if !l.Collapsed.Contains(stack.Name) {
		return true
	}
	q := l.filter.query()
	return q != "" && !matches(q, stack.Name)
}

// visible applies the filter, running-only toggle and sort to the top-level
// rows. A stack stays if any of its services does.
func (l listContainersModel) visible(all []Container) []Container {
	q := l.filter.query()
	out := make([]Container, 0, len(all))
	for _, c := range all {
		if c.Type == TypeComposeStack {
			if len(l.visibleChildren(c)) > 0 {
				out = append(out, c)
			}
			continue
		}
		if l.passes(c, q) {
			out = append(out, c)
		}
	}
	l.sortContainers(out)
	return out
}

func (l listContainersModel) sortContainers(cs []Container) {
	if l.sortBy <= 0 || l.sortBy >= len(containerSorts) {
		sort.SliceStable(cs, func(i, j int) bool { return cs[i].Name < cs[j].Name })
		return
	}
	less := containerSorts[l.sortBy].less
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if less(&l, a, b) {
			return true
		}
		if less(&l, b, a) {
			return false
		}
		return a.Name < b.Name
	})
}

// statsOf is the CPU and memory of a container, or the sum over a stack's
// services. ok is false when nothing is known.
func (l *listContainersModel) statsOf(c Container) (containerStats, bool) {
	if c.Type != TypeComposeStack {
		s, ok := l.stats[c.ID]
		return s, ok
	}
	var sum containerStats
	found := false
	for _, child := range c.Children {
		if s, ok := l.stats[child.ID]; ok {
			found = true
			sum.mem += s.mem
			sum.memLimit = max(sum.memLimit, s.memLimit)
			if s.hasCPU {
				sum.cpu += s.cpu
				sum.hasCPU = true
			}
		}
	}
	return sum, found
}

func (l *listContainersModel) cpuOf(c Container) float64 {
	s, _ := l.statsOf(c)
	return s.cpu
}

func (l *listContainersModel) memOf(c Container) uint64 {
	s, _ := l.statsOf(c)
	return s.mem
}

// createdOf is a container's creation time, or a stack's newest service.
func createdOf(c Container) time.Time {
	t := c.Created
	for _, child := range c.Children {
		if child.Created.After(t) {
			t = child.Created
		}
	}
	return t
}

// runningIDs are the IDs of every running container, for stats.
func runningIDs(all []Container) []string {
	var ids []string
	for _, c := range all {
		for _, x := range append([]Container{c}, c.Children...) {
			if x.Type == TypeContainer && x.State == containerTypes.StateRunning {
				ids = append(ids, x.ID)
			}
		}
	}
	return ids
}

func (l listContainersModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		// While the filter is being typed, every key goes to it.
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

		case "esc":
			// esc first clears an active filter, then goes back.
			if l.filter.query() != "" {
				l.filter.clear()
				l.rebuild()
				return l, nil
			}
			m := InitIndexModel(l.dockerClient, l.width, l.height).withSelected(cardContainers)
			return m, m.Init()

		case "/":
			l.filter.open()
			l.layout()
			return l, nil

		case "a":
			l.runningOnly = !l.runningOnly
			l.rebuild()
			return l, nil

		case "o":
			l.sortBy = (l.sortBy + 1) % len(containerSorts)
			l.rebuild()
			return l, nil

		case "d":
			if c := l.selected(); c != nil {
				l.confirm = deleteContainerPrompt(l.dockerClient, *c)
				l.layout()
			}
			return l, nil

		case "P":
			prompt, refusal := pruneContainersPrompt(l.dockerClient, l.stopped)
			if refusal != "" {
				return l, l.status.set(l.tickID, statusSuccess, refusal)
			}
			l.confirm = prompt
			l.layout()
			return l, nil

		case "enter":
			c := l.selected()
			if c == nil {
				return l, nil
			}

			switch c.Type {

			case TypeContainer:
				lm := InitLogsModel(l.dockerClient, c.ID, c.Name, l.width, l.height)
				back := l // returned as-is on esc, cursor and expanded stacks included
				lm.back = &back
				return lm, lm.Init()

			case TypeComposeStack:
				if l.Collapsed.Contains(c.Name) {
					l.Collapsed.Remove(c.Name)
				} else {
					l.Collapsed.Add(c.Name)
				}
				l.rebuild()
			}
			return l, nil

		case "i":
			c := l.selected()
			if c == nil || c.Type != TypeContainer {
				return l, nil
			}
			im := InitInspectModel(l.dockerClient, c.ID, c.Name, l.width, l.height)
			back := l
			im.back = &back
			return im, im.Init()

		case "e":
			c := l.selected()
			if c == nil || c.Type != TypeContainer {
				return l, nil
			}
			run, refusal := execShellCmd(*c)
			if refusal != "" {
				return l, l.status.set(l.tickID, statusError, refusal)
			}
			return l, run

		case "q":
			return l, tea.Quit

		case "s", "r", "p":
			c := l.selected()
			if c == nil {
				return l, nil
			}
			var (
				plan containerPlan
				ok   bool
			)
			switch msg.String() {
			case "s":
				plan, ok = planStartStop(*c)
			case "r":
				plan, ok = planRestart(*c)
			case "p":
				plan, ok = planPause(*c)
			}
			if !ok {
				return l, nil
			}
			l.status.set(l.tickID, statusProgress, plan.progress)
			return l, containerPlanCmd(l.dockerClient, plan)
		}

		l.grid.handleKey(msg)
		return l, nil

	case containerActionMsg:
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

	case statsMsg:
		if msg.tickID != l.tickID {
			return l, nil
		}
		l.statsInFlight = false
		l.stats = mergeStats(l.samples, msg.samples)
		l.samples = msg.samples
		l.rebuild()
		return l, nil

	case tickMsg:
		if msg.id != l.tickID {
			return l, nil
		}
		l.refresh()
		cmds := []tea.Cmd{tickCmd(l.tickID)}
		if !l.statsInFlight && l.dockerClient != nil {
			if ids := runningIDs(l.all); len(ids) > 0 {
				l.statsInFlight = true
				cmds = append(cmds, fetchStatsCmd(l.dockerClient, l.tickID, ids))
			} else {
				l.stats, l.samples = nil, nil
			}
		}
		return l, tea.Batch(cmds...)
	}

	return l, nil
}

func (l listContainersModel) View() string {
	width := pageWidth(l.width)

	panel := ""
	if l.confirm != nil {
		panel = renderConfirmPanel(width, l.confirm)
	}

	status := l.status.render()
	if l.listErr != "" {
		status = statusLine{text: l.listErr, kind: statusError}.render()
	}

	info := stateCounts(l.running, l.stopped)
	var mode []string
	if l.runningOnly {
		mode = append(mode, "running only")
	}
	if l.sortBy > 0 {
		mode = append(mode, "by "+containerSorts[l.sortBy].label)
	}
	if len(mode) > 0 {
		info = mutedStyle.Render(strings.Join(mode, ", ")+"   ") + info
	}

	return renderGridPage(
		renderHeader(width, info, withFilterCrumb(l.filter, "Containers")...),
		l.grid.view(l.emptyText()),
		panel,
		renderBottomBar(width, l.bottomLeft(), status),
	)
}

// emptyText is what the grid says when there is nothing to show.
func (l listContainersModel) emptyText() string {
	switch {
	case l.listErr != "" && len(l.all) == 0:
		return "Can't reach Docker. The error is at the bottom right."
	case l.filter.query() != "":
		return "Nothing matches /" + l.filter.query() + ". Press esc to clear the filter."
	case l.runningOnly && len(l.all) > 0:
		return "Nothing is running. Press a to show stopped containers too."
	}
	return "No containers yet. Create one with docker run or docker compose up."
}

// shortStatus tightens Docker's status wording so it fits a card:
// "Exited (0) About a minute ago" becomes "Exited (0) 1 minute ago".
func shortStatus(s string) string {
	s = strings.Replace(s, "About a minute", "1 minute", 1)
	s = strings.Replace(s, "About an hour", "1 hour", 1)
	return strings.Replace(s, "Less than a second", "1 second", 1)
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

// entries returns the grid entries and, in the same order, the container
// or stack each one shows (a zero Container for a plain heading). Actions
// read from the latter. Stacks come first, each a selectable heading over
// its services' cards; standalone containers follow under their own heading.
func (l *listContainersModel) entries(containers []Container) ([]gridEntry, []Container) {
	var entries []gridEntry
	var items []Container
	var standalone []Container

	for _, c := range containers {
		if c.Type != TypeComposeStack {
			standalone = append(standalone, c)
			continue
		}
		open := l.expanded(c)
		marker := "▸ "
		if open {
			marker = "▾ "
		}
		entries = append(entries, gridEntry{
			key:        itemKey(c),
			heading:    true,
			selectable: true,
			glyph:      glyphFor(stateGlyph(c.State)),
			title:      marker + c.Name,
			meta:       l.stackMeta(c),
		})
		items = append(items, c)
		if open {
			for _, child := range l.visibleChildren(c) {
				entries = append(entries, l.containerCard(child))
				items = append(items, child)
			}
		}
	}

	if len(standalone) > 0 && len(entries) > 0 {
		entries = append(entries, gridEntry{key: "heading:standalone", heading: true, title: "Standalone"})
		items = append(items, Container{})
	}
	for _, c := range standalone {
		entries = append(entries, l.containerCard(c))
		items = append(items, c)
	}
	return entries, items
}

// stackMeta is the summary after a stack's name: services running, the
// stack's total CPU and memory.
func (l *listContainersModel) stackMeta(c Container) string {
	running := 0
	for _, child := range c.Children {
		if child.State == containerTypes.StateRunning {
			running++
		}
	}
	parts := []string{valueLabel(fmt.Sprintf("%d of %d", running, len(c.Children)), "running")}
	s, ok := l.statsOf(c)
	if cpu := formatCPU(s, ok); cpu != "" {
		parts = append(parts, valueLabel(cpu, "CPU"))
	}
	if mem := formatMem(s, ok); mem != "" {
		parts = append(parts, valueLabel(mem, "memory"))
	}
	return strings.Join(parts, "   ")
}

// containerCard shows a container: image, published ports, live CPU and
// memory, then its status. A stopped container has no ports or stats, so
// it shows just its image and status.
func (l *listContainersModel) containerCard(c Container) gridEntry {
	body := []string{textLine(c.Image)}

	if ports := strings.ReplaceAll(formatPorts(c.Ports), "->", "→"); ports != "" {
		body = append(body, textLine(ports))
	} else if c.State == containerTypes.StateRunning {
		body = append(body, mutedStyle.Render("No published ports"))
	}

	var stats []string
	s, ok := l.statsOf(c)
	if cpu := formatCPU(s, ok); cpu != "" {
		stats = append(stats, valueLabel(cpu, "CPU"))
	}
	if mem := formatMem(s, ok); mem != "" {
		stats = append(stats, valueLabel(mem, "memory"))
	}
	if len(stats) > 0 {
		body = append(body, strings.Join(stats, "   "))
	}

	body = append(body, mutedStyle.Render(shortStatus(c.Status)))

	return gridEntry{
		key:        itemKey(c),
		selectable: true,
		glyph:      glyphFor(stateGlyph(c.State)),
		title:      c.Name,
		body:       body,
	}
}

// deleteContainerPrompt builds the confirmation for deleting a container,
// or every container of a compose stack.
func deleteContainerPrompt(dockerClient client.SDKClient, c Container) *confirmPrompt {
	if c.Type == TypeComposeStack {
		running := 0
		for _, child := range c.Children {
			if child.State == containerTypes.StateRunning {
				running++
			}
		}
		noun := "containers"
		if len(c.Children) == 1 {
			noun = "container"
		}
		lines := []string{}
		if running > 0 {
			lines = append(lines, fmt.Sprintf("%d of them are running and will be stopped first.", running))
		}
		lines = append(lines, "Their logs and any files changed inside them are lost. Volumes and networks are kept.")
		return &confirmPrompt{
			title:    fmt.Sprintf("Delete all %d %s in %s?", len(c.Children), noun, c.Name),
			lines:    lines,
			progress: "Deleting " + c.Name + "…",
			run:      deleteContainersCmd(dockerClient, c.Children, fmt.Sprintf("Deleted %d %s in %s", len(c.Children), noun, c.Name)),
		}
	}

	lines := []string{}
	if c.State == containerTypes.StateRunning {
		lines = append(lines, "It's running, so it will be stopped and then deleted.")
	}
	lines = append(lines, "Its logs and any files changed inside it are lost. Volumes are kept.")
	return &confirmPrompt{
		title:    "Delete " + c.Name + "?",
		lines:    lines,
		progress: "Deleting " + c.Name + "…",
		run:      deleteContainersCmd(dockerClient, []Container{c}, "Deleted "+c.Name),
	}
}

// deleteContainersCmd removes the containers off the UI goroutine. Running
// ones are force-removed (stopped first), which the prompt has said; volumes
// are never removed.
func deleteContainersCmd(dockerClient client.SDKClient, targets []Container, done string) tea.Cmd {
	return func() tea.Msg {
		var failed []string
		for _, t := range targets {
			err := dockerClient.ContainerRemove(context.Background(), t.ID, containerTypes.RemoveOptions{
				Force: t.State == containerTypes.StateRunning,
			})
			if err != nil {
				failed = append(failed, fmt.Sprintf("%s: %v", t.Name, err))
			}
		}
		if len(failed) > 0 {
			return containerActionMsg{err: fmt.Errorf("couldn't delete %s", strings.Join(failed, "; "))}
		}
		return containerActionMsg{done: done}
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
