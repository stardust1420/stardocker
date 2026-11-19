package src

import (
	"testing"

	"github.com/charmbracelet/bubbles/table"
	containerTypes "github.com/docker/docker/api/types/container"
)

func TestFormatPorts(t *testing.T) {
	ports := []containerTypes.Port{
		{IP: "::", PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
		{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
		{IP: "127.0.0.1", PrivatePort: 5432, PublicPort: 5433, Type: "tcp"},
		{PrivatePort: 9000, Type: "tcp"},
	}
	// Sorted by public port (unpublished first), then private port; the
	// IPv6 duplicate of 8080 is collapsed.
	want := "9000/tcp, 127.0.0.1:5433->5432/tcp, 8080->80/tcp"
	if got := formatPorts(ports); got != want {
		t.Errorf("formatPorts = %q; want %q", got, want)
	}
	if got := formatPorts(nil); got != "" {
		t.Errorf("formatPorts(nil) = %q; want empty", got)
	}
}

func TestComposeStackSummary(t *testing.T) {
	children := []Container{
		{Name: "db", State: containerTypes.StateRunning, Ports: []containerTypes.Port{{PrivatePort: 5432, PublicPort: 5432, Type: "tcp"}}},
		{Name: "web", State: containerTypes.StateExited},
	}
	s := composeStackSummary("shop", children)
	if s.Status != "1/2 running" || s.State != "partial" || s.Image != "2 services" {
		t.Errorf("summary = %q / %q / %q", s.Status, s.State, s.Image)
	}
	if got := formatPorts(s.Ports); got != "5432->5432/tcp" {
		t.Errorf("stack ports = %q", got)
	}
}

func TestFitColumns(t *testing.T) {
	specs := []colSpec{
		{title: "A", min: 2, max: 2},
		{title: "Name", min: 4, max: 30, shrinkOrder: 1, grow: 1},
		{title: "Ports", min: 5, max: 40, shrinkOrder: 2, grow: 3},
	}
	rows := []table.Row{{"x", "a-very-long-container-name", "8080->80/tcp, 9090->90/tcp"}}

	// Unknown width: natural widths.
	cols := fitColumns(specs, rows, 0)
	if cols[1].Width != 26 || cols[2].Width != 26 {
		t.Fatalf("natural widths = %d, %d", cols[1].Width, cols[2].Width)
	}

	// Natural total = 2 (border) + (2+2) + (26+2) + (26+2) = 62.

	// Narrow: Name (lower shrinkOrder) gives up width before Ports.
	cols = fitColumns(specs, rows, 52)
	if cols[1].Width != 16 || cols[2].Width != 26 {
		t.Errorf("fitted widths = %d, %d; want 16, 26", cols[1].Width, cols[2].Width)
	}

	// Very narrow: everything at its minimum.
	cols = fitColumns(specs, rows, 10)
	if cols[1].Width != 4 || cols[2].Width != 5 {
		t.Errorf("min widths = %d, %d; want 4, 5", cols[1].Width, cols[2].Width)
	}

	// Wide: 20 spare columns split 1:3 between Name and Ports, and the
	// table comes out exactly as wide as asked.
	cols = fitColumns(specs, rows, 82)
	if cols[0].Width != 2 || cols[1].Width != 31 || cols[2].Width != 41 {
		t.Errorf("grown widths = %d, %d, %d; want 2, 31, 41", cols[0].Width, cols[1].Width, cols[2].Width)
	}
	total := tableOverhead
	for _, c := range cols {
		total += c.Width + cellPadding
	}
	if total != 82 {
		t.Errorf("table width = %d; want 82", total)
	}
}

func TestGetRowsItemsAligned(t *testing.T) {
	stack := composeStackSummary("shop", []Container{
		{Name: "db", Type: TypeContainer, ID: "aaaaaaaaaaaaaaaa"},
		{Name: "web", Type: TypeContainer, ID: "bbbbbbbbbbbbbbbb"},
	})
	solo := Container{Name: "solo", Type: TypeContainer, ID: "cccccccccccccccc"}

	l := listContainersModel{ShowChildrenSet: make(StringSet)}
	rows, items := l.getRows([]Container{stack, solo})
	if len(rows) != 2 || len(items) != 2 || rows[0][1] != "▸ shop" {
		t.Fatalf("collapsed: %d rows, %d items, name %q", len(rows), len(items), rows[0][1])
	}

	l.ShowChildrenSet.Add("shop")
	rows, items = l.getRows([]Container{stack, solo})
	if len(rows) != 4 || len(items) != 4 {
		t.Fatalf("expanded: %d rows, %d items", len(rows), len(items))
	}
	if rows[0][1] != "▾ shop" || items[1].Name != "db" || items[2].Name != "web" || items[3].Name != "solo" {
		t.Errorf("unexpected order: %q, %v", rows[0][1], items)
	}
	if rows[1][2] != "aaaaaaaaaaaa" {
		t.Errorf("child ID cell = %q; want short ID without indent", rows[1][2])
	}
}
