package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/ai/providers/local"
	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

// pane4AI is which of the two lists the keys are talking to.
type pane4AI int

const (
	paneInstances pane4AI = iota
	paneModels
)

type fetchProgressMsg struct {
	progress local.Progress
	token    int
}

type fetchEndedMsg struct {
	err   error
	token int
}

// fetching is a download in flight: what it produces and how it ended.
type fetching struct {
	progress <-chan local.Progress
	failed   <-chan error
}

// model4AI is one row of the catalogue with what is known about it here.
type model4AI struct {
	entry     local.Entry
	installed bool
	verdict   local.Verdict
}

// aiSettings is the screen where instances are chosen and models are fetched.
type aiSettings struct {
	theme     *ui.Theme
	instances []config.AIInstance
	active    string
	models    []model4AI
	library   libraryState
	pane      pane4AI
	at        [2]int
	busy      string
	doing     string
	subject   string
	ollama    string
	progress  local.Progress
	warmed    bool

	// memory is what this machine has to run a model in, as the inference
	// library reports it. It is nought until the library has been asked, and a
	// fit worked out without it is about the disk alone.
	memory int64

	// since and first are what a rate is worked out from: when bytes started
	// arriving and how many were already there. A download resumed at three
	// gigabytes has not just done three gigabytes in no time.
	since time.Time
	first int64

	// job says what is being fetched, because what happens when it lands
	// depends on it and a sentence about it is not a thing to take apart again.
	job     job4AI
	token   int
	note    string
	trouble string
	running fetching
}

// job4AI is what a download is of.
type job4AI int

const (
	jobLibrary job4AI = iota
	jobModel
)

// libraryState is what is known about the inference library.
type libraryState struct {
	present bool
	trouble string
}

func newAISettings(theme *ui.Theme) aiSettings { return aiSettings{theme: theme} }

// read4AI gathers what the screen shows. It is called when the screen is opened
// and after anything that changes what is on disk, because a list built once at
// start would go stale the moment a model finished arriving.
//
// It reports the trouble the session carries, so anything the screen has to say
// about a download of its own is set after it rather than before.
func (m Model) read4AI() Model {
	assistant := m.session.AI
	m.ai.instances = assistant.Settings.Instances
	m.ai.active = assistant.Settings.Active
	m.ai.trouble = assistant.Trouble
	if assistant.Library != nil {
		m.ai.library = libraryState{present: assistant.Library.Present()}
		if !local.Carried() {
			m.ai.library.trouble = local.ErrNoAsset.Error()
		}
	}
	entries, err := local.Catalogue()
	if err != nil {
		m.ai.trouble = err.Error()
		return m
	}
	room := m.room4AI()
	rows := make([]model4AI, 0, len(entries))
	for _, entry := range entries {
		row := model4AI{entry: entry, verdict: local.Fits(entry, m.session.Settings.AI.Context, room)}
		if assistant.Models != nil {
			row.installed = assistant.Models.Has(entry.ID)
		}
		rows = append(rows, row)
	}
	m.ai.models = rows
	return m
}

// room4AI is what this machine has: the disk under the models directory, and
// the memory the inference library reported when it was opened.
func (m Model) room4AI() local.Machine {
	room := local.Room(m.models4AI())
	room.Memory = m.ai.memory
	return room
}

func (m Model) models4AI() string {
	if m.session.AI.Models == nil {
		return "."
	}
	return m.session.AI.Models.Dir()
}

// aiBody draws the screen: what can be talked to, what can be run here, and
// what is missing before either works.
func (m Model) aiBody() string {
	width := ui.FrameWidth(m.width)
	inner := ui.TextWidth(m.width)
	parts := []string{m.theme.Screen("ai", m.ai.tag(), width), ""}
	if m.ai.trouble != "" {
		parts = append(parts, m.theme.Error.Render("✗ "+m.ai.trouble), "")
	}
	parts = append(parts,
		m.theme.Section("instances", "", inner),
		m.instances4AI(inner),
		"",
		m.theme.Section("models", m.library4AI(), inner),
		m.models4AIView(inner),
	)
	if m.ai.busy != "" {
		parts = append(parts, "", m.progress4AI(inner))
	}
	if m.ai.note != "" {
		parts = append(parts, "", m.theme.Muted.Render(m.ai.note))
	}
	return strings.Join(parts, "\n")
}

func (a aiSettings) tag() string {
	if a.active == "" {
		return "nothing configured"
	}
	return a.active
}

func (m Model) library4AI() string {
	switch {
	case m.ai.library.trouble != "":
		return "no build for this machine"
	case m.ai.library.present:
		return "library " + local.Build
	default:
		return "library " + local.Build + " · included"
	}
}

// doing4AI, subject4AI and arrived4AI are what the download panel says: what is
// happening, what it is happening to, and how far it has got.
func (m Model) doing4AI() string { return m.ai.doing }

func (m Model) subject4AI() string { return m.ai.subject }

func (m Model) arrived4AI() string {
	told := m.ai.progress
	said := ui.ByteSize(told.Bytes) + " so far"
	if told.Total > 0 {
		said = fmt.Sprintf("%d%% · %s of %s", int(told.Ratio()*100),
			ui.ByteSize(told.Bytes), ui.ByteSize(told.Total))
	}
	return ui.Dotted(said, m.rate4AI(), m.left4AI())
}

// rate4AI is how fast bytes are arriving, worked out over the whole download
// rather than between the last two reports: reports come every few megabytes,
// so the gap between two of them says more about the buffer than about the
// line.
func (m Model) rate4AI() string {
	if m.ai.first < 0 || m.ai.progress.Bytes <= m.ai.first {
		return ""
	}
	seconds := time.Since(m.ai.since).Seconds()
	if seconds < minRate {
		return ""
	}
	return ui.ByteSize(int64(float64(m.ai.progress.Bytes-m.ai.first)/seconds)) + "/s"
}

// left4AI is how long the rest would take at the rate so far, which is the
// number somebody deciding whether to wait actually wants.
func (m Model) left4AI() string {
	told := m.ai.progress
	if told.Total <= 0 {
		return ""
	}
	rate := m.perSecond()
	if rate <= 0 {
		return ""
	}
	return waiting(time.Duration(float64(told.Total-told.Bytes) / rate * float64(time.Second)))
}

// waiting is how long is left, in whole units. A download has no business
// reporting hundredths of a second, and the number is an estimate off an
// average rate: the decimals would be precision about a guess.
func waiting(left time.Duration) string {
	switch {
	case left >= time.Hour:
		return fmt.Sprintf("%dh %dm left", int(left.Hours()), int(left.Minutes())%60)
	case left >= time.Minute:
		return fmt.Sprintf("%dm %ds left", int(left.Minutes()), int(left.Seconds())%60)
	default:
		return fmt.Sprintf("%ds left", max(int(left.Seconds()), 1))
	}
}

func (m Model) perSecond() float64 {
	if m.ai.first < 0 || m.ai.progress.Bytes <= m.ai.first {
		return 0
	}
	seconds := time.Since(m.ai.since).Seconds()
	if seconds < minRate {
		return 0
	}
	return float64(m.ai.progress.Bytes-m.ai.first) / seconds
}

// minRate is how long a download has to have been going before a rate is worth
// printing. Anything less and the first buffer reads as a gigabit line.
const minRate = 0.5

// progress4AI is a download on the settings screen, drawn with the same bar the
// modal uses and the same bar every reading on the dashboard uses, because a
// second way of drawing a proportion is a second thing to learn to read.
func (m Model) progress4AI(width int) string {
	said := m.spinner.View() + m.theme.Muted.Render(" "+m.ai.busy)
	if m.ai.progress.Bytes == 0 {
		return said
	}
	return said + "\n" + m.theme.Track(m.ai.progress.Ratio(), min(width, gaugeRoom)) +
		"  " + m.theme.Muted.Render(m.arrived4AI())
}

// gaugeRoom is how wide a bar is on a screen that is not a dialog: wide enough
// to read a tenth off, narrow enough to leave the numbers beside it.
const gaugeRoom = 30

func (m Model) instances4AI(width int) string {
	if len(m.ai.instances) == 0 {
		return m.theme.Muted.Render("  none yet; choose something below and it is written here")
	}
	lines := make([]string, 0, len(m.ai.instances))
	for at, instance := range m.ai.instances {
		marker := "  "
		if m.ai.pane == paneInstances && at == m.ai.at[paneInstances] {
			marker = m.theme.Accent.Render("▌ ")
		}
		state := ""
		if instance.Name == m.ai.active {
			state = m.theme.Accent.Render("active")
		}
		lines = append(lines, marker+ui.SplitLine(
			m.theme.Value.Render(ui.Clip(instance.Name, 18))+"  "+
				m.theme.Muted.Render(ui.Clip(instance.Kind, 12))+"  "+
				m.theme.Subtle.Render(ui.Clip(instance.Model, 28)),
			state, width-2))
	}
	return strings.Join(lines, "\n")
}

func (m Model) models4AIView(width int) string {
	lines := make([]string, 0, len(m.ai.models))
	for at, row := range m.ai.models {
		marker := "  "
		if m.ai.pane == paneModels && at == m.ai.at[paneModels] {
			marker = m.theme.Accent.Render("▌ ")
		}
		lines = append(lines, marker+ui.SplitLine(
			m.theme.Value.Render(ui.Clip(row.entry.Title, 20))+"  "+
				m.theme.Muted.Render(ui.Clip(sizeOf(row.entry.Bytes), 9))+"  "+
				m.theme.Subtle.Render(ui.Clip(row.entry.Licence, 12)),
			m.state4Model(row), width-2))
	}
	return strings.Join(lines, "\n")
}

// state4Model is the right hand side of a row on this screen. It says nothing
// about a model that fits and is not here, because that is every model until
// somebody fetches one, and a column of the word "fits" is a column nobody
// reads. This screen has the room to spell the rest out, which is the
// difference between it and the modal.
func (m Model) state4Model(row model4AI) string {
	switch {
	case row.installed:
		return m.theme.Severity(ui.SevOK).Render("✓ downloaded")
	case !row.verdict.Fits:
		return m.theme.Subtle.Render("too big for this machine")
	case !row.verdict.Comfortable:
		return m.theme.Severity(ui.SevWarn).Render("tight")
	default:
		return ""
	}
}

func sizeOf(bytes int64) string {
	return fmt.Sprintf("%.1f GiB", float64(bytes)/float64(1<<30))
}

// aiKey is what the screen does with a key.
func (m Model) aiKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		if m.ai.busy != "" {
			return m.stopFetching(), nil
		}
		return m.show(viewDashboard)
	case key.Matches(msg, m.keys.Focus):
		m.ai.pane = 1 - m.ai.pane
		return m, nil
	case key.Matches(msg, m.keys.Up):
		return m.walk4AI(-1), nil
	case key.Matches(msg, m.keys.Down):
		return m.walk4AI(1), nil
	case key.Matches(msg, m.keys.Choose):
		return m.chose4AI()
	case key.Matches(msg, m.keys.Remove):
		return m.remove4AI()
	case m.keys.opensPalette(msg, false):
		return m.openPalette()
	}
	return m.scroll(msg), nil
}

func (m Model) walk4AI(step int) Model {
	rows := len(m.ai.instances)
	if m.ai.pane == paneModels {
		rows = len(m.ai.models)
	}
	if rows == 0 {
		return m
	}
	at := m.ai.at[m.ai.pane] + step
	m.ai.at[m.ai.pane] = min(max(at, 0), rows-1)
	return m
}

// chose4AI is what enter does: switch to an instance, or fetch what a model
// needs to run.
func (m Model) chose4AI() (tea.Model, tea.Cmd) {
	if m.ai.busy != "" {
		return m, nil
	}
	if m.ai.pane == paneInstances {
		return m.activate4AI()
	}
	if !m.ai.library.present {
		return m.fetchLibrary()
	}
	return m.fetchModel()
}

// activate4AI switches to the instance under the cursor. The writing down and
// the letting go of the open conversation are the same on this screen as they
// are in the modal, so they are done in one place.
func (m Model) activate4AI() (tea.Model, tea.Cmd) {
	if len(m.ai.instances) == 0 {
		return m, nil
	}
	return m.activate(m.ai.instances[m.ai.at[paneInstances]].Name)
}

func (m Model) remove4AI() (tea.Model, tea.Cmd) {
	if m.ai.pane != paneModels || m.ai.busy != "" || len(m.ai.models) == 0 {
		return m, nil
	}
	row := m.ai.models[m.ai.at[paneModels]]
	if !row.installed || m.session.AI.Models == nil {
		return m, nil
	}
	if err := m.session.AI.Models.Remove(row.entry.ID); err != nil {
		m.ai.trouble = err.Error()
		return m, nil
	}
	return m.read4AI(), m.notify(row.entry.Title + " was removed")
}

// fetchLibrary gets what every local model needs before any of them can run.
func (m Model) fetchLibrary() (tea.Model, tea.Cmd) {
	if m.session.AI.Library == nil || m.ai.library.trouble != "" {
		return m, nil
	}
	library := m.session.AI.Library
	return m.started4AI(jobLibrary, "unpacking", "llama.cpp "+local.Build,
		func(ctx context.Context, out chan<- local.Progress) error {
			return library.Install(ctx, out)
		})
}

// fetchModel downloads the weights the cursor is on.
func (m Model) fetchModel() (tea.Model, tea.Cmd) {
	if len(m.ai.models) == 0 || m.session.AI.Models == nil {
		return m, nil
	}
	row := m.ai.models[m.ai.at[paneModels]]
	if row.installed {
		m.ai.note = row.entry.Title + " is already here; d removes it"
		return m, nil
	}
	if !row.verdict.Fits {
		m.ai.note = row.verdict.Reason
		return m, nil
	}
	return m.fetchEntry(row.entry)
}

// fetchEntry downloads one model. It takes the entry rather than reading a
// cursor, so the screen and the modal can both ask for the same thing.
func (m Model) fetchEntry(entry local.Entry) (tea.Model, tea.Cmd) {
	if m.session.AI.Models == nil {
		return m, nil
	}
	fetcher := local.NewFetcher(m.session.AI.Models, string(m.session.AI.Token))
	return m.started4AI(jobModel, "fetching", entry.Title,
		func(ctx context.Context, out chan<- local.Progress) error {
			return fetcher.Fetch(ctx, entry, out)
		})
}

// warmedMsg is the inference library having been put where it can be opened,
// and what it then said this machine has to run a model in.
type warmedMsg struct {
	err    error
	memory int64
}

// warming writes the inference library to disk the first time the conversation
// is opened.
//
// It is not done when the program starts: somebody who never asks a question
// should not pay for it, and the bytes are twenty megabytes of copying. It is
// not left until a model is chosen either, because by then somebody is waiting,
// and this can happen while they are still reading the list.
func (m Model) warming() (Model, tea.Cmd) {
	assistant := m.session.AI
	if assistant.Library == nil || m.ai.warmed {
		return m, nil
	}
	if !assistant.Library.Present() && !local.Carried() {
		return m, nil
	}
	m.ai.warmed = true
	return m, func() tea.Msg {
		if !assistant.Library.Present() {
			if err := assistant.Library.Install(context.Background(), nil); err != nil {
				return warmedMsg{err: err}
			}
		}
		return warmedMsg{memory: assistant.Memory()}
	}
}

// warmed takes the library having arrived, or not. A failure is written down
// rather than raised: nothing was asked for yet, and every hosted back-end
// works without it.
func (m Model) warmed(msg warmedMsg) (tea.Model, tea.Cmd) {
	m.ai.memory = msg.memory
	read := m.read4AI()
	if msg.err != nil {
		read.ai.library.trouble = msg.err.Error()
	}
	if read.chooser != nil {
		read.chooser.offers = read.offers()
	}
	return read, nil
}

// started4AI runs a download and returns the command that watches it. What is
// happening is kept as a verb and a subject rather than as one sentence,
// because the modal draws them on separate lines and taking a sentence apart
// again to do it would be a sentence pretending to be two fields.
func (m Model) started4AI(what job4AI, doing, subject string,
	run func(context.Context, chan<- local.Progress) error,
) (tea.Model, tea.Cmd) {
	ctx, stop := context.WithCancel(context.Background())
	progress := make(chan local.Progress)
	failed := make(chan error, 1)
	go func() {
		defer close(progress)
		failed <- run(ctx, progress)
	}()
	m.ai.token++
	m.ai.busy = doing + " " + subject
	m.ai.doing, m.ai.subject, m.ai.job = doing, subject, what
	m.ai.since, m.ai.first = time.Now(), -1
	m.ai.note, m.ai.trouble = "", ""
	m.ai.progress = local.Progress{}
	m.ai.running = fetching{progress: progress, failed: failed}
	m.stopFetch = stop
	return m, tea.Batch(watchFetch(m.ai.running, m.ai.token), m.spinner.Tick)
}

func watchFetch(running fetching, token int) tea.Cmd {
	return func() tea.Msg {
		told, open := <-running.progress
		if !open {
			return fetchEndedMsg{err: <-running.failed, token: token}
		}
		return fetchProgressMsg{progress: told, token: token}
	}
}

// fetched takes one report of how far a download has got.
func (m Model) fetched(msg fetchProgressMsg) (tea.Model, tea.Cmd) {
	if msg.token != m.ai.token {
		return m, nil
	}
	if m.ai.first < 0 {
		m.ai.first, m.ai.since = msg.progress.Bytes, time.Now()
	}
	m.ai.progress = msg.progress
	return m, watchFetch(m.ai.running, msg.token)
}

// doneFetching closes a download off and reads the disk again, because what is
// on it has changed.
func (m Model) doneFetching(msg fetchEndedMsg) (tea.Model, tea.Cmd) {
	if msg.token != m.ai.token {
		return m, nil
	}
	what := m.ai.subject
	m.ai.busy = ""
	m.stopFetch = nil
	if m.chooser != nil {
		m.chooser.giving = false
	}
	if msg.err != nil {
		read := m.read4AI()
		read.pending = ""
		read.ai.trouble = msg.err.Error()
		if read.chooser != nil {
			read.chooser.trouble = msg.err.Error()
		}
		return read, nil
	}
	m.ai.note = what + " is here"
	if m.pending != "" {
		return m.continued()
	}
	return m.read4AI(), m.notify("done")
}

// continued carries on with what somebody asked for. Choosing a model that is
// not here fetches the library first and the weights second, and the point of
// the whole errand is a working assistant, not a full disk: when the last piece
// lands the instance is written and made the one that answers.
//
// The weights are fetched once. A download that reported no failure and left
// nothing the store will admit to is a fault worth saying out loud, and asking
// for it again would be a loop that downloads gigabytes for ever without ever
// saying why.
//
// It ends in the conversation rather than in the list it was started from: what
// was wanted was something to ask, and by the time that screen is drawn the
// model is already being read into memory.
func (m Model) continued() (tea.Model, tea.Cmd) {
	wanted := m.pending
	entry, err := local.Offered(wanted)
	if err != nil {
		m.pending = ""
		return m.troubled4AI(err.Error()), nil
	}
	if m.session.AI.Models != nil && m.session.AI.Models.Has(wanted) {
		m.pending = ""
		m.view, m.offset, m.onSessions = viewAsk, 0, false
		return m.add4Model(entry)
	}
	if m.ai.job == jobModel {
		m.pending = ""
		return m.troubled4AI(entry.Title + " finished downloading and the store will not have it: " +
			reason4Model(m.session.AI.Models, wanted)), nil
	}
	return m.fetchEntry(entry)
}

// reason4Model asks the store why it will not admit to a model, so that what
// somebody reads is the fault itself rather than that there was one.
func reason4Model(store *local.Store, id string) string {
	if store == nil {
		return "there is nowhere on this machine to keep models"
	}
	if _, err := store.Find(id); err != nil {
		return err.Error()
	}
	return "it is there now"
}

// troubled4AI puts a failure where it will be read. The modal is what somebody
// is looking at when a download ends, so a screen behind it is the wrong place
// to leave the only account of what went wrong.
func (m Model) troubled4AI(what string) Model {
	read := m.read4AI()
	read.ai.trouble = what
	if read.chooser != nil {
		read.chooser.trouble = what
	}
	return read
}

// stopFetching gives up on a download. What arrived is left where it is, so
// that pressing enter again continues rather than starts over.
func (m Model) stopFetching() Model {
	if m.stopFetch != nil {
		m.stopFetch()
	}
	return m
}
