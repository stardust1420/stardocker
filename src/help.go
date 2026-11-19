package src

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

// newHelp returns a help.Model styled like the rest of the UI: bold keys,
// grey descriptions, plain spacing instead of bullets.
func newHelp() help.Model {
	h := help.New()
	h.ShortSeparator = "   "
	h.FullSeparator = "     "
	h.Styles.ShortKey = crumbStyle
	h.Styles.FullKey = crumbStyle
	h.Styles.ShortDesc = mutedStyle
	h.Styles.FullDesc = mutedStyle
	h.Styles.ShortSeparator = mutedStyle
	h.Styles.FullSeparator = mutedStyle
	h.Styles.Ellipsis = mutedStyle
	return h
}

// keyMap defines a set of keybindings. To work for help it must satisfy
// key.Map. It could also very easily be a map[string]key.Binding.
type keyMap struct {
	Up    key.Binding
	Down  key.Binding
	Left  key.Binding
	Right key.Binding
	Help  key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "move down"),
	),
	Left: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Right: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "forward"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "toggle help"),
	),
}

// containersKeyMap adds the container actions to the shared bindings. It is
// only used by the containers screen, so other screens don't advertise keys
// they don't handle.
type containersKeyMap struct {
	keyMap
	Open   key.Binding
	Toggle key.Binding
	Quit   key.Binding
}

var containersKeys = containersKeyMap{
	keyMap: keys,
	Open: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "logs / expand stack"),
	),
	Toggle: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "start/stop container"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

func (k containersKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Toggle, k.Open, k.Help}
}

func (k containersKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Open, k.Left},
		{k.Toggle, k.Quit, k.Help},
	}
}

// ShortHelp returns keybindings to be shown in the mini help view. It's part
// of the key.Map interface.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Left, k.Help}
}

// FullHelp returns keybindings for the expanded help view. It's part of the
// key.Map interface.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down},
		{k.Left, k.Help},
	}
}
