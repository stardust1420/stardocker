package src

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// detailLine is one "Label  value" row of the details panel shown under a
// list for the selected item.
type detailLine struct {
	label, value string
}

var detailLabelStyle = lipgloss.NewStyle().Foreground(colorMuted)

// detailsBody lays the lines out as a two-column grid that fits inside a
// panel of the given outer width. Long values wrap under themselves, aligned
// after the label, and the result is capped at detailsLines lines (the last
// visible line ends in "…" if anything was cut).
func detailsBody(panelWidth int, lines []detailLine) string {
	if len(lines) == 0 {
		return mutedStyle.Render("Nothing selected")
	}

	labelWidth := 0
	for _, d := range lines {
		labelWidth = max(labelWidth, lipgloss.Width(d.label))
	}
	labelCol := detailLabelStyle.Width(labelWidth + 3)

	valueStyle := lipgloss.NewStyle().Foreground(colorText)
	if panelWidth > 0 {
		// panel border (2) + padding (2) + label column
		valueStyle = valueStyle.Width(max(10, panelWidth-4-(labelWidth+3)))
	}

	rendered := make([]string, len(lines))
	for i, d := range lines {
		rendered[i] = lipgloss.JoinHorizontal(lipgloss.Top, labelCol.Render(d.label), valueStyle.Render(d.value))
	}

	all := strings.Split(strings.Join(rendered, "\n"), "\n")
	if len(all) > detailsLines {
		all = all[:detailsLines]
		last := strings.TrimRight(all[detailsLines-1], " ")
		all[detailsLines-1] = last + "…"
	}
	return strings.Join(all, "\n")
}
