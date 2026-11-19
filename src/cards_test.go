package src

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func card(key string) gridEntry { return gridEntry{key: key, selectable: true, title: key} }

func keyNamed(s string) tea.KeyMsg {
	switch s {
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestGridNavigation(t *testing.T) {
	// 80 wide fits 2 cards per row. Layout:
	//   heading s (selectable)
	//   a b
	//   c
	//   heading "Standalone" (not selectable)
	//   d e
	g := gridView{cursor: -1, width: 80, height: 100}
	g.setEntries([]gridEntry{
		{key: "s", heading: true, selectable: true, title: "s"},
		card("a"), card("b"), card("c"),
		{key: "h", heading: true, title: "Standalone"},
		card("d"), card("e"),
	})
	if g.cols() != 2 {
		t.Fatalf("cols = %d; want 2", g.cols())
	}
	at := func() string { return g.entries[g.cursor].key }
	steps := []struct{ key, want string }{
		{"down", "a"}, {"right", "b"}, {"down", "c"}, // b's column is past c: last card of the row
		{"down", "d"},                  // skips the plain heading
		{"right", "e"}, {"right", "e"}, // stops at the end
		{"up", "c"}, {"up", "a"}, {"up", "s"},
		{"G", "e"}, {"g", "s"},
		{"left", "s"},
	}
	for _, s := range steps {
		g.handleKey(keyNamed(s.key))
		if at() != s.want {
			t.Fatalf("after %s: at %s; want %s", s.key, at(), s.want)
		}
	}
}

func TestGridKeepsSelectionByKey(t *testing.T) {
	g := gridView{cursor: -1, width: 80, height: 100}
	g.setEntries([]gridEntry{card("a"), card("b"), card("c")})
	g.handleKey(keyNamed("right"))
	g.handleKey(keyNamed("right")) // on c
	g.setEntries([]gridEntry{card("x"), card("c"), card("a")})
	if g.entries[g.cursor].key != "c" {
		t.Errorf("cursor on %s; want c", g.entries[g.cursor].key)
	}
	g.setEntries([]gridEntry{card("x")}) // c gone: nearest that exists
	if g.cursor != 0 {
		t.Errorf("cursor = %d; want 0", g.cursor)
	}
	g.setEntries(nil)
	if g.cursor != -1 {
		t.Errorf("empty grid cursor = %d; want -1", g.cursor)
	}
}

func TestGridScrollsToSelection(t *testing.T) {
	// One card per row, 6 lines each plus a blank between: 10 rows don't
	// fit in 20 lines.
	g := gridView{cursor: -1, width: 40, height: 20}
	var entries []gridEntry
	for _, k := range strings.Split("abcdefghij", "") {
		entries = append(entries, card(k))
	}
	g.setEntries(entries)
	g.handleKey(keyNamed("G"))
	view := g.view("")
	if lines := strings.Split(view, "\n"); len(lines) != 20 {
		t.Fatalf("view is %d lines; want 20", len(lines))
	}
	if !strings.Contains(view, "j") || strings.Contains(view, " a ") {
		t.Errorf("last card not scrolled into view:\n%s", view)
	}
}

func TestGridRowHeightFollowsTallestCard(t *testing.T) {
	short := gridEntry{key: "a", selectable: true, body: []string{"img", "status"}}
	tall := gridEntry{key: "b", selectable: true, body: []string{"1", "2", "3", "4", "5"}}
	g := gridView{cursor: -1, width: 80}
	g.setEntries([]gridEntry{short, short, tall})
	rows := g.rows() // 2 per row: [short short] [tall]
	if rows[0].height() != gridCardLines-2 || rows[1].height() != gridCardLines {
		t.Errorf("row heights = %d, %d; want %d, %d", rows[0].height(), rows[1].height(), gridCardLines-2, gridCardLines)
	}
	if got := len(strings.Split(g.view(""), "\n")); got != rows[0].height()+1+rows[1].height() {
		t.Errorf("view is %d lines", got)
	}
}

func TestShortStatus(t *testing.T) {
	cases := map[string]string{
		"Exited (137) About a minute ago": "Exited (137) 1 minute ago",
		"Up About an hour":                "Up 1 hour",
		"Up Less than a second":           "Up 1 second",
		"Up 3 days":                       "Up 3 days",
	}
	for in, want := range cases {
		if got := shortStatus(in); got != want {
			t.Errorf("shortStatus(%q) = %q; want %q", in, got, want)
		}
	}
}
