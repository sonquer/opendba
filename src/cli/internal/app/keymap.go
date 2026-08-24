package app

import (
	"strconv"
	"strings"

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

	// NewTab, CloseTab, PrevTab and NextTab are the tabs of the editor. They
	// take the control keys the textarea uses for moving a line and deleting a
	// word, which the arrows and alt+backspace still do, because a tab is worth
	// a key somebody can find and a second way to press down is not.
	NewTab   key.Binding
	CloseTab key.Binding
	PrevTab  key.Binding
	NextTab  key.Binding

	// Jump is the digits, which reach a tab by its place rather than by walking
	// to it. A terminal that cannot send ctrl and a digit together sends alt
	// and a digit instead, and both are accepted.
	Jump key.Binding

	// Export writes the result to a file. It takes the control key the textarea
	// uses for the end of a line, which the end key still does.
	Export key.Binding

	// Copy and CopyRow put a value or a whole row on the clipboard, which is
	// what a mouse is otherwise reached for.
	Copy    key.Binding
	CopyRow key.Binding

	// History opens what has been run. Not ctrl+h: terminals send that as
	// backspace, and the editor would eat it.
	History key.Binding

	// Explain asks the server what it would do with a statement rather than
	// doing it.
	Explain key.Binding

	// Forget throws one kept thing away. It is a control key rather than a
	// letter because the lists it works in are searched by typing, and a letter
	// cannot both be typed and mean something.
	Forget   key.Binding
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
		NewTab:      binding("new tab", "ctrl+n"),
		CloseTab:    binding("close tab", "ctrl+w"),
		PrevTab:     tabBinding(false, -1),
		NextTab:     tabBinding(false, 1),
		Jump:        jumpBinding(),
		Export:      binding("export", "ctrl+e"),
		Copy:        binding("copy", "y"),
		CopyRow:     binding("copy the row", "Y"),
		History:     binding("history", "ctrl+g"),
		Explain:     binding("explain", "f6"),
		Forget:      binding("forget", "ctrl+x"),
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

// Conversations and NewConversation are the history and new-tab keys said in
// the words of the ask screen, because opening what was said before and putting
// the current one away are the same two ideas as on the editor screen.
func (k keymap) Conversations() key.Binding { return relabel(k.History, "conversations") }

func (k keymap) NewConversation() key.Binding { return relabel(k.NewTab, "new conversation") }

// Halt is esc once more, in the words of a statement that is still running. It
// is only ever drawn while one is, so the key that goes back and the key that
// gives up are never offered at the same time.
func (k keymap) Halt() key.Binding { return relabel(k.Back, "cancel the query") }

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

// jumpBinding is every way a terminal can say a digit with a modifier on it.
// Both are taken because ctrl and a digit is what the strip prints and what a
// terminal speaking the Kitty protocol sends, while everything else sends alt
// and a digit or nothing at all.
func jumpBinding() key.Binding {
	keys := make([]string, 0, 2*maxJumpTabs)
	for i := 1; i <= maxJumpTabs; i++ {
		keys = append(keys, "ctrl+"+strconv.Itoa(i), "alt+"+strconv.Itoa(i))
	}
	return key.NewBinding(key.WithKeys(keys...),
		key.WithHelp(ui.Keystroke("ctrl+1"), "tab by number"))
}

// jumped is the tab a digit reaches, counting from nought, or minus one when
// the key was not a digit with a modifier on it.
func jumped(msg tea.KeyPressMsg) int {
	name := msg.String()
	for _, prefix := range []string{"ctrl+", "alt+"} {
		digit, found := strings.CutPrefix(name, prefix)
		if !found || len(digit) != 1 || digit < "1" || digit > "9" {
			continue
		}
		return int(digit[0] - '1')
	}
	return -1
}

// tabBinding accepts both ways a terminal can say "the tab beside this one".
// Only a terminal with keyboard enhancements can send ctrl+tab at all, so the
// page keys are what gets advertised until it says it can.
func tabBinding(enhanced bool, step int) key.Binding {
	keys, label := []string{"ctrl+pgup"}, ui.Keystroke("ctrl+pgup")
	help := "previous tab"
	if step > 0 {
		keys, label = []string{"ctrl+pgdown"}, ui.Keystroke("ctrl+pgdown")
		help = "next tab"
	}
	if enhanced && step > 0 {
		keys = append(keys, "ctrl+tab")
		label = ui.Keystroke("ctrl+tab")
	}
	if enhanced && step < 0 {
		keys = append(keys, "ctrl+shift+tab")
		label = ui.Keystroke("ctrl+shift+tab")
	}
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, help))
}

func (k keymap) withEnhancements(msg tea.KeyboardEnhancementsMsg) keymap {
	k.enhanced = msg.SupportsKeyDisambiguation()
	k.Run = runBinding(k.enhanced)
	k.PrevTab = tabBinding(k.enhanced, -1)
	k.NextTab = tabBinding(k.enhanced, 1)
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
func (k keymap) footer(current view, completing, zoomed, running, filtering, inflight bool) screenKeys {
	if completing {
		return screenKeys{k.Accept, k.Above, k.Below, k.Back}
	}
	if zoomed {
		return screenKeys{k.Zoom, k.Up, k.Down, k.Home, k.Leave}
	}
	switch current {
	case viewQuery:
		if inflight {
			return screenKeys{k.Halt(), k.Focus, k.Sidebar, k.Commands, k.Leave}
		}
		return screenKeys{
			k.Run, k.NewTab, k.CloseTab, k.Commands, k.Focus, k.Sidebar, k.Home, k.Leave,
		}
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
		return screenKeys{
			k.Send(), k.Stop(), k.Conversations(), k.NewConversation(),
			k.Thinking, k.Switch, k.Commands,
		}
	case viewSettings:
		return screenKeys{k.Above, k.Below, k.Choose, k.Home, k.Leave}
	case viewAI:
		return screenKeys{k.Up, k.Down, k.Focus, k.Choose, k.Remove, k.Home}
	case viewHistory:
		return screenKeys{k.Above, k.Below, k.Choose, k.Pick, k.Home, k.Leave}
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
