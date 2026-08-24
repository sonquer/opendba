package app

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/opendba/src/cli/internal/ui"
)

type fieldKind int

const (
	fieldText fieldKind = iota
	fieldSecret
	fieldChoice
	fieldToggle
	fieldAction
)

const (
	labelWidth = 14
	valueWidth = 44
)

type field struct {
	key   string
	label string
	hint  string

	// section is the heading this field belongs under, announced once when it
	// changes.
	section  string
	kind     fieldKind
	input    textinput.Model
	choices  []string
	choice   int
	on       bool
	required bool
	rule     func(string) error
}

func (f field) require() field {
	f.required = true
	return f
}

func (f field) checked(rule func(string) error) field {
	f.rule = rule
	return f
}

func (f field) problem() error {
	if f.kind == fieldAction {
		return nil
	}
	value := f.value()
	if f.required && value == "" {
		return fmt.Errorf("%s cannot be empty", f.label)
	}
	if f.rule == nil || value == "" {
		return nil
	}
	if err := f.rule(value); err != nil {
		return fmt.Errorf("%s: %w", f.label, err)
	}
	return nil
}

func textField(theme *ui.Theme, key, label, value, hint string) field {
	return field{key: key, label: label, hint: hint, kind: fieldText, input: input(theme, value, false)}
}

func secretField(theme *ui.Theme, key, label, value, hint string) field {
	return field{key: key, label: label, hint: hint, kind: fieldSecret, input: input(theme, value, true)}
}

func choiceField(key, label string, choices []string, selected string, hint string) field {
	entry := field{key: key, label: label, hint: hint, kind: fieldChoice, choices: choices}
	for i, choice := range choices {
		if choice == selected {
			entry.choice = i
		}
	}
	return entry
}

func toggleField(key, label string, choices []string, on bool, hint string) field {
	return field{key: key, label: label, hint: hint, kind: fieldToggle, choices: choices, on: on}
}

func actionField(key, label, hint string) field {
	return field{key: key, label: label, hint: hint, kind: fieldAction}
}

func input(theme *ui.Theme, value string, secret bool) textinput.Model {
	model := textinput.New()
	model.Prompt = ""
	model.SetValue(value)
	model.SetWidth(valueWidth)
	if secret {
		model.EchoMode = textinput.EchoPassword
		model.EchoCharacter = '•'
	}
	styles := textinput.DefaultStyles(true)
	styles.Focused.Text = lipgloss.NewStyle().Foreground(theme.P.Fg)
	styles.Blurred.Text = lipgloss.NewStyle().Foreground(theme.P.Fg)
	styles.Cursor.Color = theme.P.Accent
	model.SetStyles(styles)
	return model
}

func (f field) editable() bool { return f.kind == fieldText || f.kind == fieldSecret }

func (f field) value() string {
	switch f.kind {
	case fieldChoice:
		if len(f.choices) == 0 {
			return ""
		}
		return f.choices[f.choice]
	case fieldToggle:
		if f.on {
			return f.choices[0]
		}
		return f.choices[1]
	case fieldAction:
		return ""
	default:
		return strings.TrimSpace(f.input.Value())
	}
}

type form struct {
	fields []field
	focus  int
}

func newForm(fields ...field) (form, tea.Cmd) {
	built := form{fields: fields}
	return built.focusOn(0)
}

func (f form) focusOn(index int) (form, tea.Cmd) {
	fields := append([]field(nil), f.fields...)
	for i := range fields {
		if fields[i].editable() {
			fields[i].input.Blur()
		}
	}
	f.fields = fields
	f.focus = index
	if !f.current().editable() {
		return f, nil
	}
	return f, f.fields[index].input.Focus()
}

func (f form) blink() tea.Cmd {
	if len(f.fields) == 0 || !f.current().editable() {
		return nil
	}
	return textinput.Blink
}

func (f form) current() field {
	if f.focus < 0 || f.focus >= len(f.fields) {
		return field{}
	}
	return f.fields[f.focus]
}

func (f form) value(key string) string {
	for _, entry := range f.fields {
		if entry.key == key {
			return entry.value()
		}
	}
	return ""
}

func (f form) secret(key string) []byte {
	for _, entry := range f.fields {
		if entry.key == key && entry.editable() {
			return []byte(entry.input.Value())
		}
	}
	return nil
}

func (f form) has(key string) bool {
	for _, entry := range f.fields {
		if entry.key == key {
			return true
		}
	}
	return false
}

func (f form) valueOr(key, fallback string) string {
	if f.has(key) {
		return f.value(key)
	}
	return fallback
}

func (f form) enabledOr(key string, fallback bool) bool {
	if f.has(key) {
		return f.enabled(key)
	}
	return fallback
}

func (f form) enabled(key string) bool {
	for _, entry := range f.fields {
		if entry.key == key {
			return entry.on
		}
	}
	return false
}

func (f form) move(step int) (form, tea.Cmd) {
	if len(f.fields) == 0 {
		return f, nil
	}
	index := (f.focus + step + len(f.fields)) % len(f.fields)
	return f.focusOn(index)
}

func (f form) cycle(step int) form {
	entry := f.current()
	fields := append([]field(nil), f.fields...)
	switch entry.kind {
	case fieldChoice:
		if len(entry.choices) == 0 {
			return f
		}
		fields[f.focus].choice = (entry.choice + step + len(entry.choices)) % len(entry.choices)
	case fieldToggle:
		fields[f.focus].on = !entry.on
	default:
		return f
	}
	f.fields = fields
	return f
}

func (f form) paste(msg tea.PasteMsg) (form, int, tea.Cmd) {
	if len(f.fields) == 0 || !f.current().editable() {
		return f, 0, nil
	}
	fields := append([]field(nil), f.fields...)
	before := len([]rune(fields[f.focus].input.Value()))
	updated, cmd := fields[f.focus].input.Update(msg)
	fields[f.focus].input = updated
	f.fields = fields
	return f, len([]rune(updated.Value())) - before, cmd
}

func (f form) update(msg tea.KeyPressMsg) (form, string, tea.Cmd) {
	if len(f.fields) == 0 {
		return f, "", nil
	}
	switch msg.String() {
	case "tab", "down":
		updated, cmd := f.move(1)
		return updated, "", cmd
	case "shift+tab", "up":
		updated, cmd := f.move(-1)
		return updated, "", cmd
	case "left":
		return f.cycle(-1), "", nil
	case "right", "space":
		if f.current().editable() {
			break
		}
		return f.cycle(1), "", nil
	case "enter":
		if f.current().kind == fieldAction {
			return f, f.current().key, nil
		}
		updated, cmd := f.move(1)
		return updated, "", cmd
	}
	if !f.current().editable() {
		return f, "", nil
	}
	fields := append([]field(nil), f.fields...)
	updated, cmd := fields[f.focus].input.Update(msg)
	fields[f.focus].input = updated
	f.fields = fields
	return f, "", cmd
}

func (f form) validate() error {
	var problems []string
	for _, entry := range f.fields {
		if err := entry.problem(); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, ", "))
}

// under puts a field under a heading.
func (f field) under(section string) field {
	f.section = section
	return f
}

func (f form) view(theme *ui.Theme, width int) string {
	lines := make([]string, 0, len(f.fields)+1)
	section := ""
	for i, entry := range f.fields {
		if entry.section != section {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			if entry.section != "" {
				lines = append(lines, theme.Section(entry.section, "", width), "")
			}
			section = entry.section
		}
		lines = append(lines, f.row(theme, entry, i == f.focus))
	}
	if hint := f.current().hint; hint != "" {
		lines = append(lines, "", theme.Muted.Render(strings.Repeat(" ", labelWidth+2)+hint))
	}
	return strings.Join(lines, "\n")
}

func (f form) row(theme *ui.Theme, entry field, active bool) string {
	marker := "  "
	if active {
		marker = theme.Accent.Render("▌ ")
	}
	if entry.kind == fieldAction {
		return marker + ui.Pad("", labelWidth) + f.cell(theme, entry, active)
	}
	style := theme.Label
	if active {
		style = theme.Value
	}
	label := style.Render(ui.Pad(entry.label, labelWidth))
	if entry.required {
		label = style.Render(ui.Pad(entry.label, labelWidth-2)) +
			theme.Severity(ui.SevCritical).Render("*") + " "
	}
	return marker + label + f.cell(theme, entry, active)
}

func (f form) cell(theme *ui.Theme, entry field, active bool) string {
	switch entry.kind {
	case fieldAction:
		style := theme.Muted
		if active {
			style = theme.Accent.Bold(true)
		}
		return style.Render("[ " + entry.label + " ]")
	case fieldChoice, fieldToggle:
		return f.options(theme, entry, active)
	default:
		if active {
			return entry.input.View()
		}
		if entry.value() == "" {
			return theme.Subtle.Render("—")
		}
		return theme.Value.Render(entry.display())
	}
}

func (f field) display() string {
	if f.kind == fieldSecret {
		return strings.Repeat("•", len([]rune(f.input.Value())))
	}
	return f.input.Value()
}

const inlineChoices = 3

func (f form) options(theme *ui.Theme, entry field, active bool) string {
	if len(entry.choices) > inlineChoices {
		return f.picker(theme, entry, active)
	}
	rendered := make([]string, 0, len(entry.choices))
	for i, choice := range entry.choices {
		selected := i == entry.choice
		if entry.kind == fieldToggle {
			selected = (i == 0) == entry.on
		}
		switch {
		case selected && active:
			rendered = append(rendered, lipgloss.NewStyle().
				Foreground(theme.P.OnEnv).Background(theme.P.Accent).Padding(0, 1).Render(choice))
		case selected:
			rendered = append(rendered, theme.Value.Render(" "+choice+" "))
		default:
			rendered = append(rendered, theme.Subtle.Render(" "+choice+" "))
		}
	}
	return strings.Join(rendered, " ")
}

func (f form) picker(theme *ui.Theme, entry field, active bool) string {
	value := theme.Value.Render(entry.value())
	if !active {
		return "  " + value
	}
	return theme.Muted.Render("‹ ") + theme.Accent.Render(entry.value()) + theme.Muted.Render(" ›")
}
