package src

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// textField is a minimal one-line input: typed runes are appended if accept
// allows them, backspace deletes, ctrl+u empties it. It stands in for bubbles
// textinput, which would pull in a clipboard dependency.
type textField struct {
	value  string
	limit  int                             // max runes; 0 means no limit
	accept func(r rune, value string) bool // nil accepts any printable rune
}

// update applies a key and reports whether the field used it.
func (f *textField) update(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyBackspace:
		if f.value != "" {
			r := []rune(f.value)
			f.value = string(r[:len(r)-1])
		}
		return true
	case tea.KeyCtrlU:
		f.value = ""
		return true
	case tea.KeySpace:
		f.add(' ')
		return true
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			f.add(r)
		}
		return true
	}
	return false
}

func (f *textField) add(r rune) {
	if f.limit > 0 && len([]rune(f.value)) >= f.limit {
		return
	}
	if f.accept != nil && !f.accept(r, f.value) {
		return
	}
	if r < ' ' {
		return
	}
	f.value += string(r)
}

// view draws the value with a block cursor after it.
func (f textField) view() string {
	return crumbStyle.Render(f.value) + lipgloss.NewStyle().Background(colorAccent).Render(" ")
}

// numberField accepts digits and one decimal point.
type numberField = textField

func newRangeInput() numberField {
	return textField{limit: 7, accept: func(r rune, v string) bool {
		return (r >= '0' && r <= '9') || (r == '.' && !strings.Contains(v, "."))
	}}
}

// filterBar is the "/" filter of a list screen or the logs view. While
// editing, it takes every key; the query applies as it's typed. Enter keeps
// it and returns to the list, esc clears it.
type filterBar struct {
	field   textField
	editing bool
}

func newFilterBar() filterBar { return filterBar{field: textField{limit: 64}} }

// query is the active filter, lower-cased and trimmed ("" means none).
func (f filterBar) query() string {
	return strings.ToLower(strings.TrimSpace(f.field.value))
}

func (f *filterBar) open() { f.editing = true }

// clear removes the filter and closes the field.
func (f *filterBar) clear() {
	f.field.value = ""
	f.editing = false
}

// handle processes a key while editing. It reports whether the query
// changed, so the caller can refilter.
func (f *filterBar) handle(msg tea.KeyMsg) (changed bool) {
	before := f.field.value
	switch msg.String() {
	case "enter":
		f.editing = false
		return false
	case "esc":
		f.clear()
		return before != ""
	}
	f.field.update(msg)
	return f.field.value != before
}

// prompt is the line shown in place of the help while editing.
func (f filterBar) prompt() string {
	hint := func(k, desc string) string { return crumbStyle.Render(k) + " " + mutedStyle.Render(desc) }
	return statusAccentStyle.Render("/") + " " + f.field.view() + "   " + hint("enter", "keep") + "   " + hint("esc", "clear")
}

// crumb is the header breadcrumb for an active filter, or "".
func (f filterBar) crumb() string {
	if q := strings.TrimSpace(f.field.value); q != "" {
		return "/" + q
	}
	return ""
}

// matches reports whether any of the fields contains the query
// (case-insensitive). An empty query matches everything.
func matches(query string, fields ...string) bool {
	if query == "" {
		return true
	}
	for _, s := range fields {
		if strings.Contains(strings.ToLower(s), query) {
			return true
		}
	}
	return false
}

// crumbs appends the filter crumb, if any, to the screen's breadcrumb.
func withFilterCrumb(f filterBar, crumbs ...string) []string {
	if c := f.crumb(); c != "" {
		return append(crumbs, c)
	}
	return crumbs
}
