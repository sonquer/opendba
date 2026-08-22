package app

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

type keymap struct {
	Run         key.Binding
	Palette     key.Binding
	Connections key.Binding
	Catalog     key.Binding
	Query       key.Binding
	Schema      key.Binding
	Indexes     key.Binding
	Health      key.Binding
	Reload      key.Binding
	Help        key.Binding
	Back        key.Binding
	Home        key.Binding
	Quit        key.Binding
	Leave       key.Binding
	Up          key.Binding
	Down        key.Binding
	Choose      key.Binding
	New         key.Binding
	Remove      key.Binding
	Focus       key.Binding
	Accept      key.Binding
	enhanced    bool
}

func newKeymap() keymap {
	return keymap{
		Run:         runBinding(false),
		Palette:     binding("commands", "ctrl+k"),
		Connections: binding("connections", "ctrl+p"),
		Catalog:     binding("databases", "ctrl+d"),
		Query:       binding("query", "e"),
		Schema:      binding("tables", "s"),
		Indexes:     binding("indexes", "i"),
		Health:      binding("health", "h"),
		Reload:      binding("reload", "r"),
		Help:        binding("help", "?"),
		Back:        binding("back", "esc"),
		Home:        binding("dashboard", "esc"),
		Quit:        binding("quit", "q", "ctrl+c"),
		Leave:       binding("quit", "ctrl+c"),
		Up:          binding("up", "up", "k"),
		Down:        binding("down", "down", "j"),
		Choose:      binding("open", "enter"),
		New:         binding("new", "n"),
		Remove:      binding("remove", "d"),
		Focus:       binding("focus", "tab"),
		Accept:      binding("accept", "tab"),
	}
}

func binding(help string, keys ...string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(ui.Keystroke(keys[0]), help))
}

// runBinding accepts every way a terminal can send "run this statement".
// Only terminals with keyboard enhancements can tell ctrl+enter apart from
// ctrl+j, so ctrl+r is what gets advertised until the terminal says otherwise.
func runBinding(enhanced bool) key.Binding {
	label := ui.Keystroke("ctrl+r")
	if enhanced {
		label = ui.Keystroke("ctrl+enter")
	}
	return key.NewBinding(
		key.WithKeys("ctrl+enter", "super+enter", "ctrl+r", "f5"),
		key.WithHelp(label, "run"),
	)
}

func (k keymap) withEnhancements(msg tea.KeyboardEnhancementsMsg) keymap {
	k.enhanced = msg.SupportsKeyDisambiguation()
	k.Run = runBinding(k.enhanced)
	return k
}

func (k keymap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Query, k.Schema, k.Health, k.Connections, k.Catalog, k.Palette, k.Help, k.Quit,
	}
}

func (k keymap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Query, k.Run, k.Focus},
		{k.Schema, k.Indexes, k.Health, k.Reload},
		{k.Palette, k.Catalog, k.Connections, k.New, k.Remove},
		{k.Up, k.Down, k.Choose, k.Home, k.Help, k.Quit},
	}
}

// screenKeys is the footer of one screen. Every entry comes from the keymap, so
// what the footer offers and what the keys do cannot drift apart.
type screenKeys []key.Binding

func (s screenKeys) ShortHelp() []key.Binding { return s }

func (s screenKeys) FullHelp() [][]key.Binding { return [][]key.Binding{s} }

// footer picks the keys worth naming on a screen. Everything else stays one
// ctrl+k away.
func (k keymap) footer(current view, completing bool) screenKeys {
	if completing {
		return screenKeys{k.Accept, k.Up, k.Down, k.Back}
	}
	switch current {
	case viewQuery:
		return screenKeys{k.Run, k.Focus, k.Palette, k.Connections, k.Home, k.Leave}
	case viewSwitch:
		return screenKeys{k.Up, k.Down, k.Choose, k.New, k.Remove, k.Home}
	case viewCatalog:
		return screenKeys{k.Up, k.Down, k.Choose, k.Home}
	case viewDashboard:
		return screenKeys(k.ShortHelp())
	default:
		return append(screenKeys{k.Home}, k.ShortHelp()...)
	}
}

// commandNote spells out the keys that only some terminals can deliver.
func (k keymap) commandNote() string {
	if !k.enhanced {
		return "this terminal cannot tell " + ui.Keystroke("ctrl+enter") +
			" apart from " + ui.Keystroke("ctrl+j") + ", so " + ui.Keystroke("ctrl+r") + " runs a statement"
	}
	return ui.Keystrokes("ctrl+enter", "super+enter") + " both run a statement"
}
