package src

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

// colSpec describes one table column for fitColumns.
type colSpec struct {
	title string
	min   int // never shrink below this
	max   int // natural width is capped here (growing to fill may exceed it)
	// shrinkOrder: columns with a lower value give up width first when the
	// table is too narrow. Columns with equal order shrink left to right.
	shrinkOrder int
	// grow: share of any spare width this column receives when the table
	// is wider than its content. 0 means the column never grows.
	grow int
}

// Each cell has 1 column of padding on both sides (table.DefaultStyles), and
// the table box has a 1-column border on both sides.
const (
	cellPadding   = 2
	tableOverhead = 2
)

// fitColumns sizes each column to its widest cell (bounded by min and max),
// then makes the table exactly tableWidth wide (border included): spare width
// goes to the columns with grow > 0, and if the content is too wide, columns
// shrink in shrinkOrder until it fits or every column is at its minimum.
// Cells that are still too long are truncated with "…" by the table.
// tableWidth 0 means unknown, in which case the natural widths are used.
func fitColumns(specs []colSpec, rows []table.Row, tableWidth int) []table.Column {
	widths := make([]int, len(specs))
	for i, s := range specs {
		w := lipgloss.Width(s.title)
		for _, r := range rows {
			if i < len(r) {
				w = max(w, lipgloss.Width(r[i]))
			}
		}
		widths[i] = min(max(w, s.min), s.max)
	}

	if tableWidth > 0 {
		total := tableOverhead
		for _, w := range widths {
			total += w + cellPadding
		}

		if spare := tableWidth - total; spare > 0 {
			growColumns(specs, widths, spare)
		} else if over := -spare; over > 0 {
			shrinkColumns(specs, widths, over)
		}
	}

	cols := make([]table.Column, len(specs))
	for i, s := range specs {
		cols[i] = table.Column{Title: s.title, Width: widths[i]}
	}
	return cols
}

// growColumns hands out spare width in proportion to each column's grow
// weight; the rounding remainder goes to the column with the largest weight.
func growColumns(specs []colSpec, widths []int, spare int) {
	totalWeight, heaviest := 0, -1
	for i, s := range specs {
		totalWeight += s.grow
		if s.grow > 0 && (heaviest < 0 || s.grow > specs[heaviest].grow) {
			heaviest = i
		}
	}
	if totalWeight == 0 {
		return
	}
	given := 0
	for i, s := range specs {
		add := spare * s.grow / totalWeight
		widths[i] += add
		given += add
	}
	widths[heaviest] += spare - given
}

func shrinkColumns(specs []colSpec, widths []int, over int) {
	maxOrder := 0
	for _, s := range specs {
		maxOrder = max(maxOrder, s.shrinkOrder)
	}
	for order := 0; over > 0 && order <= maxOrder; order++ {
		for i, s := range specs {
			if over <= 0 {
				break
			}
			if s.shrinkOrder != order {
				continue
			}
			cut := min(over, widths[i]-s.min)
			if cut > 0 {
				widths[i] -= cut
				over -= cut
			}
		}
	}
}
