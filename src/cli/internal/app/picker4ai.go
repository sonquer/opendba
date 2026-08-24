package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/ai"
	"github.com/sonquer/opendba/src/cli/internal/ai/providers/local"
	"github.com/sonquer/opendba/src/cli/internal/cli"
	"github.com/sonquer/opendba/src/cli/internal/config"
	"github.com/sonquer/opendba/src/cli/internal/ui"
	"github.com/sonquer/opendba/src/cli/pkg/secretref"
)

const (
	chooserWidth   = 62
	minChooserRows = 4

	// hereSection is the group that needs nothing from anybody, which is why it
	// is first.
	hereSection = "on this machine"
)

// deed is what choosing a row does.
type deed int

const (
	useInstance deed = iota
	useModel
	askForKey
	useEnvironment
)

// offer is one row of the modal: what it says, and what it does.
type offer struct {
	key      string
	label    string
	note     string
	section  string
	hint     string
	current  bool
	deed     deed
	host     cli.Hosted
	model    local.Entry
	instance config.AIInstance
	env      string

	// mark is the state of a row, said in a glyph rather than in a word.
	mark string

	// verdict is what this machine makes of the model.
	verdict local.Verdict
	here    bool
}

// chooser is the modal that says what can answer, and lets one of them be
// chosen.
type chooser struct {
	theme  *ui.Theme
	filter textinput.Model
	offers []offer
	cursor int
	top    int

	// asking is the second half of this modal: the field that takes a key.
	asking  bool
	host    cli.Hosted
	field   textinput.Model
	trouble string

	// giving is the question a download gets when somebody tries to leave.
	giving bool
}

func newChooser(theme *ui.Theme) *chooser {
	return &chooser{theme: theme, filter: input(theme, "", false)}
}

// choosing gathers everything that could answer and opens the modal on
// whatever is answering now.
func (m Model) choosing() (Model, tea.Cmd) {
	built := newChooser(m.theme)
	built.offers = m.offers()
	built = built.at(m.session.Settings.AI.Active)
	m.chooser = built
	return m, tea.Batch(built.filter.Focus(), m.probeOllama())
}

// offers is the whole list, local first.
func (m Model) offers() []offer {
	offers := m.models4Offer()
	for _, host := range cli.Hosts() {
		offers = append(offers, m.host4Offer(host)...)
	}
	return offers
}

func (m Model) models4Offer() []offer {
	entries, err := local.Catalogue()
	if err != nil {
		return nil
	}
	store := m.session.AI.Models
	room := m.room4AI()
	active := m.session.Settings.AI.Active
	answering := m.session.Settings.AI.Enabled
	offers := make([]offer, 0, len(entries))
	for _, entry := range entries {
		here := store != nil && store.Has(entry.ID)
		instance, configured := m.local4Instance(entry.ID)
		verdict := local.Fits(entry, m.session.Settings.AI.Context, room)
		offers = append(offers, offer{
			key:      entry.ID,
			label:    entry.Title,
			note:     note4Model(entry, verdict, here),
			section:  hereSection,
			hint:     m.library4AI(),
			current:  answering && configured && instance.Name == active,
			deed:     useModel,
			model:    entry,
			instance: instance,
			mark:     mark4Model(here, answering && configured && instance.Name == active),
			verdict:  verdict,
			here:     here,
		})
	}
	return offers
}

// mark4Model is the state of a model in one glyph: the one answering, one that
// is here and ready, or one that would have to be fetched.
func mark4Model(here, answering bool) string {
	switch {
	case answering:
		return "▌"
	case here:
		return "✓"
	default:
		return ""
	}
}

// local4Instance finds the instance that runs a model here, whatever it was
// named.
func (m Model) local4Instance(model string) (config.AIInstance, bool) {
	for _, instance := range m.session.Settings.AI.Instances {
		if ai.Kind(instance.Kind) == ai.KindLocal && instance.Model == model {
			return instance, true
		}
	}
	return config.AIInstance{}, false
}

// note4Model is what is worth saying about a model beside its name: what it
// costs to fetch and what it may be used for.
func note4Model(entry local.Entry, verdict local.Verdict, here bool) string {
	said := fmt.Sprintf("%s · %s", ui.ByteSize(entry.Bytes), entry.Licence)
	if !here && verdict.Fits && !verdict.Comfortable {
		return said + " · tight"
	}
	return said
}

// host4Offer is one hosted back-end: the instances already configured for it,
// then the way to add one.
func (m Model) host4Offer(host cli.Hosted) []offer {
	section := strings.ToLower(host.Title)
	settings := m.session.Settings.AI
	offers := []offer{}
	for _, instance := range settings.Instances {
		if ai.Kind(instance.Kind) != host.Kind {
			continue
		}
		offers = append(offers, offer{
			key:      instance.Name,
			label:    instance.Model,
			note:     instance.Name,
			section:  section,
			current:  settings.Enabled && instance.Name == settings.Active,
			deed:     useInstance,
			instance: instance,
		})
	}
	if reference, ok := cli.FromEnvironment(host, os.Getenv); ok {
		return append(offers, offer{
			key:     section + ":env",
			label:   "use the key already in your environment",
			note:    host.Env,
			section: section,
			hint:    host.Env + " is set",
			deed:    useEnvironment,
			host:    host,
			env:     reference,
		})
	}
	if host.Env == "" {
		return append(offers, offer{
			key:     section + ":add",
			label:   "use this daemon",
			section: section,
			hint:    m.ollama4Hint(),
			deed:    askForKey,
			host:    host,
		})
	}
	return append(offers, offer{
		key:     section + ":add",
		label:   "add a key",
		section: section,
		hint:    host.Note,
		deed:    askForKey,
		host:    host,
	})
}

// ollama4Hint is what is known about the daemon so far.
func (m Model) ollama4Hint() string {
	if m.ai.ollama == "" {
		return "a daemon you run yourself"
	}
	return m.ai.ollama
}

// probeOllama asks the daemon whether it is there, in the background, the way
// every other question this program puts to a server is asked.
func (m Model) probeOllama() tea.Cmd {
	registry := m.session.AI.Registry
	if registry == nil {
		return nil
	}
	return func() tea.Msg {
		client, err := registry.Open(ai.Instance{Name: "ollama", Kind: ai.KindOllama}, ai.Deps{HTTP: cli.Downloader()})
		if err != nil {
			return ollamaMsg{}
		}
		prober, ok := client.(ai.Prober)
		if !ok {
			return ollamaMsg{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		defer cancel()
		return ollamaMsg{running: prober.Probe(ctx) == nil}
	}
}

// ollamaMsg is what the daemon said, or did not.
type ollamaMsg struct{ running bool }

// answered4Ollama puts what was learned on the list.
func (m Model) answered4Ollama(msg ollamaMsg) (tea.Model, tea.Cmd) {
	m.ai.ollama = "not running here"
	if msg.running {
		m.ai.ollama = "running here"
	}
	if m.chooser != nil {
		m.chooser.offers = m.offers()
	}
	return m, nil
}

const probeTimeout = 2 * time.Second

func (c *chooser) at(key string) *chooser {
	for i, item := range c.offers {
		if item.key == key {
			c.cursor = i
			return c
		}
	}
	return c
}

// found is what the filter left.
func (c *chooser) found() []offer {
	needle := strings.ToLower(strings.TrimSpace(c.filter.Value()))
	if needle == "" {
		return c.offers
	}
	kept := make([]offer, 0, len(c.offers))
	for _, item := range c.offers {
		if !strings.Contains(strings.ToLower(item.label+" "+item.section+" "+item.note), needle) {
			continue
		}
		item.note = item.section
		item.section = ""
		kept = append(kept, item)
	}
	return kept
}

func (c *chooser) selected() (offer, bool) {
	found := c.found()
	if c.cursor < 0 || c.cursor >= len(found) {
		return offer{}, false
	}
	return found[c.cursor], true
}

func (c *chooser) move(step int) *chooser {
	found := c.found()
	if len(found) == 0 {
		return c
	}
	c.cursor = (c.cursor + step + len(found)) % len(found)
	return c
}

// chooserRows is how many rows fit.
func chooserRows(height, groups int) int {
	rows := ui.BodyHeight(height) - 8 - groups*2
	return max(rows, minChooserRows)
}

// window keeps the cursor on screen.
func (c *chooser) window(found []offer, rows int) []offer {
	if len(found) <= rows {
		c.top = 0
		return found
	}
	if c.cursor < c.top {
		c.top = c.cursor
	}
	if c.cursor >= c.top+rows {
		c.top = c.cursor - rows + 1
	}
	c.top = min(max(c.top, 0), len(found)-rows)
	return found[c.top : c.top+rows]
}

// groups counts the headings a list would draw, which is what they cost in room.
func groups(found []offer) int {
	seen := map[string]bool{}
	for _, item := range found {
		if item.section != "" {
			seen[item.section] = true
		}
	}
	return len(seen)
}

func (m Model) chooserView(width, height int) string {
	inner := min(ui.TextWidth(width)-6, chooserWidth)
	if m.chooser.asking {
		return m.key4View(inner)
	}
	if m.ai.busy != "" {
		return m.busy4View(inner)
	}
	found := m.chooser.found()
	shown := m.chooser.window(found, chooserRows(height, groups(found)))

	parts := []string{
		ui.SplitLine(m.theme.Title.Render("what answers"),
			m.theme.KeycapStyle.Render("esc"), inner),
		"",
		m.theme.Prompt.Render("› ") + m.chooser.filter.View(),
		"",
	}
	parts = append(parts, m.rows4Chooser(shown, inner)...)
	if more := len(found) - len(shown) - m.chooser.top; more > 0 {
		parts = append(parts, m.theme.Subtle.Render(fmt.Sprintf("  … %d more", more)))
	}
	if m.chooser.trouble != "" {
		parts = append(parts, "", m.theme.Error.Render("  ✗ "+m.chooser.trouble))
	}
	parts = append(parts, "", m.chooser4Foot())
	return m.theme.Panel.Render(square(strings.Join(parts, "\n"), inner))
}

// busy4View is the modal while something is arriving.
func (m Model) busy4View(inner int) string {
	if m.chooser.giving {
		return m.giving4View(inner)
	}
	parts := []string{
		ui.SplitLine(m.theme.Title.Render(m.doing4AI()),
			m.theme.Subtle.Render(m.spinner.View()), inner),
		"",
		m.theme.Value.Render(m.subject4AI()),
		"",
		m.theme.Track(m.ai.progress.Ratio(), inner),
		m.theme.Muted.Render(m.arrived4AI()),
		"",
		m.theme.Hints(ui.Hint{Key: "esc", Does: "stop"}),
	}
	return m.theme.Panel.Render(square(strings.Join(parts, "\n"), inner))
}

// giving4View is the question a download gets on the way out. It says what
// happens to the bytes that arrived, because that is what the answer turns on.
func (m Model) giving4View(inner int) string {
	parts := []string{
		m.theme.Title.Render("stop the download?"),
		"",
		m.theme.Muted.Render(wrap(m.subject4AI()+" is "+m.arrived4AI()+
			". What is here is kept, so choosing it again carries on from where it stopped.", inner)),
		"",
		m.theme.Hints(
			ui.Hint{Key: "enter", Does: "stop it"},
			ui.Hint{Key: "esc", Does: "carry on"},
		),
	}
	return m.theme.Panel.Render(square(strings.Join(parts, "\n"), inner))
}

// rows4Chooser draws the list. A row is not marked with a bar in the margin the
// way a list on a screen is: this is a dialog, the chosen row is the whole point
// of it, and the whole line lights up.
func (m Model) rows4Chooser(shown []offer, width int) []string {
	lines := make([]string, 0, len(shown)+len(shown)/2)
	section := ""
	for i, item := range shown {
		if item.section != section {
			section = item.section
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, m.theme.Section(section, item.hint, width))
		}
		lines = append(lines, m.row4Chooser(item, width, i+m.chooser.top == m.chooser.cursor))
	}
	if len(lines) == 0 {
		return []string{m.theme.Muted.Render("  nothing matches")}
	}
	return lines
}

// row4Chooser draws one row. The chosen one is laid out from plain text and then
// coloured whole, so that the highlight reaches both ends of the line and lands
// in the same columns as every row above it.
func (m Model) row4Chooser(item offer, width int, active bool) string {
	gutter := "  "
	if item.mark != "" {
		gutter = item.mark + " "
	}
	if active {
		return m.theme.Selected2.Render(ui.SplitLine(gutter+item.label, item.note, width))
	}
	name := m.theme.Value
	if item.grey() {
		name = m.theme.Subtle
	}
	left := m.mark4Chooser(item) + name.Render(item.label)
	if item.note == "" {
		return left
	}
	return ui.SplitLine(left, m.theme.Subtle.Render(item.note), width)
}

// grey is a row that cannot do what it offers on this machine.
func (o offer) grey() bool { return o.deed == useModel && !o.here && !o.verdict.Fits }

func (m Model) mark4Chooser(item offer) string {
	switch item.mark {
	case "▌":
		return m.theme.Accent.Render("▌ ")
	case "✓":
		return m.theme.Severity(ui.SevOK).Render("✓ ")
	default:
		return "  "
	}
}

// chooser4Foot offers d only on a row that has weights to remove, because a key
// that does nothing on eleven rows out of twelve is a key nobody trusts.
func (m Model) chooser4Foot() string {
	hints := []ui.Hint{{Key: "enter", Does: "use"}}
	if chosen, ok := m.chooser.selected(); ok && chosen.deed == useModel {
		if m.talk.loaded && chosen.current {
			hints = append(hints, ui.Hint{Key: "r", Does: "release"})
		}
		if chosen.here {
			hints = append(hints, ui.Hint{Key: "d", Does: "remove"})
		}
	}
	return m.theme.Hints(append(hints, ui.Hint{Key: "esc", Does: "close"})...)
}

// let4Go gives back the memory the model under the cursor is loaded into.
func (m Model) let4Go() (tea.Model, tea.Cmd) {
	chosen, ok := m.chooser.selected()
	if !ok || chosen.deed != useModel || !chosen.current || !m.talk.loaded {
		return m, nil
	}
	return m.released()
}

// chooserKey is what the modal does with a key.
func (m Model) chooserKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.chooser.asking {
		return m.typing4Key(msg)
	}
	if m.ai.busy != "" {
		return m.busy4Key(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Back):
		m.chooser = nil
		return m, m.talk.prompt.Focus()
	case key.Matches(msg, m.keys.Up):
		m.chooser = m.chooser.move(-1)
		return m, nil
	case key.Matches(msg, m.keys.Down):
		m.chooser = m.chooser.move(1)
		return m, nil
	case key.Matches(msg, m.keys.Choose):
		return m.chose4Chooser()
	case key.Matches(msg, m.keys.Remove) && !m.chooser.typing():
		return m.forget4Chooser()
	case key.Matches(msg, m.keys.Release) && !m.chooser.typing():
		return m.let4Go()
	}
	updated, cmd := m.chooser.filter.Update(msg)
	m.chooser.filter = updated
	m.chooser.cursor, m.chooser.top = 0, 0
	return m, cmd
}

// busy4Key is what a key does while something is arriving.
func (m Model) busy4Key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if !m.chooser.giving {
		if key.Matches(msg, m.keys.Back) {
			m.chooser.giving = true
		}
		return m, nil
	}
	switch {
	case key.Matches(msg, m.keys.Choose):
		m.chooser.giving = false
		m.pending = ""
		return m.stopFetching(), nil
	case key.Matches(msg, m.keys.Back):
		m.chooser.giving = false
	}
	return m, nil
}

// typing reports whether the filter has anything in it, which is what decides
// whether a bare letter is a command or a letter.
func (c *chooser) typing() bool { return c.filter.Value() != "" }

// forget4Chooser removes the weights of a model that is here.
func (m Model) forget4Chooser() (tea.Model, tea.Cmd) {
	chosen, ok := m.chooser.selected()
	if !ok || chosen.deed != useModel {
		return m, nil
	}
	if m.session.AI.Models == nil {
		m.chooser.trouble = "there is nowhere on this machine that models are kept"
		return m, nil
	}
	if !m.session.AI.Models.Has(chosen.model.ID) {
		return m, m.notify(chosen.model.Title + " is not downloaded, so there is nothing to remove")
	}
	m.modal = ask(m.theme, "remove "+chosen.model.Title+"?",
		ui.ByteSize(chosen.model.Bytes)+" is deleted. It can be downloaded again.",
		forgetMsg{id: chosen.model.ID})
	return m, nil
}

// forgetMsg is that question answered yes.
type forgetMsg struct{ id string }

// forgot deletes the weights and puts the list back the way it now is.
func (m Model) forgot(msg forgetMsg) (tea.Model, tea.Cmd) {
	entry, err := local.Offered(msg.id)
	if err != nil {
		return m.troubled4AI(err.Error()), nil
	}
	if m.session.AI.Models == nil {
		return m, nil
	}
	if m.talk.loaded && m.local4Model(msg.id) {
		m, _ = m.released()
	}
	if err := m.session.AI.Models.Remove(msg.id); err != nil {
		return m.troubled4AI(err.Error()), nil
	}
	read := m.read4AI()
	if read.chooser != nil {
		read.chooser.offers = read.offers()
		read.chooser.trouble = ""
	}
	return read, read.notify(entry.Title + " was removed")
}

// chose4Chooser is what a row does. Every one of them leads somewhere.
func (m Model) chose4Chooser() (tea.Model, tea.Cmd) {
	chosen, ok := m.chooser.selected()
	if !ok || m.ai.busy != "" {
		return m, nil
	}
	switch chosen.deed {
	case useInstance:
		return m.activate(chosen.instance.Name)
	case useModel:
		if chosen.grey() {
			return m.asked4Anyway(chosen)
		}
		return m.chose4Model(chosen.model)
	case useEnvironment:
		return m.add4Host(chosen.host, chosen.env)
	default:
		return m.asking4Key(chosen.host)
	}
}

// asked4Anyway is the question a model too big for this machine gets.
func (m Model) asked4Anyway(chosen offer) (tea.Model, tea.Cmd) {
	dialog := ask(m.theme, "fetch it anyway?", chosen.verdict.Reason+".", anywayMsg{id: chosen.model.ID})
	dialog.tag = chosen.model.Title
	dialog.chart = m.cost4Model(chosen)
	m.modal = dialog
	return m, nil
}

// cost4Model draws what a model would take against what this machine has, on the
// same bars as every reading on the dashboard.
func (m Model) cost4Model(chosen offer) string {
	room := m.room4AI()
	readings := []ui.Reading{}
	if room.Memory > 0 {
		readings = append(readings, share4Reading("memory", chosen.verdict.Need.Total, room.Memory))
	}
	if room.FreeDisk > 0 {
		readings = append(readings, share4Reading("disk", chosen.model.Bytes, room.FreeDisk))
	}
	if len(readings) == 0 {
		return ""
	}
	return m.theme.Readings(readings, modalWidth-4, ui.Measure(readings))
}

// share4Reading is one of those bars: what would be taken, out of what there is.
func share4Reading(label string, need, have int64) ui.Reading {
	share := float64(need) / float64(have)
	severity := ui.SevOK
	switch {
	case share > 1:
		severity = ui.SevCritical
	case share > tightShare:
		severity = ui.SevWarn
	}
	return ui.Reading{
		Label:    label,
		Severity: severity,
		Ratio:    share,
		Measured: true,
		Value:    fmt.Sprintf("%d%%", int(share*100+0.5)),
	}
}

// tightShare is the share of a machine past which a model is not comfortable.
const tightShare = 0.80

// anywayMsg is that question answered yes.
type anywayMsg struct{ id string }

// anyway fetches what the estimate said not to.
func (m Model) anyway(msg anywayMsg) (tea.Model, tea.Cmd) {
	entry, err := local.Offered(msg.id)
	if err != nil {
		return m.troubled4AI(err.Error()), nil
	}
	return m.chose4Model(entry)
}

// asking4Key turns the modal into the one field it needs.
func (m Model) asking4Key(host cli.Hosted) (tea.Model, tea.Cmd) {
	if host.Env == "" {
		return m.add4Host(host, "")
	}
	m.chooser.asking = true
	m.chooser.host = host
	m.chooser.trouble = ""
	m.chooser.field = input(m.theme, "", true)
	return m, m.chooser.field.Focus()
}

func (m Model) typing4Key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.chooser.asking = false
		m.chooser.trouble = ""
		return m, m.chooser.filter.Focus()
	case key.Matches(msg, m.keys.Choose):
		return m.kept4Key()
	}
	updated, cmd := m.chooser.field.Update(msg)
	m.chooser.field = updated
	return m, cmd
}

// kept4Key puts the key in the keychain and writes down where it went, never
// the key itself.
func (m Model) kept4Key() (tea.Model, tea.Cmd) {
	host := m.chooser.host
	given := []byte(strings.TrimSpace(m.chooser.field.Value()))
	if len(given) == 0 {
		m.chooser.trouble = "a key is needed, or esc to go back"
		return m, nil
	}
	reference, err := m.workspace.Setup().StoreKey(cli.InstanceName(host.Kind, ""), given)
	secretref.Zero(given)
	if err != nil {
		m.chooser.trouble = err.Error() + "; export " + host.Env + " instead"
		return m, nil
	}
	return m.add4Host(host, reference.String())
}

// key4View is the modal once it is asking for a key.
func (m Model) key4View(inner int) string {
	host := m.chooser.host
	parts := []string{
		m.theme.Title.Render("a key for " + host.Title),
		m.theme.Muted.Render("it goes in your keychain; settings.toml keeps only a reference"),
		"",
		m.theme.Prompt.Render("› ") + m.chooser.field.View(),
	}
	if m.chooser.trouble != "" {
		parts = append(parts, "", m.theme.Error.Render("✗ "+m.chooser.trouble))
	}
	parts = append(parts, "", m.theme.Hints(
		ui.Hint{Key: "enter", Does: "keep it"},
		ui.Hint{Key: "esc", Does: "back"},
	))
	return m.theme.Panel.Render(square(strings.Join(parts, "\n"), inner))
}

// local4Model is whether a model is the one currently answering, which is what
// decides whether removing its weights has to let go of it first.
func (m Model) local4Model(id string) bool {
	instance, ok := m.local4Instance(id)
	return ok && instance.Name == m.talk.instance
}

// chose4Model uses a model that is here, and fetches one that is not. The
// library comes first, because no model runs without it.
func (m Model) chose4Model(entry local.Entry) (tea.Model, tea.Cmd) {
	store := m.session.AI.Models
	if store != nil && store.Has(entry.ID) {
		return m.add4Model(entry)
	}
	if m.session.AI.Library != nil && !m.session.AI.Library.Present() {
		m.pending = entry.ID
		return m.fetchLibrary()
	}
	m.pending = entry.ID
	return m.fetchEntry(entry)
}

// add4Model uses the instance that already runs this model, and writes one when
// there is none.
func (m Model) add4Model(entry local.Entry) (tea.Model, tea.Cmd) {
	if instance, ok := m.local4Instance(entry.ID); ok {
		return m.activate(instance.Name)
	}
	return m.wrote(config.AIInstance{
		Name:  cli.InstanceName(ai.KindLocal, entry.ID),
		Kind:  string(ai.KindLocal),
		Model: entry.ID,
	})
}

func (m Model) add4Host(host cli.Hosted, reference string) (tea.Model, tea.Cmd) {
	return m.wrote(config.AIInstance{
		Name:  cli.InstanceName(host.Kind, ""),
		Kind:  string(host.Kind),
		Model: host.Model,
		Key:   reference,
	})
}

// wrote saves an instance, makes it the one that answers, and closes the modal.
func (m Model) wrote(instance config.AIInstance) (tea.Model, tea.Cmd) {
	settings, err := cli.AddInstance(m.workspace.Setup().Store, m.session.Settings, instance)
	if err != nil {
		m.ai.trouble = err.Error()
		return m, nil
	}
	return m.answering(settings, instance.Name)
}

func (m Model) activate(name string) (tea.Model, tea.Cmd) {
	settings, err := cli.Activate(m.workspace.Setup().Store, m.session.Settings, name)
	if err != nil {
		m.ai.trouble = err.Error()
		return m, nil
	}
	return m.answering(settings, name)
}

// answering points everything at the instance that was chosen and lets go of the
// one that was open, so the next question goes to the new one.
func (m Model) answering(settings config.Settings, name string) (tea.Model, tea.Cmd) {
	setup := m.workspace.Setup()
	if m.assistant != nil {
		_ = m.assistant.Close()
	}
	m.session.Settings = settings
	m.session.AI = cli.NewAssistant(context.Background(), setup.Store.Paths, settings, setup.Secrets)
	m.build = assistantFor(m.session)
	m.assistant = nil
	m.chooser = nil
	m.talk.instance = name
	m.talk.trouble = ""
	m.talk.loaded = false
	read := m.read4AI()
	return read, tea.Batch(read.talk.prompt.Focus(),
		read.notify(name+" will answer from now on"))
}
