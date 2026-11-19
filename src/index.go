package src

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	containerTypes "github.com/docker/docker/api/types/container"
	imageTypes "github.com/docker/docker/api/types/image"
	"github.com/docker/go-sdk/client"
)

// index Model: the home screen, a row of menu cards.

type menuCard struct {
	key         string // shortcut digit
	title       string
	description string
}

var menuCards = []menuCard{
	{key: "1", title: "Containers", description: "Start, stop, read logs"},
	{key: "2", title: "Images", description: "See what's on disk"},
	{key: "3", title: "Exit", description: "Close StarDocker"},
}

const (
	cardContainers = iota
	cardImages
	cardExit
)

// dockerSummary is the live data shown on the cards.
type dockerSummary struct {
	running, containers int
	images              int
	imagesSize          int64
	err                 error
}

type summaryMsg struct {
	tickID  int
	summary dockerSummary
}

type indexKeyMap struct {
	Left, Right, Select, Jump, Quit, Help key.Binding
}

var indexKeys = indexKeyMap{
	Left:   key.NewBinding(key.WithKeys("left", "h", "shift+tab", "up", "k"), key.WithHelp("←/h", "previous")),
	Right:  key.NewBinding(key.WithKeys("right", "l", "tab", "down", "j"), key.WithHelp("→/l", "next")),
	Select: key.NewBinding(key.WithKeys("enter", " "), key.WithHelp("enter", "open")),
	Jump:   key.NewBinding(key.WithKeys("1", "2", "3"), key.WithHelp("1-3", "open card")),
	Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "toggle help")),
}

func (k indexKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Left, k.Right, k.Select, k.Help}
}

func (k indexKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Left, k.Right, k.Select},
		{k.Jump, k.Quit, k.Help},
	}
}

type indexModel struct {
	help         help.Model
	keys         indexKeyMap
	dockerClient client.SDKClient
	width        int
	height       int
	selected     int
	tickID       int
	summary      *dockerSummary // nil until the first fetch returns
}

func InitIndexModel(dockerClient client.SDKClient, width int, height int) indexModel {
	return indexModel{
		help:         newHelp(),
		keys:         indexKeys,
		dockerClient: dockerClient,
		width:        width,
		height:       height,
		tickID:       newTickID(),
	}
}

func (m indexModel) Init() tea.Cmd {
	return tea.Batch(tea.SetWindowTitle("StarDocker"), fetchSummaryCmd(m.dockerClient, m.tickID))
}

// fetchSummaryCmd loads the card counts off the UI goroutine.
func fetchSummaryCmd(dockerClient client.SDKClient, tickID int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.TODO()
		var s dockerSummary

		containers, err := dockerClient.ContainerList(ctx, containerTypes.ListOptions{All: true})
		if err != nil {
			s.err = err
			return summaryMsg{tickID: tickID, summary: s}
		}
		s.containers = len(containers)
		for _, c := range containers {
			if c.State == containerTypes.StateRunning {
				s.running++
			}
		}

		images, err := dockerClient.ImageList(ctx, imageTypes.ListOptions{})
		if err != nil {
			s.err = err
			return summaryMsg{tickID: tickID, summary: s}
		}
		s.images = len(images)
		for _, img := range images {
			s.imagesSize += img.Size
		}

		return summaryMsg{tickID: tickID, summary: s}
	}
}

func (m indexModel) open(card int) (tea.Model, tea.Cmd) {
	switch card {
	case cardContainers:
		l := InitListContainersModel(m.dockerClient, m.width, m.height)
		return l, l.Init()
	case cardImages:
		im := InitListImagesModel(m.dockerClient, m.width, m.height)
		return im, im.Init()
	case cardExit:
		return m, tea.Quit
	}
	return m, nil
}

func (m indexModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case summaryMsg:
		if msg.tickID != m.tickID {
			return m, nil
		}
		m.summary = &msg.summary
		return m, tickCmd(m.tickID)

	case tickMsg:
		if msg.id != m.tickID {
			return m, nil
		}
		return m, fetchSummaryCmd(m.dockerClient, m.tickID)

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Left):
			m.selected = (m.selected - 1 + len(menuCards)) % len(menuCards)
		case key.Matches(msg, m.keys.Right):
			m.selected = (m.selected + 1) % len(menuCards)
		case key.Matches(msg, m.keys.Select):
			return m.open(m.selected)
		case key.Matches(msg, m.keys.Jump):
			for i, c := range menuCards {
				if c.key == msg.String() {
					m.selected = i
					return m.open(i)
				}
			}
		}
	}

	return m, nil
}

// Home screen layout: the orange banner is exactly as wide as the row of
// cards, and the whole block is centred in the window.
const (
	cardWidth  = 26 // lipgloss Width: includes padding, excludes border
	cardHeight = 6  // same height for every card even if a line wraps
	cardGap    = 3
	cardText   = cardWidth - 4 // minus horizontal padding
)

var bannerArt = []string{
	"███████╗████████╗ █████╗ ██████╗ ██████╗  ██████╗  ██████╗██╗  ██╗███████╗██████╗ ",
	"██╔════╝╚══██╔══╝██╔══██╗██╔══██╗██╔══██╗██╔═══██╗██╔════╝██║ ██╔╝██╔════╝██╔══██╗",
	"███████╗   ██║   ███████║██████╔╝██║  ██║██║   ██║██║     █████╔╝ █████╗  ██████╔╝",
	"╚════██║   ██║   ██╔══██║██╔══██╗██║  ██║██║   ██║██║     ██╔═██╗ ██╔══╝  ██╔══██╗",
	"███████║   ██║   ██║  ██║██║  ██║██████╔╝╚██████╔╝╚██████╗██║  ██╗███████╗██║  ██║",
	"╚══════╝   ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝  ╚═════╝  ╚═════╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝",
}

var (
	bannerStyle = lipgloss.NewStyle().
			Background(colorAccent).
			Foreground(colorInk).
			Padding(1, 0).
			Align(lipgloss.Center)

	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2).
			Width(cardWidth).
			Height(cardHeight)

	selectedCardStyle     = cardStyle.Border(lipgloss.ThickBorder()).BorderForeground(colorAccent)
	selectedExitCardStyle = selectedCardStyle.BorderForeground(colorDanger)

	cardTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorText)
	cardDescStyle   = lipgloss.NewStyle().Foreground(colorMuted)
	dangerTextStyle = lipgloss.NewStyle().Foreground(colorDanger)
)

// cardStat is the live line at the bottom of each card.
func (m indexModel) cardStat(card int) string {
	if card == cardExit {
		return ""
	}
	if m.summary == nil {
		return mutedStyle.Render("Checking Docker…")
	}
	if m.summary.err != nil {
		return dangerTextStyle.Render("Docker not reachable")
	}
	switch card {
	case cardContainers:
		return runningStyle.Render(glyphRunning) + " " +
			crumbStyle.Render(fmt.Sprintf("%d", m.summary.running)) +
			mutedStyle.Render(fmt.Sprintf(" of %d running", m.summary.containers))
	case cardImages:
		noun := "images"
		if m.summary.images == 1 {
			noun = "image"
		}
		return crumbStyle.Render(humanSize(m.summary.imagesSize)) +
			mutedStyle.Render(fmt.Sprintf(" in %d %s", m.summary.images, noun))
	}
	return ""
}

func (m indexModel) renderCard(i int) string {
	c := menuCards[i]
	style, titleStyle := cardStyle, cardTitleStyle
	if i == m.selected {
		style, titleStyle = selectedCardStyle, cardTitleStyle.Foreground(colorAccent)
		if i == cardExit {
			style, titleStyle = selectedExitCardStyle, cardTitleStyle.Foreground(colorDanger)
		}
	}

	// Title on the left, its shortcut key on the right.
	title := titleStyle.Render(c.title)
	keyHint := mutedStyle.Render(c.key)
	gap := max(1, cardText-lipgloss.Width(title)-lipgloss.Width(keyHint))

	body := strings.Join([]string{
		title + strings.Repeat(" ", gap) + keyHint,
		"",
		cardDescStyle.Render(c.description),
		m.cardStat(i),
	}, "\n")

	return style.Render(body)
}

// daemonLine says which Docker daemon StarDocker is talking to.
func (m indexModel) daemonLine() string {
	host := ""
	if m.dockerClient != nil {
		host = m.dockerClient.DaemonHost()
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			host = strings.Replace(host, home, "~", 1)
		}
	}
	if m.summary != nil && m.summary.err != nil {
		return dangerTextStyle.Render("Can't reach Docker at " + host + ". Is Colima running?")
	}
	if host == "" {
		return ""
	}
	return mutedStyle.Render("Connected to " + host)
}

func (m indexModel) View() string {
	cards := make([]string, len(menuCards))
	for i := range menuCards {
		cards[i] = m.renderCard(i)
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, intersperse(cards, strings.Repeat(" ", cardGap))...)
	rowWidth := lipgloss.Width(row)
	banner := bannerStyle.Width(rowWidth).Render(strings.Join(bannerArt, "\n"))

	// Too narrow for the big banner and three cards side by side: stack the
	// cards and use a compact banner of the same width.
	if m.width > 0 && rowWidth+2 > m.width {
		row = lipgloss.JoinVertical(lipgloss.Left, cards...)
		rowWidth = lipgloss.Width(row)
		banner = bannerStyle.Width(rowWidth).Bold(true).Render("★ StarDocker")
	}

	// JoinVertical with Center centres every block on the widest one.
	page := lipgloss.JoinVertical(lipgloss.Center,
		banner,
		"",
		row,
		"",
		m.daemonLine(),
		m.help.View(m.keys),
	)

	if m.width <= 0 || m.height <= 0 {
		return page
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, page)
}

func intersperse(items []string, sep string) []string {
	out := make([]string, 0, 2*len(items))
	for i, it := range items {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, it)
	}
	return out
}
