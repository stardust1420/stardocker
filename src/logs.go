package src

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	containerTypes "github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-sdk/client"
	"github.com/muesli/reflow/wordwrap"
)

type logsModel struct {
	dockerClient  client.SDKClient
	containerID   string
	containerName string
	tty           bool
	state         *containerTypes.State // latest inspect result, nil if unknown
	gone          bool                  // the container was deleted while open
	// hours is how far back to show logs; "t" changes it.
	hours float64
	// clearedAfter is set by "c": only output after this daemon timestamp is
	// shown. Zero means show the last `hours`.
	clearedAfter time.Time
	// rangeInput is the "show the last N hours" field, open while
	// editingRange is true. rangeErr explains a rejected value.
	rangeInput   numberField
	editingRange bool
	rangeErr     string
	// status reports a start/stop in progress or its result.
	status statusLine
	// back is the containers screen this view was opened from; esc returns
	// to it so its cursor and expanded stacks are kept.
	back *listContainersModel
	// stream follows new output while the container runs; nil otherwise.
	stream *logStream
	// filter shows only matching lines; nowrap turns line wrapping off
	// (←/→ then scroll sideways).
	filter   filterBar
	nowrap   bool
	logs     string
	ready    bool
	viewport viewport.Model
	width    int
	height   int
	tickID   int
}

// InitLogsModel builds the logs screen. width and height are the terminal
// size known by the previous screen; Bubble Tea only sends a WindowSizeMsg at
// startup and on resize, so the viewport must be built from them here.
func InitLogsModel(dockerClient client.SDKClient, containerID string, containerName string, width int, height int) logsModel {
	l := logsModel{
		dockerClient:  dockerClient,
		containerID:   containerID,
		containerName: containerName,
		tickID:        newTickID(),
		hours:         defaultLogHours,
		rangeInput:    newRangeInput(),
		filter:        newFilterBar(),
	}
	if dockerClient != nil {
		l.inspect()
		l.logs = GetContainerLogs(dockerClient, containerID, l.tty, l.since())
		l.follow()
	}
	if width > 0 && height > 0 {
		l.setSize(width, height)
	}
	return l
}

// defaultLogHours is how far back the logs screen looks when it opens.
const defaultLogHours = 1

// maxLogHours caps the range a user can type (one year).
const maxLogHours = 24 * 365

// parseHours reads the range field: a positive number of hours, decimals
// allowed, at most maxLogHours.
func parseHours(s string) (float64, error) {
	h, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || h <= 0 {
		return 0, fmt.Errorf("Enter a number of hours, like 6 or 0.5.")
	}
	if h > maxLogHours {
		return 0, fmt.Errorf("That's more than a year. Enter %d hours or fewer.", maxLogHours)
	}
	return h, nil
}

// formatHours is the range in words: "30 minutes", "1 hour", "1.5 hours".
func formatHours(h float64) string {
	switch {
	case h < 1:
		m := int(h*60 + 0.5)
		if m <= 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", m)
	case h == 1:
		return "1 hour"
	default:
		return strconv.FormatFloat(h, 'f', -1, 64) + " hours"
	}
}

func (l logsModel) Init() tea.Cmd {
	return tea.Batch(tea.SetWindowTitle("Logs: "+l.containerName), tickCmd(l.tickID), waitLogs(l.stream))
}

// follow starts streaming new output, from just after the last line shown
// (or the chosen range if there is none), if the container is running and
// no stream is open. A stopped container's stream would only replay its
// logs and end.
func (l *logsModel) follow() {
	if l.stream != nil || l.gone || l.state == nil || !l.state.Running {
		return
	}
	since := l.since()
	if last, ok := lastLogTimestamp(l.logs); ok {
		since = last.Add(time.Nanosecond).Format(time.RFC3339Nano)
	}
	l.stream = startLogStream(l.dockerClient, l.containerID, l.tty, since)
}

// stopFollowing closes the stream, if any.
func (l *logsModel) stopFollowing() {
	l.stream.stop()
	l.stream = nil
}

// setContent redraws the viewport, staying at the bottom if it was there.
func (l *logsModel) setContent() {
	if !l.ready {
		return
	}
	atBottom := l.viewport.AtBottom()
	l.viewport.SetContent(l.wrapped())
	if atBottom {
		l.viewport.GotoBottom()
	}
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
		if l.editingRange {
			return l.updateRangeInput(msg)
		}
		if l.filter.editing {
			if l.filter.handle(msg) {
				l.viewport.SetContent(l.wrapped())
				l.viewport.GotoBottom()
			}
			return l, nil
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return l, tea.Quit
		case "/":
			l.filter.open()
			return l, nil
		case "w":
			l.nowrap = !l.nowrap
			if l.nowrap {
				l.viewport.SetHorizontalStep(8)
			} else {
				l.viewport.SetHorizontalStep(0)
				l.viewport.SetXOffset(0)
			}
			l.setContent()
			return l, nil
		case "o":
			path, n, err := saveLogs(l.containerName, l.visibleLogs())
			if err != nil {
				l.status.set(l.tickID, statusError, "Couldn't save the logs: "+err.Error())
				return l, nil
			}
			return l, l.status.set(l.tickID, statusSuccess, fmt.Sprintf("Saved %d lines to %s", n, path))
		case "e":
			if l.gone || l.state == nil {
				return l, nil
			}
			run, refusal := execShellCmd(Container{ID: l.containerID, Name: l.containerName, Type: TypeContainer, State: l.state.Status})
			if refusal != "" {
				l.status.set(l.tickID, statusError, refusal)
				return l, nil
			}
			return l, run
		case "s":
			// Start or stop this container, as on the containers screen.
			if l.gone || l.state == nil {
				return l, nil
			}
			plan, ok := planStartStop(Container{ID: l.containerID, Name: l.containerName, Type: TypeContainer, State: l.state.Status})
			if !ok {
				return l, nil
			}
			l.status.set(l.tickID, statusProgress, plan.progress)
			return l, containerPlanCmd(l.dockerClient, plan)
		case "t":
			l.editingRange = true
			l.rangeErr = ""
			l.rangeInput.value = strconv.FormatFloat(l.hours, 'f', -1, 64)
			return l, nil
		case "esc":
			if l.filter.query() != "" {
				l.filter.clear()
				l.viewport.SetContent(l.wrapped())
				l.viewport.GotoBottom()
				return l, nil
			}
			l.stopFollowing()
			if l.back != nil {
				m := l.back.resume(l.width, l.height)
				return m, m.Init()
			}
			m := InitListContainersModel(l.dockerClient, l.width, l.height)
			return m, m.Init()
		case "G", "end":
			l.viewport.GotoBottom()
			return l, nil
		case "c":
			// Hide everything shown so far. The cutoff is the daemon's own
			// timestamp on the last line, not this machine's clock, which can
			// differ from the Colima VM's.
			if last, ok := lastLogTimestamp(l.logs); ok {
				l.clearedAfter = last
			} else if l.clearedAfter.IsZero() {
				return l, nil // nothing to clear
			}
			l.logs = ""
			l.viewport.SetContent(l.wrapped())
			l.viewport.GotoBottom()
			return l, nil
		case "a":
			if l.clearedAfter.IsZero() {
				return l, nil
			}
			l.clearedAfter = time.Time{}
			return l, l.reload()
		}

	case tea.WindowSizeMsg:
		l.setSize(msg.Width, msg.Height)
		return l, nil

	case containerActionMsg:
		// Show the new state right away instead of on the next tick, and
		// follow again if the container was just started.
		l.inspect()
		var wait tea.Cmd
		if l.stream == nil {
			l.follow()
			wait = waitLogs(l.stream)
		}
		if msg.err != nil {
			l.status.set(l.tickID, statusError, msg.err.Error())
			return l, wait
		}
		if msg.done == "" {
			return l, wait
		}
		return l, tea.Batch(wait, l.status.set(l.tickID, statusSuccess, msg.done))

	case logLinesMsg:
		if l.stream == nil || msg.streamID != l.stream.id {
			return l, nil
		}
		if len(msg.lines) > 0 {
			l.logs = appendCapped(l.logs, strings.Join(msg.lines, ""))
			l.setContent()
		}
		if msg.ended {
			l.stream = nil // the next tick follows again if it runs
			return l, nil
		}
		return l, waitLogs(l.stream)

	case clearStatusMsg:
		l.status.clear(msg, l.tickID)
		return l, nil

	case tickMsg:
		if msg.id != l.tickID {
			return l, nil
		}
		// The tick only refreshes the state in the header and restarts the
		// stream if the container runs again; new lines come from the stream.
		// Once the container is gone, its last logs stay on screen.
		l.inspect()
		cmds := []tea.Cmd{tickCmd(l.tickID)}
		if l.stream == nil {
			l.follow()
			cmds = append(cmds, waitLogs(l.stream))
		}
		return l, tea.Batch(cmds...)
	}

	// Handle keyboard and mouse events in the viewport
	l.viewport, cmd = l.viewport.Update(msg)

	return l, cmd
}

// updateRangeInput handles keys while the "show the last N hours" field is
// open. Enter applies, esc cancels; everything else goes to the field.
func (l logsModel) updateRangeInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return l, tea.Quit
	case "esc":
		l.editingRange = false
		l.rangeErr = ""
		return l, nil
	case "enter":
		h, err := parseHours(l.rangeInput.value)
		if err != nil {
			l.rangeErr = err.Error()
			return l, nil
		}
		l.hours = h
		l.editingRange = false
		l.rangeErr = ""
		// A new range replaces any clear.
		l.clearedAfter = time.Time{}
		return l, l.reload()
	}
	if l.rangeInput.update(msg) {
		l.rangeErr = ""
	}
	return l, nil
}

// reload fetches the logs again for the current range, jumps to the end
// and follows from there with a new stream.
func (l *logsModel) reload() tea.Cmd {
	l.stopFollowing()
	l.logs = GetContainerLogs(l.dockerClient, l.containerID, l.tty, l.since())
	l.viewport.SetContent(l.wrapped())
	l.viewport.GotoBottom()
	l.follow()
	return waitLogs(l.stream)
}

// visibleLogs is the log text with the filter applied.
func (l logsModel) visibleLogs() string {
	q := l.filter.query()
	if q == "" {
		return l.logs
	}
	var b strings.Builder
	for _, line := range strings.SplitAfter(l.logs, "\n") {
		if line != "" && strings.Contains(strings.ToLower(line), q) {
			b.WriteString(line)
		}
	}
	return b.String()
}

// wrapped is the log text wrapped to the viewport's text width (inside the
// left padding that lines it up with the header).
func (l logsModel) wrapped() string {
	if q := l.filter.query(); q != "" {
		shown := l.visibleLogs()
		if shown == "" {
			return mutedStyle.Render("No lines match “" + l.filter.crumb()[1:] + "”. Press esc to clear the filter.")
		}
		shown = highlight(shown, q)
		if l.nowrap {
			return shown
		}
		return wordwrap.String(shown, max(1, l.viewport.Width-textIndent))
	}
	if l.logs == "" && !l.clearedAfter.IsZero() {
		return mutedStyle.Render("Cleared. New output will appear here. Press a to show the last " + formatHours(l.hours) + " again.")
	}
	if l.logs == "" {
		return mutedStyle.Render("No output in the last " + formatHours(l.hours) + ". Press t to look further back.")
	}
	if l.nowrap {
		return l.logs
	}
	return wordwrap.String(l.logs, max(1, l.viewport.Width-textIndent))
}

var matchStyle = lipgloss.NewStyle().Background(colorAccent).Foreground(colorInk)

// highlight marks every case-insensitive occurrence of q (already
// lower-case) in s.
func highlight(s, q string) string {
	lower := strings.ToLower(s)
	if q == "" || len(lower) != len(s) {
		return s // lower-casing changed byte offsets; don't risk cutting runes
	}
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(lower[i:], q)
		if j < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		b.WriteString(s[i : i+j])
		b.WriteString(matchStyle.Render(s[i+j : i+j+len(q)]))
		i += j + len(q)
	}
}

// saveLogs writes the logs to <name>-<time>.log in the current directory
// and returns its path and line count.
func saveLogs(name, logs string) (string, int, error) {
	safe := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == ' ' {
			return '-'
		}
		return r
	}, name)
	path := fmt.Sprintf("%s-%s.log", safe, time.Now().Format("20060102-150405"))
	if err := os.WriteFile(path, []byte(logs), 0o644); err != nil {
		return "", 0, err
	}
	return path, strings.Count(logs, "\n"), nil
}

// since is the LogsOptions.Since value: the chosen number of hours back, or
// just after the last line that was on screen when the view was cleared.
func (l logsModel) since() string {
	if l.clearedAfter.IsZero() {
		hours := l.hours
		if hours <= 0 {
			hours = defaultLogHours
		}
		// Docker parses this with time.ParseDuration, so "1.5h" works.
		return strconv.FormatFloat(hours, 'f', -1, 64) + "h"
	}
	// Docker includes lines stamped exactly at Since; skip the cleared one.
	return l.clearedAfter.Add(time.Nanosecond).Format(time.RFC3339Nano)
}

// lastLogTimestamp returns the Docker timestamp at the start of the last
// non-empty line (logs are fetched with Timestamps: true).
func lastLogTimestamp(logs string) (time.Time, bool) {
	lines := strings.Split(strings.TrimRight(logs, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		field, _, _ := strings.Cut(lines[i], " ")
		if t, err := time.Parse(time.RFC3339Nano, field); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func (l logsModel) View() string {
	if !l.ready {
		return "\n  Initializing..."
	}
	return fmt.Sprintf("%s\n%s\n%s", l.headerView(), l.viewport.View(), l.footerView())
}

// headerView is the breadcrumb header, with the container's state on the
// right, over a rule.
func (l logsModel) headerView() string {
	state := describeContainerState(l.state, l.gone, time.Now())
	header := renderHeader(l.viewport.Width, state, withFilterCrumb(l.filter, "Containers", l.containerName)...)
	return header + "\n" + borderStyle.Render(strings.Repeat("─", max(0, l.viewport.Width)))
}

// footerView is a rule over the key hints, with the time range, follow
// mode and scroll position on the right. While the range field is open it
// replaces the key hints.
func (l logsModel) footerView() string {
	rule := borderStyle.Render(strings.Repeat("─", max(0, l.viewport.Width)))
	hint := func(k, desc string) string { return crumbStyle.Render(k) + " " + mutedStyle.Render(desc) }
	indent := strings.Repeat(" ", textIndent)

	if l.editingRange {
		line := indent + mutedStyle.Render("Show logs from the last ") + "[" + l.rangeInput.view() + "]" +
			mutedStyle.Render(" hours") + "   " + hint("enter", "apply") + "   " + hint("esc", "cancel")
		if l.rangeErr != "" {
			line += "   " + dangerTextStyle.Render(l.rangeErr)
		}
		return rule + "\n" + line
	}

	if l.filter.editing {
		return rule + "\n" + indent + l.filter.prompt()
	}

	clearHint := hint("c", "clear")
	if !l.clearedAfter.IsZero() {
		clearHint = hint("a", "show all")
	}
	wrapHint := hint("w", "no wrap")
	if l.nowrap {
		wrapHint = hint("w", "wrap") + "  " + hint("←/→", "scroll")
	}
	// Most useful first; hints are dropped from the end when they don't fit.
	all := []string{
		hint("esc", "back"), hint("/", "filter"), hint("s", "start/stop"), hint("e", "shell"), clearHint,
		hint("t", "time range"), wrapHint, hint("o", "save"), hint("G", "end"), hint("q", "quit"),
	}
	keys := indent + strings.Join(all, "   ")
	for n := len(all) - 1; n > 2 && l.viewport.Width > 0 && lipgloss.Width(keys)+12 > l.viewport.Width; n-- {
		keys = indent + strings.Join(all[:n], "   ")
	}

	window := "last " + formatHours(l.hours)
	if !l.clearedAfter.IsZero() {
		window = "since " + l.clearedAfter.Local().Format("15:04:05")
	}
	follow := "scrolled back"
	if !l.ready || l.viewport.AtBottom() {
		follow = "following"
	}
	pct := crumbStyle.Render(fmt.Sprintf("%3.f%%", l.viewport.ScrollPercent()*100)) + indent

	// A start/stop message takes the place of the range and follow mode.
	// Otherwise use the long form when it fits, then shorter ones.
	candidates := []string{
		mutedStyle.Render(window+", "+follow+"   ") + pct,
		mutedStyle.Render(window+"   ") + pct,
		pct,
	}
	if st := l.status.render(); st != "" {
		candidates = []string{st + "   " + pct, pct}
	}
	right := candidates[len(candidates)-1]
	for _, c := range candidates {
		if l.viewport.Width <= 0 || lipgloss.Width(keys)+lipgloss.Width(c)+2 <= l.viewport.Width {
			right = c
			break
		}
	}

	gap := max(2, l.viewport.Width-lipgloss.Width(keys)-lipgloss.Width(right))
	return rule + "\n" + keys + strings.Repeat(" ", gap) + right
}

// inspect refreshes the container's state (and, the first time, whether it
// has a TTY, which decides how its logs are decoded).
func (l *logsModel) inspect() {
	info, err := l.dockerClient.ContainerInspect(context.Background(), l.containerID)
	if err != nil {
		if dockerclient.IsErrNotFound(err) {
			l.gone = true
		}
		return
	}
	l.state = info.State
	if info.Config != nil {
		l.tty = info.Config.Tty
	}
}

// describeContainerState renders the header's state line, e.g.
// "● Running for 12 minutes" or "○ Exited with code 1, 3 minutes ago".
func describeContainerState(st *containerTypes.State, gone bool, now time.Time) string {
	label := func(glyph string, glyphStyle lipgloss.Style, title, detail string) string {
		out := glyphStyle.Render(glyph) + " " + crumbStyle.Render(title)
		if detail != "" {
			out += mutedStyle.Render(" " + detail)
		}
		return out
	}
	since := func(ts string) string {
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil || t.IsZero() || t.Year() < 2000 {
			return ""
		}
		return humanDuration(now.Sub(t))
	}

	switch {
	case gone:
		return label(glyphStopped, mutedStyle, "Deleted", "(showing its last logs)")
	case st == nil:
		return mutedStyle.Render("State unknown")
	case st.Restarting:
		return label(glyphPartial, statusAccentStyle, "Restarting", "")
	case st.Paused:
		return label(glyphPartial, statusAccentStyle, "Paused", "")
	case st.Running:
		if d := since(st.StartedAt); d != "" {
			return label(glyphRunning, runningStyle, "Running", "for "+d)
		}
		return label(glyphRunning, runningStyle, "Running", "")
	case st.Status == containerTypes.StateCreated:
		return label(glyphStopped, mutedStyle, "Not started yet", "")
	default:
		detail := fmt.Sprintf("with code %d", st.ExitCode)
		if st.OOMKilled {
			detail = "out of memory"
		}
		if d := since(st.FinishedAt); d != "" {
			detail += ", " + d + " ago"
		}
		title := "Exited"
		if st.Status == containerTypes.StateDead {
			title = "Dead"
		}
		return label(glyphStopped, mutedStyle, title, detail)
	}
}

// GetContainerLogs returns the logs since `since` (a duration like "1h" or
// an RFC 3339 timestamp). Errors are returned as the log text so they show
// up in the viewport instead of killing the program.
func GetContainerLogs(dockerClient client.SDKClient, containerID string, tty bool, since string) string {
	ctx := context.Background()
	logs, err := dockerClient.ContainerLogs(ctx, containerID, containerTypes.LogsOptions{ShowStdout: true, ShowStderr: true, Since: since, Timestamps: true})
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
