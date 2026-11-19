package src

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// confirmPrompt is a pending destructive action. While one is set, the
// details panel turns into a red confirmation box and only y / n / esc are
// handled. The action is captured when the prompt opens, so the refresh
// moving the cursor can't change what gets deleted.
type confirmPrompt struct {
	title    string   // the question, e.g. "Delete stardocker-test?"
	lines    []string // what will happen
	progress string   // status shown while the action runs
	run      tea.Cmd
}

var confirmKeyStyle = lipgloss.NewStyle().Foreground(colorDanger).Bold(true)

func renderConfirmPanel(width int, p *confirmPrompt) string {
	textWidth := 0
	if width > 0 {
		textWidth = width - 4 // border + padding
	}
	textStyle := lipgloss.NewStyle().Foreground(colorText).Width(textWidth)

	body := make([]string, 0, len(p.lines)+1)
	for _, line := range p.lines {
		body = append(body, textStyle.Render(line))
	}
	keys := confirmKeyStyle.Render("y") + mutedStyle.Render(" delete   ") +
		crumbStyle.Render("n") + mutedStyle.Render(" cancel")

	// Keep the keys on the last line of the panel whatever the text length.
	text := strings.Split(strings.Join(body, "\n"), "\n")
	if len(text) > detailsLines-1 {
		text = text[:detailsLines-1]
	}
	for len(text) < detailsLines-1 {
		text = append(text, "")
	}
	text = append(text, keys)

	return renderPanelColored(width, p.title, strings.Join(text, "\n"), detailsLines,
		colorDanger, lipgloss.NewStyle().Foreground(colorDanger).Bold(true))
}

// handleConfirmKey resolves a key press while a prompt is open. It returns
// the command to run (nil if none) and whether the prompt is finished.
func handleConfirmKey(p *confirmPrompt, msg tea.KeyMsg) (cmd tea.Cmd, done bool) {
	switch msg.String() {
	case "y", "Y":
		return p.run, true
	case "n", "N", "esc":
		return nil, true
	}
	return nil, false
}

// Status line kinds.
const (
	statusProgress = iota
	statusSuccess
	statusError
)

// statusLine is the message at the right end of the bottom bar. Success
// messages clear themselves after a few seconds; seq makes sure an older
// timer doesn't clear a newer message.
type statusLine struct {
	text string
	kind int
	seq  int
}

type clearStatusMsg struct {
	screen int // tickID of the screen that set it
	seq    int
}

const statusClearDelay = 4 * time.Second

func (s statusLine) render() string {
	switch {
	case s.text == "":
		return ""
	case s.kind == statusError:
		return lipgloss.NewStyle().Foreground(colorDanger).Render(s.text)
	case s.kind == statusSuccess:
		return runningStyle.Render("✓ " + s.text)
	default:
		return lipgloss.NewStyle().Foreground(colorAccent).Render(s.text)
	}
}

// set replaces the message and, for a success, returns a command that
// clears it later.
func (s *statusLine) set(screen int, kind int, text string) tea.Cmd {
	s.seq++
	s.text, s.kind = text, kind
	if kind != statusSuccess || text == "" {
		return nil
	}
	seq := s.seq
	return tea.Tick(statusClearDelay, func(time.Time) tea.Msg {
		return clearStatusMsg{screen: screen, seq: seq}
	})
}

func (s *statusLine) clear(msg clearStatusMsg, screen int) {
	if msg.screen == screen && msg.seq == s.seq {
		s.text = ""
	}
}
