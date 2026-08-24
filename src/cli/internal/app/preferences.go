package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

// preferences is settings.toml with a screen in front of it. Everything but the
// assistant was edited by hand until now, which is fine for a file somebody
// reads once and wrong for the two things on it that throw data away.
type preferences struct {
	theme   *ui.Theme
	form    form
	trouble string
}

// laterOnly is said on the fields the open connection cannot be told about. The
// row limit and the timeouts are handed to the driver when the connection is
// made, so changing them here changes the next one, and it is said on the field
// it is about rather than in a line under the whole form that reads as a
// warning about everything.
const laterOnly = "it reaches the server when a connection is opened, so this applies to the next one"

func (m Model) openPreferences() (tea.Model, tea.Cmd) {
	settings := m.session.Settings
	fields := []field{
		choiceField("bar", "bars", ui.BarStyleNames(), settings.Appearance.Bar,
			"the shape a measurement is drawn with, since a font decides how well a glyph draws").
			under("appearance"),
		toggleField("mouse", "mouse", []string{config.MouseOn, config.MouseOff},
			settings.Appearance.MouseWanted(),
			"off gives the mouse back to the terminal, which can then select text with it").
			under("appearance"),
		toggleField("own", "own sessions", []string{"show", "hide"},
			settings.Appearance.OwnSessions,
			"the dashboard's own reads are two sessions of its own; hiding them "+
				"leaves the work you asked for").
			under("appearance"),

		choiceField("mode", "opens as",
			[]string{string(config.ReadOnly), string(config.ReadWrite)},
			string(settings.Safety.DefaultMode),
			"what a new connection is made in when its profile does not say").
			under("safety"),
		toggleField("confirm", "ask on writes", []string{"yes", "no"},
			settings.Safety.ConfirmQueries,
			"show a statement that changes data and wait, before it is sent").
			under("safety"),
		textField(m.theme, "rows", "rows", strconv.Itoa(settings.Safety.RowLimit),
			"how many rows a result stops at; an export ignores it. "+laterOnly).
			require().checked(counted(1)).under("safety"),
		textField(m.theme, "query", "query time", settings.Safety.QueryTimeout,
			"how long the server lets one statement run. "+laterOnly).
			require().checked(lasting).under("safety"),
		textField(m.theme, "lock", "lock time", settings.Safety.LockTimeout,
			"how long a statement waits for a lock before giving up. "+laterOnly).
			require().checked(lasting).under("safety"),

		toggleField("history", "keep them", []string{"yes", "no"},
			settings.History.Enabled, "write down the statements you run").
			under("query history"),
		toggleField("statements", "keep the sql", []string{"yes", "no"},
			settings.History.StoreSQL,
			"keep the statement itself, rather than only that something ran").
			under("query history"),
		textField(m.theme, "queries", "how many", strconv.Itoa(settings.History.Limit),
			"how many statements to keep; nought keeps every one").
			require().checked(counted(0)).under("query history"),
		actionField("forget-queries", "clear them", "throws every statement away").
			under("query history"),

		toggleField("chats", "keep them", []string{"yes", "no"}, settings.Chats.Enabled,
			"write down conversations with the assistant, including the rows it read").
			under("conversations"),
		textField(m.theme, "conversations", "how many",
			strconv.Itoa(settings.Chats.Limit),
			"how many conversations to keep; nought keeps every one").
			require().checked(counted(0)).under("conversations"),
		actionField("forget-chats", "clear them", "throws every conversation away").
			under("conversations"),

		actionField("save", "save", "enter writes settings.toml"),
	}
	built, cmd := newForm(fields...)
	m.preferences = &preferences{theme: m.theme, form: built}
	return m.show4Preferences(cmd)
}

func (m Model) show4Preferences(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m.view = viewSettings
	m.offset = 0
	m.editor.Blur()
	return m, cmd
}

// counted refuses anything that is not a whole number at least this big.
func counted(least int) func(string) error {
	return func(value string) error {
		held, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("%q is not a number", value)
		}
		if held < least {
			return fmt.Errorf("cannot be less than %d", least)
		}
		return nil
	}
}

// lasting refuses anything that is not a length of time.
func lasting(value string) error {
	if _, err := time.ParseDuration(strings.TrimSpace(value)); err != nil {
		return fmt.Errorf("%q is not a length of time, such as 15s", value)
	}
	return nil
}

func (m Model) preferencesKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Leave) {
		return m.confirmQuit()
	}
	if key.Matches(msg, m.keys.Back) {
		m.preferences = nil
		return m.show(viewDashboard)
	}
	if m.keys.opensPalette(msg, m.preferences.form.current().editable()) {
		return m.openPalette()
	}
	held := *m.preferences
	updated, action, cmd := held.form.update(msg)
	held.form = updated
	m.preferences = &held
	switch action {
	case "save":
		return m.savePreferences()
	case "forget-queries":
		return m.confirmForgetting("the query history", forgetHistoryMsg{})
	case "forget-chats":
		return m.confirmForgetting("the conversations", forgetChatsMsg{})
	}
	return m, cmd
}

// savePreferences writes the file and applies what can be applied now.
func (m Model) savePreferences() (tea.Model, tea.Cmd) {
	held := *m.preferences
	if err := held.form.validate(); err != nil {
		held.trouble = err.Error()
		m.preferences = &held
		return m, nil
	}
	next := m.settled4Preferences()
	if err := m.workspace.Setup().Store.SaveSettings(next); err != nil {
		held.trouble = err.Error()
		m.preferences = &held
		return m, nil
	}
	held.trouble = ""
	m.preferences = &held
	m.session.Settings = next
	m.mouse = next.Appearance.MouseWanted()
	m.theme.Bars(next.Appearance.Bar)
	m.running = m.running.showingOwn(next.Appearance.OwnSessions)
	return m, m.notify("settings.toml is written")
}

// settled4Preferences is the settings the form is describing.
func (m Model) settled4Preferences() config.Settings {
	held := m.preferences.form
	next := m.session.Settings
	next.Appearance.Bar = held.value("bar")
	next.Appearance.Mouse = config.MouseOff
	if held.enabled("mouse") {
		next.Appearance.Mouse = config.MouseOn
	}
	next.Appearance.OwnSessions = held.enabled("own")
	next.Safety.DefaultMode = config.AccessMode(held.value("mode"))
	next.Safety.ConfirmQueries = held.enabled("confirm")
	next.Safety.RowLimit = number(held.value("rows"))
	next.Safety.QueryTimeout = held.value("query")
	next.Safety.LockTimeout = held.value("lock")
	next.History.Enabled = held.enabled("history")
	next.History.StoreSQL = held.enabled("statements")
	next.History.Limit = number(held.value("queries"))
	next.Chats.Enabled = held.enabled("chats")
	next.Chats.Limit = number(held.value("conversations"))
	return next
}

func number(value string) int {
	held, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return held
}

// preferencesMsg opens the settings screen.
type preferencesMsg struct{}

type forgetHistoryMsg struct{}

type forgetChatsMsg struct{}

// confirmForgetting is the question in front of a store being emptied. It says
// how much is about to go, because "are you sure?" is a question nobody reads
// and "2,431 statements" is a number somebody weighs.
func (m Model) confirmForgetting(what string, action tea.Msg) (tea.Model, tea.Cmd) {
	held := m.counted4Preferences(action)
	dialog := ask(m.theme, "clear "+what+"?", "", action)
	dialog.tag = m.theme.Muted.Render(held)
	dialog.warning(held+" would be thrown away, and this is the only copy of them",
		"throw them away")
	m.modal = dialog
	return m, nil
}

func (m Model) counted4Preferences(action tea.Msg) string {
	ctx, cancel := context.WithTimeout(context.Background(), rememberTimeout)
	defer cancel()
	if _, ok := action.(forgetChatsMsg); ok {
		if m.session.Chats == nil {
			return "no conversations"
		}
		count, err := m.session.Chats.Count(ctx)
		if err != nil {
			return "what is kept"
		}
		return ui.Plural(count, "conversation", "conversations")
	}
	if m.session.History == nil {
		return "no statements"
	}
	count, err := m.session.History.Count(ctx)
	if err != nil {
		return "what is kept"
	}
	return ui.Plural(count, "statement", "statements")
}

func (m Model) forgetHistory() (tea.Model, tea.Cmd) {
	if m.session.History == nil {
		return m, m.notify("nothing is being kept")
	}
	store := m.session.History
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rememberTimeout)
		defer cancel()
		if err := store.Clear(ctx); err != nil {
			return toastMsg{}
		}
		return clearedMsg{what: "the query history"}
	}
}

func (m Model) forgetChats() (tea.Model, tea.Cmd) {
	if m.session.Chats == nil {
		return m, m.notify("nothing is being kept")
	}
	store := m.session.Chats
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rememberTimeout)
		defer cancel()
		if err := store.Clear(ctx); err != nil {
			return toastMsg{}
		}
		return clearedMsg{what: "the conversations"}
	}
}

type clearedMsg struct{ what string }

func (m Model) cleared(msg clearedMsg) (tea.Model, tea.Cmd) {
	if msg.what == "the conversations" {
		m.talk.id = 0
	}
	return m, m.notify(msg.what + " are empty")
}

func (m Model) preferencesBody() string {
	width := ui.FrameWidth(m.width)
	held := m.preferences
	lines := []string{
		m.theme.Screen("settings", m.theme.Muted.Render("settings.toml"), width),
		"",
		held.form.view(m.theme, width),
	}
	if held.trouble != "" {
		lines = append(lines, "", m.theme.Error.Render("✗ "+wrap(held.trouble, width-4)))
	}
	return strings.Join(lines, "\n")
}
