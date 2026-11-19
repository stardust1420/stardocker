package src

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Shared layout for the list screens (containers, images):
//
//	★ StarDocker › Containers                       ● 3 running  ○ 1 stopped
//
//	┌ table, full width, fills the remaining height ─────────────────────────┐
//	└────────────────────────────────────────────────────────────────────────┘
//	╭─ selected-item-name ───────────────────────────────────────────────────╮
//	│ Label     value                                                        │
//	╰────────────────────────────────────────────────────────────────────────╯
//	  help keys                                               status message
//
// Every block spans the window minus pageMargin on each side, and all text
// starts at the same column: pageMargin + textIndent (border + 1 padding).

const (
	pageMargin   = 1
	textIndent   = 2
	detailsLines = 4 // content lines inside the details panel
)

// Palette. Orange is the brand and focus colour; steel blue marks the
// selected row; green means running. Grey carries structure and secondary
// text so the colours stay meaningful.
var (
	colorAccent  = lipgloss.Color("#f9a318") // container orange
	colorSteel   = lipgloss.Color("#2f4f6f") // selected row
	colorRunning = lipgloss.Color("#5fd787") // signal green
	colorDanger  = lipgloss.Color("#ff5f5f")
	colorBorder  = lipgloss.Color("240")
	colorMuted   = lipgloss.Color("245")
	colorText    = lipgloss.Color("252")
	colorInk     = lipgloss.Color("#020202")

	pageStyle         = lipgloss.NewStyle().MarginLeft(pageMargin)
	brandStyle        = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	crumbStyle        = lipgloss.NewStyle().Foreground(colorText).Bold(true)
	mutedStyle        = lipgloss.NewStyle().Foreground(colorMuted)
	runningStyle      = lipgloss.NewStyle().Foreground(colorRunning)
	borderStyle       = lipgloss.NewStyle().Foreground(colorBorder)
	statusAccentStyle = lipgloss.NewStyle().Foreground(colorAccent)
)

// Row state glyphs: shape carries the meaning, colour only reinforces it.
const (
	glyphRunning = "●"
	glyphPartial = "◐"
	glyphStopped = "○"
)

// pageWidth is the width every block on a list screen spans.
func pageWidth(windowWidth int) int {
	if windowWidth <= 0 {
		return 0
	}
	return max(20, windowWidth-2*pageMargin)
}

// listTableHeight is the height to give the bubbles table (its header row
// included) so the screen fills windowHeight exactly.
func listTableHeight(windowHeight, helpLines int) int {
	const header, gap, tableBorder, panel = 1, 1, 2, detailsLines + 2
	return max(4, windowHeight-(header+gap+tableBorder+panel+helpLines))
}

// renderHeader draws "★ StarDocker › crumb › crumb" on the left and info on
// the right, spanning width.
func renderHeader(width int, info string, crumbs ...string) string {
	sep := mutedStyle.Render(" › ")
	left := brandStyle.Render("★ StarDocker")
	for i, c := range crumbs {
		style := mutedStyle
		if i == len(crumbs)-1 {
			style = crumbStyle
		}
		left += sep + style.Render(c)
	}
	left = strings.Repeat(" ", textIndent) + left

	gap := 3
	if width > 0 {
		gap = max(2, width-lipgloss.Width(left)-lipgloss.Width(info)-textIndent)
	}
	return left + strings.Repeat(" ", gap) + info
}

// stateCounts renders "● 3 running  ○ 1 stopped" for the header.
func stateCounts(running, stopped int) string {
	return runningStyle.Render(glyphRunning) + " " + crumbStyle.Render(strconv.Itoa(running)) + mutedStyle.Render(" running") +
		"   " + mutedStyle.Render(glyphStopped+" ") + crumbStyle.Render(strconv.Itoa(stopped)) + mutedStyle.Render(" stopped")
}

// renderPanel draws a rounded box spanning width with the title set into
// its top border, and a fixed number of content lines so the layout doesn't
// jump as the selection moves.
func renderPanel(width int, title, body string, lines int) string {
	return renderPanelColored(width, title, body, lines, colorBorder, crumbStyle)
}

// renderPanelColored is renderPanel with a custom border colour and title
// style (the delete confirmation uses red).
func renderPanelColored(width int, title, body string, lines int, border lipgloss.Color, titleStyle lipgloss.Style) string {
	edge := lipgloss.NewStyle().Foreground(border)
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderTop(false).
		BorderForeground(border).
		Padding(0, 1).
		Height(lines).
		MaxHeight(lines + 1) // + bottom border
	if width > 0 {
		style = style.Width(width - 2) // Width includes padding, not border
	}
	box := style.Render(body)

	boxWidth := lipgloss.Width(box)
	label := ""
	if title != "" {
		label = " " + truncate(title, max(0, boxWidth-6)) + " "
	}
	fill := max(0, boxWidth-3-lipgloss.Width(label))
	top := edge.Render("╭─") + titleStyle.Render(label) + edge.Render(strings.Repeat("─", fill)+"╮")
	return top + "\n" + box
}

// renderBottomBar puts the help keys on the left and an already styled
// status message (if any) right-aligned on the same line.
func renderBottomBar(width int, helpView, st string) string {
	help := lipgloss.NewStyle().PaddingLeft(textIndent).Render(helpView)
	if st == "" {
		return help
	}
	// Never run past the window: shorten the status to the room left.
	if width > 0 {
		if room := width - lipgloss.Width(help) - 2 - textIndent; lipgloss.Width(st) > room {
			st = ansiTruncate(st, max(1, room))
		}
	}
	gap := 3
	if width > 0 {
		gap = max(2, width-lipgloss.Width(help)-lipgloss.Width(st)-textIndent)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, help, strings.Repeat(" ", gap), st)
}

// renderListPage stacks the blocks of a list screen with consistent margins.
func renderListPage(header, table, panel, bottom string) string {
	return lipgloss.JoinVertical(lipgloss.Left,
		pageStyle.Render(header),
		"",
		pageStyle.Render(table),
		pageStyle.Render(panel),
		pageStyle.Render(bottom),
	)
}

// ansiTruncate shortens a styled string to w cells, keeping its styling.
func ansiTruncate(s string, w int) string {
	return ansi.Truncate(s, w, "…")
}

func truncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}
