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
	Open, Toggle, Restart, Pause, Exec, Inspect key.Binding
	Delete, Prune, Filter, Sort, All, Quit      key.Binding
}

var (
	quitKey   = key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit"))
	filterKey = key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter"))
	deleteKey = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete"))
	pruneKey  = key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "prune"))
)

var containersKeys = containersKeyMap{
	keyMap:  keys,
	Open:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "logs / expand stack")),
	Toggle:  key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start/stop")),
	Restart: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "restart")),
	Pause:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pause/unpause")),
	Exec:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "shell")),
	Inspect: key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "inspect")),
	Delete:  deleteKey,
	Prune:   key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "prune stopped")),
	Filter:  filterKey,
	Sort:    key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "sort")),
	All:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "running only")),
	Quit:    quitKey,
}

func (k containersKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Open, k.Toggle, k.Exec, k.Filter, k.Left, k.Help}
}

func (k containersKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Open, k.Left},
		{k.Toggle, k.Restart, k.Pause, k.Delete},
		{k.Exec, k.Inspect, k.Prune},
		{k.Filter, k.Sort, k.All},
		{k.Quit, k.Help},
	}
}

// listKeyMap is the help of the images, volumes and networks screens: the
// shared bindings plus that screen's actions.
type listKeyMap struct {
	keyMap
	actions []key.Binding
}

func (k listKeyMap) ShortHelp() []key.Binding {
	short := append([]key.Binding{}, k.actions...)
	return append(short, filterKey, k.Left, k.Help)
}

func (k listKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left},
		k.actions,
		{filterKey, quitKey, k.Help},
	}
}

var (
	imagesKeys = listKeyMap{keyMap: keys, actions: []key.Binding{
		deleteKey,
		key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pull")),
		key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "prune dangling")),
		key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "sort")),
	}}
	volumesKeys = listKeyMap{keyMap: keys, actions: []key.Binding{
		deleteKey,
		key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "prune unused")),
	}}
	networksKeys = listKeyMap{keyMap: keys, actions: []key.Binding{
		deleteKey,
		key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "prune unused")),
	}}
)

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
