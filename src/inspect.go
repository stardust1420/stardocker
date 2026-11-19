package src

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	containerTypes "github.com/docker/docker/api/types/container"
	"github.com/docker/go-sdk/client"
	"github.com/muesli/reflow/wordwrap"
)

// inspectModel shows a container's configuration: what the details panel
// has no room for (environment, mounts, networks, labels, restart policy).
// It is read once when opened; "R" reads it again.
type inspectModel struct {
	dockerClient  client.SDKClient
	containerID   string
	containerName string
	body          string // rendered sections, before wrapping
	back          *listContainersModel
	viewport      viewport.Model
	ready         bool
	width, height int
}

func InitInspectModel(dockerClient client.SDKClient, containerID, containerName string, width, height int) inspectModel {
	m := inspectModel{dockerClient: dockerClient, containerID: containerID, containerName: containerName}
	m.load()
	if width > 0 && height > 0 {
		m.setSize(width, height)
	}
	return m
}

func (m inspectModel) Init() tea.Cmd {
	return tea.SetWindowTitle("Inspect: " + m.containerName)
}

func (m *inspectModel) load() {
	if m.dockerClient == nil {
		m.body = "No Docker client."
		return
	}
	info, err := m.dockerClient.ContainerInspect(context.Background(), m.containerID)
	if err != nil {
		m.body = dangerTextStyle.Render(fmt.Sprintf("Can't inspect %s: %v", m.containerName, err))
		return
	}
	m.body = renderInspect(info)
}

func (m *inspectModel) setSize(width, height int) {
	m.width, m.height = width, height
	vpHeight := max(1, height-lipgloss.Height(m.headerView())-lipgloss.Height(m.footerView()))
	if !m.ready {
		m.viewport = viewport.New(width, vpHeight)
		m.viewport.Style = lipgloss.NewStyle().PaddingLeft(textIndent)
		m.ready = true
	}
	m.viewport.Width = width
	m.viewport.Height = vpHeight
	m.viewport.SetContent(wordwrap.String(m.body, max(1, width-textIndent)))
}

func (m inspectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.setSize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "esc":
			if m.back != nil {
				b := m.back.resume(m.width, m.height)
				return b, b.Init()
			}
			b := InitListContainersModel(m.dockerClient, m.width, m.height)
			return b, b.Init()
		case "R":
			m.load()
			m.setSize(m.width, m.height)
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m inspectModel) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}
	return m.headerView() + "\n" + m.viewport.View() + "\n" + m.footerView()
}

func (m inspectModel) headerView() string {
	w := m.width
	header := renderHeader(w, "", "Containers", m.containerName, "Inspect")
	return header + "\n" + borderStyle.Render(strings.Repeat("─", max(0, w)))
}

func (m inspectModel) footerView() string {
	hint := func(k, desc string) string { return crumbStyle.Render(k) + " " + mutedStyle.Render(desc) }
	keys := strings.Repeat(" ", textIndent) + strings.Join([]string{
		hint("↑/↓", "scroll"), hint("R", "reload"), hint("esc", "back"), hint("q", "quit"),
	}, "   ")
	pct := ""
	if m.ready {
		pct = crumbStyle.Render(fmt.Sprintf("%3.f%%", m.viewport.ScrollPercent()*100)) + strings.Repeat(" ", textIndent)
	}
	gap := max(2, m.width-lipgloss.Width(keys)-lipgloss.Width(pct))
	return borderStyle.Render(strings.Repeat("─", max(0, m.width))) + "\n" + keys + strings.Repeat(" ", gap) + pct
}

// renderInspect lays the inspect result out as titled sections of
// "label  value" lines.
func renderInspect(info containerTypes.InspectResponse) string {
	var b strings.Builder
	section := func(title string, lines [][2]string) {
		b.WriteString(brandStyle.Render(title) + "\n")
		if len(lines) == 0 {
			b.WriteString("  " + mutedStyle.Render("None") + "\n\n")
			return
		}
		w := 0
		for _, l := range lines {
			w = max(w, lipgloss.Width(l[0]))
		}
		for _, l := range lines {
			label := l[0] + strings.Repeat(" ", w-lipgloss.Width(l[0]))
			b.WriteString("  " + mutedStyle.Render(label) + "   " + l[1] + "\n")
		}
		b.WriteString("\n")
	}
	list := func(items []string) [][2]string {
		out := make([][2]string, 0, len(items))
		for _, it := range items {
			k, v, _ := strings.Cut(it, "=")
			out = append(out, [2]string{k, v})
		}
		return out
	}

	var general [][2]string
	general = append(general, [2]string{"ID", info.ID})
	if info.Config != nil {
		general = append(general, [2]string{"Image", info.Config.Image})
	}
	general = append(general, [2]string{"Image ID", info.Image})
	if t, err := time.Parse(time.RFC3339Nano, info.Created); err == nil {
		general = append(general, [2]string{"Created", t.Local().Format("2 Jan 2006 15:04:05")})
	}
	if info.State != nil {
		general = append(general, [2]string{"State", info.State.Status})
		if info.State.Health != nil {
			general = append(general, [2]string{"Health", info.State.Health.Status})
		}
		if info.State.Pid > 0 {
			general = append(general, [2]string{"PID", fmt.Sprint(info.State.Pid)})
		}
	}
	if info.Config != nil {
		if len(info.Config.Entrypoint) > 0 {
			general = append(general, [2]string{"Entrypoint", strings.Join(info.Config.Entrypoint, " ")})
		}
		if len(info.Config.Cmd) > 0 {
			general = append(general, [2]string{"Command", strings.Join(info.Config.Cmd, " ")})
		}
		if info.Config.WorkingDir != "" {
			general = append(general, [2]string{"Working dir", info.Config.WorkingDir})
		}
		if info.Config.User != "" {
			general = append(general, [2]string{"User", info.Config.User})
		}
	}
	if info.HostConfig != nil {
		rp := string(info.HostConfig.RestartPolicy.Name)
		if rp == "" {
			rp = "no"
		}
		if info.HostConfig.RestartPolicy.MaximumRetryCount > 0 {
			rp += fmt.Sprintf(" (max %d retries)", info.HostConfig.RestartPolicy.MaximumRetryCount)
		}
		general = append(general, [2]string{"Restart policy", rp})
		general = append(general, [2]string{"Restart count", fmt.Sprint(info.RestartCount)})
		if info.HostConfig.Memory > 0 {
			general = append(general, [2]string{"Memory limit", humanBinarySize(uint64(info.HostConfig.Memory))})
		}
		if info.HostConfig.NanoCPUs > 0 {
			general = append(general, [2]string{"CPU limit", fmt.Sprintf("%.2f CPUs", float64(info.HostConfig.NanoCPUs)/1e9)})
		}
	}
	section("General", general)

	var ports [][2]string
	if info.NetworkSettings != nil {
		for p, bindings := range info.NetworkSettings.Ports {
			var hosts []string
			for _, b := range bindings {
				hosts = append(hosts, b.HostIP+":"+b.HostPort)
			}
			v := "not published"
			if len(hosts) > 0 {
				v = "→ " + strings.Join(hosts, ", ")
			}
			ports = append(ports, [2]string{string(p), v})
		}
		sort.Slice(ports, func(i, j int) bool { return ports[i][0] < ports[j][0] })
	}
	section("Ports", ports)

	var nets [][2]string
	if info.NetworkSettings != nil {
		names := make([]string, 0, len(info.NetworkSettings.Networks))
		for n := range info.NetworkSettings.Networks {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			ep := info.NetworkSettings.Networks[n]
			v := ep.IPAddress
			if v == "" {
				v = "no address"
			}
			if len(ep.Aliases) > 0 {
				v += "   aliases " + strings.Join(ep.Aliases, ", ")
			}
			nets = append(nets, [2]string{n, v})
		}
	}
	section("Networks", nets)

	var mounts [][2]string
	for _, mt := range info.Mounts {
		src := mt.Source
		if mt.Name != "" {
			src = mt.Name
		}
		mode := "rw"
		if !mt.RW {
			mode = "ro"
		}
		mounts = append(mounts, [2]string{mt.Destination, fmt.Sprintf("%s %s (%s)", mt.Type, src, mode)})
	}
	section("Mounts", mounts)

	if info.Config != nil {
		env := append([]string(nil), info.Config.Env...)
		sort.Strings(env)
		section("Environment", list(env))

		labels := make([]string, 0, len(info.Config.Labels))
		for k, v := range info.Config.Labels {
			labels = append(labels, k+"="+v)
		}
		sort.Strings(labels)
		section("Labels", list(labels))
	}

	return strings.TrimRight(b.String(), "\n")
}
