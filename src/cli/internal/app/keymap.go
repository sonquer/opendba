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
	Ask         key.Binding
	Switch      key.Binding
	Release     key.Binding
	Thinking    key.Binding
	Page        key.Binding
	Schema      key.Binding
	Indexes     key.Binding
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
	Expand      key.Binding
	Sidebar     key.Binding
	Zoom        key.Binding
	Cancel      key.Binding
	Insert      key.Binding
	Grow        key.Binding
	Shrink      key.Binding
	Left        key.Binding
	Right       key.Binding
	Terminate   key.Binding
	Pick        key.Binding
	// Commands carries a label as well as its key, because a screen where text
	// is typed cannot offer the slash and its footer has to name the alias that
	// does work there.
	Commands key.Binding
	Above    key.Binding
	Below    key.Binding
	Edit     key.Binding
	Find     key.Binding
	Order    key.Binding
	Reverse  key.Binding
	enhanced bool
}

func newKeymap() keymap {
	return keymap{
		Run:         runBinding(false),
		Palette:     binding("commands", "/"),
		Connections: binding("connections", "ctrl+p"),
		Catalog:     binding("databases", "ctrl+d"),
		Query:       binding("query", "e"),
		Ask:         binding("ask", "a"),
		Switch:      binding("change what answers", "ctrl+o"),
		Release:     binding("release the model", "r"),
		Thinking:    binding("thinking", "ctrl+t"),
		Page:        binding("scroll", "pgup", "pgdown"),
		Schema:      binding("tables", "s"),
		Indexes:     binding("indexes", "i"),
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
		Expand:      binding("columns", "space"),
		Sidebar:     binding("tables", "ctrl+b"),
		Zoom:        binding("zoom", "z"),
		Cancel:      binding("cancel", "c"),
		Insert:      binding("insert", "i"),
		Grow:        binding("taller", "ctrl+up"),
		Shrink:      binding("shorter", "ctrl+down"),
		Left:        binding("left", "left", "h"),
		Right:       binding("right", "right", "l"),
		Terminate:   binding("close session", "x"),
		Pick:        binding("pick", "space"),
		Commands:    key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp(ui.Keystroke("ctrl+k"), "commands")),
		Above:       binding("up", "up"),
		Below:       binding("down", "down"),
		Edit:        binding("edit", "e"),
		Find:        binding("find", "f"),
		Order:       binding("sort", "o"),
		Reverse:     binding("reverse", "O"),
	}
}

// opensPalette answers the key that reaches the command list. A screen where
// text is typed takes the alias only, because a slash there is a slash.
func (k keymap) opensPalette(msg tea.KeyPressMsg, typing bool) bool {
	if typing {
		return key.Matches(msg, k.Commands)
	}
	return key.Matches(msg, k.Palette) || key.Matches(msg, k.Commands)
}

// Save and Discard are the two answers a form takes. They are the keys that
// already mean open and back, said in the words of a form.
func (k keymap) Save() key.Binding { return relabel(k.Choose, "save") }

func (k keymap) Discard() key.Binding { return relabel(k.Back, "cancel") }

// Send and Stop are the same two keys again, in the words of a conversation.
// Esc stops an answer that is arriving and goes back when none is.
func (k keymap) Send() key.Binding { return relabel(k.Choose, "send") }

func (k keymap) Stop() key.Binding { return relabel(k.Back, "stop or back") }

func relabel(from key.Binding, help string) key.Binding {
	return key.NewBinding(key.WithKeys(from.Keys()...),
		key.WithHelp(from.Help().Key, help))
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
		k.Query, k.Ask, k.Schema, k.Indexes, k.Connections, k.Catalog, k.Palette, k.Help, k.Quit,
	}
}

func (k keymap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Query, k.Ask, k.Run, k.Focus},
		{k.Schema, k.Indexes, k.Reload},
		{k.Palette, k.Catalog, k.Connections, k.New, k.Remove},
		{k.Focus, k.Cancel, k.Terminate},
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
func (k keymap) footer(current view, completing, zoomed, running, filtering bool) screenKeys {
	if completing {
		return screenKeys{k.Accept, k.Above, k.Below, k.Back}
	}
	if zoomed {
		return screenKeys{k.Zoom, k.Up, k.Down, k.Home, k.Leave}
	}
	switch current {
	case viewQuery:
		return screenKeys{k.Run, k.Focus, k.Sidebar, k.Palette, k.Home, k.Leave}
	case viewSwitch:
		return screenKeys{k.Up, k.Down, k.Choose, k.Edit, k.New, k.Remove, k.Home}
	case viewCatalog:
		return screenKeys{k.Up, k.Down, k.Pick, k.Save(), k.Discard()}
	case viewSchema, viewIndexes:
		if filtering {
			return screenKeys{k.Above, k.Below, k.Save(), k.Discard()}
		}
		return screenKeys{k.Up, k.Down, k.Choose, k.Find, k.Order, k.Reverse, k.Home}
	case viewAsk:
		return screenKeys{k.Send(), k.Stop(), k.Page, k.Thinking, k.Switch, k.Commands}
	case viewAI:
		return screenKeys{k.Up, k.Down, k.Focus, k.Choose, k.Remove, k.Home}
	case viewDashboard:
		if running {
			return screenKeys{k.Up, k.Down, k.Cancel, k.Terminate, k.Focus, k.Quit}
		}
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
