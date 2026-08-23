package app

import (
	"context"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/tui4db/src/cli/internal/ai/providers/local"
	"github.com/sonquer/tui4db/src/cli/internal/cli"
	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

func opened4Chooser(t *testing.T) Model {
	t.Helper()
	m := configured(t)
	shown, _ := m.show(viewAsk)
	chosen, _ := shown.(Model).choosing()
	if chosen.chooser == nil {
		t.Fatal("the modal did not open")
	}
	return chosen
}

func drawn(t *testing.T, m Model) string {
	t.Helper()
	return plain(m.chooserView(m.width, m.height))
}

func TestChooserOpensOnWhatAnswers(t *testing.T) {
	m := opened4Chooser(t)
	chosen, ok := m.chooser.selected()
	if !ok {
		t.Fatal("nothing is under the cursor")
	}
	if chosen.key != "claude" {
		t.Fatalf("the cursor is on %q, want the instance that is answering", chosen.key)
	}
	if !chosen.current {
		t.Fatal("the row that is answering is not marked")
	}
}

func TestChooserGroupsAndPutsLocalFirst(t *testing.T) {
	m := opened4Chooser(t)
	order := []string{}
	for _, item := range m.chooser.offers {
		if len(order) == 0 || order[len(order)-1] != item.section {
			order = append(order, item.section)
		}
	}
	if order[0] != "on this machine" {
		t.Fatalf("groups are %v, want the local one first", order)
	}
	if len(order) < 5 {
		t.Fatalf("groups are %v, want one per provider", order)
	}
}

func TestChooserSearchFlattensAndKeepsTheProvider(t *testing.T) {
	m := opened4Chooser(t)
	m = typeInto(t, m, "gemma")

	found := m.chooser.found()
	if len(found) == 0 {
		t.Fatal("searching for a model found nothing")
	}
	for _, item := range found {
		if item.section != "" {
			t.Fatalf("row %q still has a heading over it while searching", item.label)
		}
		if !strings.Contains(strings.ToLower(item.label), "gemma") {
			t.Fatalf("row %q does not match what was typed", item.label)
		}
		if item.note == "" {
			t.Fatalf("row %q lost the group it belongs to", item.label)
		}
	}

	m = typeInto(t, m, "zzz")
	if len(m.chooser.found()) != 0 {
		t.Fatal("searching for nonsense found something")
	}
	if !strings.Contains(drawn(t, m), "nothing matches") {
		t.Fatalf("the modal says nothing about an empty result:\n%s", drawn(t, m))
	}
}

func TestChooserSearchFindsAProviderByName(t *testing.T) {
	m := opened4Chooser(t)
	m = typeInto(t, m, "openai")
	found := m.chooser.found()
	if len(found) == 0 {
		t.Fatal("searching for a provider by name found nothing")
	}
}

func TestChooserWalksAndWraps(t *testing.T) {
	m := opened4Chooser(t)
	m.chooser.cursor = 0
	up, _ := press(t, m, "up")
	last := len(up.chooser.found()) - 1
	if up.chooser.cursor != last {
		t.Fatalf("cursor = %d, want it wrapped to %d", up.chooser.cursor, last)
	}
	down, _ := press(t, up, "down")
	if down.chooser.cursor != 0 {
		t.Fatalf("cursor = %d, want it wrapped back", down.chooser.cursor)
	}
}

func TestChooserKeepsTheCursorInView(t *testing.T) {
	m := opened4Chooser(t)
	found := m.chooser.found()
	rows := 3
	m.chooser.cursor = len(found) - 1
	shown := m.chooser.window(found, rows)
	if len(shown) != rows {
		t.Fatalf("showed %d rows, want %d", len(shown), rows)
	}
	if shown[len(shown)-1].key != found[len(found)-1].key {
		t.Fatal("the cursor walked off the bottom of the window")
	}
	m.chooser.cursor = 0
	shown = m.chooser.window(found, rows)
	if shown[0].key != found[0].key {
		t.Fatal("the cursor walked off the top of the window")
	}
}

// install puts weights on disk the way a finished download leaves them.
func install4Model(t *testing.T, m Model, id string) local.Entry {
	t.Helper()
	entry, err := local.Offered(id)
	if err != nil {
		t.Fatal(err)
	}
	store := m.session.AI.Models
	if err := store.Write(local.Manifest{ID: entry.ID, File: entry.File, Repo: entry.Repo, Revision: entry.Revision}); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(t, store.Dir()+"/"+entry.ID+"/"+entry.File); err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestChooserSwitchesInstance(t *testing.T) {
	m := opened4Chooser(t)
	entry := install4Model(t, m, "gemma-4-e4b-qat")
	m.chooser.offers = m.offers()
	m.chooser = m.chooser.at(entry.ID)
	chosen, _ := press(t, m, "enter")

	if chosen.chooser != nil {
		t.Fatal("the modal stayed open after a choice")
	}
	if chosen.talk.instance != "here" {
		t.Fatalf("the conversation names %q, want the instance that already runs those weights", chosen.talk.instance)
	}
	if chosen.assistant != nil {
		t.Fatal("the assistant that was open was not let go of")
	}
	written, err := chosen.workspace.Setup().Store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings() error = %v", err)
	}
	if written.AI.Active != "here" {
		t.Fatalf("settings.toml says %q, want the choice written down", written.AI.Active)
	}
	if len(written.AI.Instances) != 2 {
		t.Fatalf("instances = %+v, want no second name for the same weights", written.AI.Instances)
	}
}

func TestChooserWritesAnInstanceForAModelNobodyConfigured(t *testing.T) {
	m := opened4Chooser(t)
	m.session.Settings.AI.Instances = []config.AIInstance{
		{Name: "claude", Kind: "anthropic", Model: "claude-sonnet-5"},
	}
	entry := install4Model(t, m, "gemma-4-e2b-qat")
	m.chooser.offers = m.offers()
	m.chooser = m.chooser.at(entry.ID)

	chosen, _ := press(t, m, "enter")
	if chosen.chooser != nil {
		t.Fatal("the modal stayed open")
	}
	written, err := chosen.workspace.Setup().Store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	instance, ok := written.AI.Instance(entry.ID)
	if !ok {
		t.Fatalf("no instance was written for the model: %+v", written.AI.Instances)
	}
	if instance.Kind != "local" || instance.Model != entry.ID {
		t.Fatalf("instance = %+v", instance)
	}
	if written.AI.Active != entry.ID || !written.AI.Enabled {
		t.Fatalf("the model is here and is not answering: %+v", written.AI)
	}
}

func TestChooserFetchesTheLibraryBeforeTheWeights(t *testing.T) {
	m := opened4Chooser(t)
	m.session.Settings.AI.Instances = nil
	m.chooser.offers = m.offers()
	m.chooser = m.chooser.at("gemma-4-e4b-qat")
	started, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatal("nothing was started")
	}
	if started.pending != "gemma-4-e4b-qat" {
		t.Fatalf("pending = %q, want the model kept for after the library", started.pending)
	}
	if !strings.Contains(started.ai.busy, local.Build) {
		t.Fatalf("busy = %q, want the library first", started.ai.busy)
	}
	settled(t, started)
}

func TestChooserRemovesAModel(t *testing.T) {
	m := opened4Chooser(t)
	entry := install4Model(t, m, "gemma-4-e2b-qat")
	store := m.session.AI.Models
	m.chooser.offers = m.offers()
	m.chooser = m.chooser.at(entry.ID)

	asked, _ := press(t, m, "d")
	if asked.modal == nil {
		t.Fatal("gigabytes were deleted without a question")
	}
	if !strings.Contains(plain(asked.modal.view(80)), "remove "+entry.Title+"?") {
		t.Fatalf("the question is not about the model:\n%s", plain(asked.modal.view(80)))
	}
	if !store.Has(entry.ID) {
		t.Fatal("the weights went before the question was answered")
	}
	said, cmd := press(t, asked, "enter")
	if cmd == nil {
		t.Fatal("saying yes did nothing")
	}
	removed, _ := said.Update(cmd())
	if store.Has(entry.ID) {
		t.Fatal("the weights are still there")
	}
	if removed.(Model).chooser == nil {
		t.Fatal("removing closed the modal")
	}
	if removed.(Model).modal != nil {
		t.Fatal("the question is still up")
	}
}

// TestARemovalCanBeSaidNoTo is the other half of asking: the question has to be
// refusable, and refusing has to leave the weights alone.
func TestARemovalCanBeSaidNoTo(t *testing.T) {
	m := opened4Chooser(t)
	entry := install4Model(t, m, "gemma-4-e2b-qat")
	m.chooser.offers = m.offers()
	m.chooser = m.chooser.at(entry.ID)
	asked, _ := press(t, m, "d")
	kept, _ := press(t, asked, "esc")
	if kept.modal != nil {
		t.Fatal("esc left the question up")
	}
	if !m.session.AI.Models.Has(entry.ID) {
		t.Fatal("saying no removed them anyway")
	}
	if kept.chooser == nil {
		t.Fatal("the list went with the question")
	}
}

// TestRemovingIsOfferedOnlyWhereThereIsSomethingToRemove keeps the footer
// honest: d is on the row that has weights, and nowhere else.
func TestRemovingIsOfferedOnlyWhereThereIsSomethingToRemove(t *testing.T) {
	m := opened4Chooser(t)
	entry := install4Model(t, m, "gemma-4-e2b-qat")
	m.chooser.offers = m.offers()
	m.chooser = m.chooser.at(entry.ID)
	if !strings.Contains(plain(m.chooser4Foot()), "remove") {
		t.Fatalf("the footer does not offer to remove weights that are here: %q", plain(m.chooser4Foot()))
	}
	m.chooser = m.chooser.at("gemma-4-12b-qat")
	if strings.Contains(plain(m.chooser4Foot()), "remove") {
		t.Fatalf("the footer offers to remove a model nobody fetched: %q", plain(m.chooser4Foot()))
	}
	m.chooser = m.chooser.at("claude")
	if strings.Contains(plain(m.chooser4Foot()), "remove") {
		t.Fatalf("the footer offers to remove an instance: %q", plain(m.chooser4Foot()))
	}
}

func TestChooserRemovesNothingThatIsNotThere(t *testing.T) {
	m := opened4Chooser(t)
	m.chooser = m.chooser.at("gemma-4-12b-qat")
	unchanged, _ := press(t, m, "d")
	if unchanged.chooser.trouble != "" {
		t.Fatalf("trouble = %q, want removing nothing to be quiet", unchanged.chooser.trouble)
	}
	m.chooser = m.chooser.at("claude")
	instance, _ := press(t, m, "d")
	if instance.chooser.trouble != "" {
		t.Fatal("d on an instance tried to remove weights")
	}
}

func TestChooserTakesAKey(t *testing.T) {
	m := opened4Chooser(t)
	m.chooser = m.chooser.at("openai:add")
	asked, _ := press(t, m, "enter")
	if !asked.chooser.asking {
		t.Fatal("choosing a provider did not ask for a key")
	}
	shown := drawn(t, asked)
	if !strings.Contains(shown, "a key for OpenAI") {
		t.Fatalf("the modal does not say what it wants:\n%s", shown)
	}
	if !strings.Contains(shown, "keychain") {
		t.Fatalf("the modal does not say where the key goes:\n%s", shown)
	}

	empty, _ := press(t, asked, "enter")
	if !strings.Contains(empty.chooser.trouble, "a key is needed") {
		t.Fatalf("trouble = %q, want it to refuse an empty key", empty.chooser.trouble)
	}

	back, _ := press(t, empty, "esc")
	if back.chooser.asking {
		t.Fatal("esc did not go back to the list")
	}
	if back.chooser == nil {
		t.Fatal("esc closed the whole modal rather than the field")
	}
}

func TestChooserMasksTheKey(t *testing.T) {
	m := opened4Chooser(t)
	m.chooser = m.chooser.at("openai:add")
	asked, _ := press(t, m, "enter")
	typed := typeInto(t, asked, "sk")
	shown := drawn(t, typed)
	if strings.Contains(shown, "sk") {
		t.Fatalf("the key is on screen in the clear:\n%s", shown)
	}
	if !strings.Contains(shown, "••") {
		t.Fatalf("the key is not masked:\n%s", shown)
	}
}

func TestChooserOffersAKeyThatIsAlreadyInTheEnvironment(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "a-key-somebody-exported")
	m := opened4Chooser(t)
	m.chooser.offers = m.offers()

	var found *offer
	for i, item := range m.chooser.offers {
		if item.deed == useEnvironment {
			found = &m.chooser.offers[i]
		}
	}
	if found == nil {
		t.Fatal("a key that is already exported was not offered")
	}
	if found.env != "env:GEMINI_API_KEY" {
		t.Fatalf("the row would store %q, want a reference to the variable", found.env)
	}

	m.chooser = m.chooser.at(found.key)
	chosen, _ := press(t, m, "enter")
	written, err := chosen.workspace.Setup().Store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	instance, ok := written.AI.Instance("gemini")
	if !ok {
		t.Fatalf("no instance was written: %+v", written.AI.Instances)
	}
	if instance.Key != "env:GEMINI_API_KEY" {
		t.Fatalf("key = %q, want the reference and never the key", instance.Key)
	}
}

func TestChooserCloses(t *testing.T) {
	m := opened4Chooser(t)
	closed, _ := press(t, m, "esc")
	if closed.chooser != nil {
		t.Fatal("esc did not close the modal")
	}
}

func TestHostsAndTheirEnvironment(t *testing.T) {
	hosts := cli.Hosts()
	if len(hosts) != 4 {
		t.Fatalf("hosts = %+v, want the four that are reached over a network", hosts)
	}
	seen := map[string]bool{}
	for _, host := range hosts {
		if host.Title == "" {
			t.Fatalf("a host has no name: %+v", host)
		}
		if seen[string(host.Kind)] {
			t.Fatalf("two hosts are both %q", host.Kind)
		}
		seen[string(host.Kind)] = true
	}
	if _, ok := cli.FromEnvironment(hosts[0], func(string) string { return "" }); ok {
		t.Fatal("a variable that is not set was offered")
	}
	reference, ok := cli.FromEnvironment(hosts[0], func(string) string { return "a-key" })
	if !ok || reference != "env:"+hosts[0].Env {
		t.Fatalf("FromEnvironment() = %q, %v", reference, ok)
	}
	if _, ok := cli.FromEnvironment(cli.Hosted{}, func(string) string { return "a-key" }); ok {
		t.Fatal("a host with no variable was offered one")
	}
}

func TestInstanceName(t *testing.T) {
	if got := cli.InstanceName("local", "gemma-4-e4b-qat"); got != "gemma-4-e4b-qat" {
		t.Fatalf("InstanceName() = %q, want the model's own name", got)
	}
	if got := cli.InstanceName("anthropic", ""); got != "anthropic" {
		t.Fatalf("InstanceName() = %q", got)
	}
}

func TestActivateRefusesWhatIsNotConfigured(t *testing.T) {
	m := opened4Chooser(t)
	broken, _ := m.activate("nobody")
	if broken.(Model).ai.trouble == "" {
		t.Fatal("activating something that is not configured said nothing")
	}
	_, err := cli.AddInstance(m.workspace.Setup().Store, config.DefaultSettings(), config.AIInstance{})
	if err == nil {
		t.Fatal("an instance with no name was written")
	}
}

func TestChooserLearnsAboutOllamaWithoutWaiting(t *testing.T) {
	m := opened4Chooser(t)
	if m.ai.ollama != "" {
		t.Fatal("the modal decided what the daemon was doing before it had asked")
	}
	if !hinted(m, "ollama", "a daemon you run yourself") {
		t.Fatal("the list says nothing about ollama before the answer comes back")
	}

	answered, _ := m.Update(ollamaMsg{running: true})
	running := answered.(Model)
	if running.ai.ollama != "running here" {
		t.Fatalf("ollama = %q", running.ai.ollama)
	}
	if !hinted(running, "ollama", "running here") {
		t.Fatal("the answer did not reach the list")
	}

	answered, _ = running.Update(ollamaMsg{})
	quiet := answered.(Model)
	if !hinted(quiet, "ollama", "not running here") {
		t.Fatal("a daemon that did not answer is not said to be absent")
	}
}

func hinted(m Model, section, want string) bool {
	for _, item := range m.chooser.offers {
		if item.section == section && item.hint == want {
			return true
		}
	}
	return false
}

func TestOllamaAnswerWithNoModalOpen(t *testing.T) {
	m := configured(t)
	if _, cmd := m.Update(ollamaMsg{running: true}); cmd != nil {
		t.Fatal("an answer that arrived after the modal closed did something")
	}
}

func TestChooserRefusesAKeyItCannotKeep(t *testing.T) {
	m := opened4Chooser(t)
	m.chooser = m.chooser.at("openai:add")
	asked, _ := press(t, m, "enter")
	asked.chooser.field.SetValue("sk-a-real-key")

	setup := asked.workspace.Setup()
	setup.Secrets = nil
	asked.workspace.(*fakeWorkspace).setup = setup

	refused, _ := press(t, asked, "enter")
	if !strings.Contains(refused.chooser.trouble, "nowhere to keep") {
		t.Fatalf("trouble = %q, want it to say the key could not be kept", refused.chooser.trouble)
	}
	if !strings.Contains(refused.chooser.trouble, "OPENAI_API_KEY") {
		t.Fatalf("trouble = %q, want it to name the way that works everywhere", refused.chooser.trouble)
	}
	written, err := refused.workspace.Setup().Store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := written.AI.Instance("openai"); ok {
		t.Fatal("an instance was written for a key that was never kept")
	}
}

func TestChooserShowsHowFarADownloadHasGot(t *testing.T) {
	m := opened4Chooser(t)
	m.ai.busy = "fetching Gemma 4 E4B"
	m.ai.progress = local.Progress{Bytes: 1 << 20, Total: 4 << 20}
	if !strings.Contains(drawn(t, m), "25%") {
		t.Fatalf("the modal does not say how far it has got:\n%s", drawn(t, m))
	}
	unchanged, cmd := press(t, m, "enter")
	if cmd != nil {
		t.Fatal("a second download was started while one was running")
	}
	if unchanged.chooser == nil {
		t.Fatal("the modal closed while a download was running")
	}
}

func TestChooserWithNothingUnderTheCursor(t *testing.T) {
	m := opened4Chooser(t)
	m = typeInto(t, m, "zzz")
	if _, ok := m.chooser.selected(); ok {
		t.Fatal("something was selected out of an empty list")
	}
	unchanged, cmd := press(t, m, "enter")
	if cmd != nil || unchanged.chooser == nil {
		t.Fatal("enter on an empty list did something")
	}
	if m.chooser.move(1).cursor != 0 {
		t.Fatal("the cursor moved in an empty list")
	}
}

func TestChooserSaysWhatAModelWouldCost(t *testing.T) {
	m := opened4Chooser(t)
	shown := drawn(t, m)
	if !strings.Contains(shown, "Apache-2.0") {
		t.Fatalf("the list does not say what a model may be used for:\n%s", shown)
	}
	if !strings.Contains(shown, "GiB") {
		t.Fatalf("the list does not say what a model would cost to fetch:\n%s", shown)
	}
	if strings.Contains(shown, "fits") {
		t.Fatalf("every row says the same word about itself, which is a column nobody reads:\n%s", shown)
	}
	note := func(entry local.Entry, machine local.Machine, here bool) string {
		return note4Model(entry, local.Fits(entry, 8192, machine), here)
	}
	tight := note(local.Entry{Bytes: 4 << 30, Licence: "MIT"}, local.Machine{Memory: 6 << 30, FreeDisk: 200 << 30}, false)
	if !strings.Contains(tight, "tight") {
		t.Fatalf("note = %q, want a model that only just fits to say so", tight)
	}
	wont := note(local.Entry{Bytes: 40 << 30, Licence: "MIT"}, local.Machine{Memory: 2 << 30, FreeDisk: 200 << 30}, false)
	if wont != "40.0 GiB · MIT" {
		t.Fatalf("note = %q, want the row greyed rather than told off", wont)
	}
	roomy := note(local.Entry{Bytes: 1 << 30, Licence: "MIT"}, local.Machine{Memory: 64 << 30, FreeDisk: 200 << 30}, false)
	if roomy != "1.0 GiB · MIT" {
		t.Fatalf("note = %q, want the size and the licence and nothing else", roomy)
	}
	if here := note(local.Entry{Bytes: 1 << 30, Licence: "MIT"}, local.Machine{}, true); here != roomy {
		t.Fatalf("note = %q, want a model that is here to be marked rather than described", here)
	}
}

// TestAModelThatIsHereIsMarked is what replaced the words: three states, three
// glyphs, in the margin where a list already says which row is which.
func TestAModelThatIsHereIsMarked(t *testing.T) {
	m := opened4Chooser(t)
	entry := install4Model(t, m, "gemma-4-e2b-qat")
	m.session.Settings.AI.Active = "here"
	m.chooser.offers = m.offers()
	marks := map[string]string{}
	for _, item := range m.chooser.offers {
		if item.deed == useModel {
			marks[item.key] = item.mark
		}
	}
	if marks[entry.ID] != "✓" {
		t.Fatalf("a model that is downloaded is marked %q", marks[entry.ID])
	}
	if marks["gemma-4-e4b-qat"] != "●" {
		t.Fatalf("the model that is answering is marked %q", marks["gemma-4-e4b-qat"])
	}
	if marks["gpt-oss-120b"] != "" {
		t.Fatalf("a model nobody has fetched is marked %q", marks["gpt-oss-120b"])
	}
}

// held4Chooser opens the modal on a download that has started and is not going
// to finish until the test says so.
func held4Chooser(t *testing.T) (Model, chan struct{}, tea.Cmd) {
	t.Helper()
	m := opened4Chooser(t)
	m.width, m.height = 100, 32
	held := make(chan struct{})
	started, cmd := m.started4AI(jobModel, "fetching", "Gemma 4 E4B", func(ctx context.Context, out chan<- local.Progress) error {
		select {
		case out <- local.Progress{Bytes: 1 << 20, Total: 4 << 20}:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-held:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	running := started.(Model)
	told, ok := runFirst(t, cmd).(fetchProgressMsg)
	if !ok {
		t.Fatal("the download reported nothing")
	}
	moved, _ := running.fetched(told)
	return moved.(Model), held, cmd
}

// TestTheChooserHidesTheListWhileSomethingArrives is the whole point of the
// panel: one thing is happening, nothing on the list can be started until it
// finishes, and a catalogue under a download invites a second one.
func TestTheChooserHidesTheListWhileSomethingArrives(t *testing.T) {
	m, held, _ := held4Chooser(t)
	defer close(held)
	view := drawn(t, m)
	for _, want := range []string{"fetching", "Gemma 4 E4B", "25%", "1.0 MiB of 4.0 MiB"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the panel does not say %q:\n%s", want, view)
		}
	}
	for _, gone := range []string{"ON THIS MACHINE", "Gemma 4 E2B", "add a key"} {
		if strings.Contains(view, gone) {
			t.Fatalf("the list is still drawn under the download, at %q:\n%s", gone, view)
		}
	}
	bar := ui.BarStyleNamed(ui.DefaultBarStyle)
	if !strings.Contains(view, bar.Full) || !strings.Contains(view, bar.Empty) {
		t.Fatalf("how far it has got is a number and no bar:\n%s", view)
	}
}

func TestADownloadWithNoTotalStillSaysWhatArrived(t *testing.T) {
	m, held, _ := held4Chooser(t)
	defer close(held)
	m.ai.progress = local.Progress{Bytes: 3 << 20}
	view := drawn(t, m)
	if !strings.Contains(view, "3.0 MiB so far") {
		t.Fatalf("the panel does not say what arrived:\n%s", view)
	}
	if strings.Contains(view, "%") {
		t.Fatalf("a share was worked out of a total nobody reported:\n%s", view)
	}
}

// TestLeavingADownloadAsksFirst is the reason the question exists: gigabytes
// over a slow line are worth one keypress, and walking out of the modal is the
// one thing that would otherwise throw them away without saying so.
func TestLeavingADownloadAsksFirst(t *testing.T) {
	m, held, _ := held4Chooser(t)
	defer close(held)
	asked, _ := press(t, m, "esc")
	if !asked.chooser.giving {
		t.Fatal("esc walked out of a running download")
	}
	view := drawn(t, asked)
	for _, want := range []string{"stop the download?", "Gemma 4 E4B", "carries on from where it stopped"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the question does not say %q:\n%s", want, view)
		}
	}
	kept, _ := press(t, asked, "esc")
	if kept.chooser.giving || kept.ai.busy == "" {
		t.Fatal("saying no to the question stopped the download anyway")
	}
	if strings.Contains(drawn(t, kept), "stop the download?") {
		t.Fatal("the question is still on screen after it was answered")
	}
}

func TestSayingYesStopsTheDownload(t *testing.T) {
	m, held, cmd := held4Chooser(t)
	defer close(held)
	asked, _ := press(t, m, "esc")
	stopped, _ := press(t, asked, "enter")
	if stopped.chooser.giving {
		t.Fatal("the question is still open")
	}
	if stopped.pending != "" {
		t.Fatal("something is still waiting to happen after the download was given up")
	}
	done := converse(t, stopped, cmd)
	if done.ai.busy != "" {
		t.Fatalf("busy = %q, want the download over", done.ai.busy)
	}
}

func TestNothingElseAnswersWhileADownloadRuns(t *testing.T) {
	m, held, _ := held4Chooser(t)
	defer close(held)
	for _, pressed := range []string{"down", "d", "g"} {
		after, _ := press(t, m, pressed)
		if after.chooser.cursor != m.chooser.cursor || after.chooser.typing() {
			t.Fatalf("%q reached the list under the download", pressed)
		}
	}
}

// TestADownloadThatFinishesUnderTheQuestionTakesItWithIt matters because the
// question is about a download: one that has ended has nothing left to say no
// to, and a question left standing would be answered about the next one.
func TestADownloadThatFinishesUnderTheQuestionTakesItWithIt(t *testing.T) {
	m, held, cmd := held4Chooser(t)
	asked, _ := press(t, m, "esc")
	if !asked.chooser.giving {
		t.Fatal("esc did not raise the question")
	}
	close(held)
	done := converse(t, asked, cmd)
	if done.chooser != nil && done.chooser.giving {
		t.Fatal("the question outlived the download it was about")
	}
	if done.chooser != nil && strings.Contains(drawn(t, done), "stop the download?") {
		t.Fatalf("the question is still drawn:\n%s", drawn(t, done))
	}
}

// TestTheSpinnerKeepsTurningWhileSomethingArrives is the difference between a
// spinner and a picture of one: a tick is answered with the next tick, so a
// screen that draws one while it waits has to keep the chain going.
func TestTheSpinnerKeepsTurningWhileSomethingArrives(t *testing.T) {
	m, held, _ := held4Chooser(t)
	defer close(held)
	turned, cmd := m.Update(spinner.TickMsg{})
	if cmd == nil {
		t.Fatal("the spinner stopped while the download was still running")
	}
	if turned.(Model).spinner.View() == m.spinner.View() {
		t.Fatal("the spinner is on the same frame it was")
	}
	still := m
	still.ai.busy = ""
	if _, cmd := still.Update(spinner.TickMsg{}); cmd != nil {
		t.Fatal("the spinner turns with nothing to wait for")
	}
}

// TestTheDownloadPanelFitsItsBox is what the bar being exactly as wide as it is
// asked for is for: one column over and the line is cut, which lands on the end
// of the bar and reads as a bar that has stopped.
func TestTheDownloadPanelFitsItsBox(t *testing.T) {
	m, held, _ := held4Chooser(t)
	defer close(held)
	for _, line := range strings.Split(drawn(t, m), "\n") {
		if strings.Contains(line, "…") {
			t.Fatalf("a line of the panel was cut: %q", line)
		}
		if got := lipgloss.Width(line); got > chooserWidth+4 {
			t.Fatalf("a line is %d wide in a box of %d: %q", got, chooserWidth+4, line)
		}
	}
}

// TestTheAnywayQuestionDrawsWhatItWouldTake is why the question is worth
// asking: a share of a machine is something you see at a glance, and a model
// that needs half again what there is should look like it does.
func TestTheAnywayQuestionDrawsWhatItWouldTake(t *testing.T) {
	m := opened4Chooser(t)
	m.ai.memory = 6 << 30
	m.chooser.offers = m.offers()
	m.chooser = m.chooser.at("gpt-oss-120b")
	chosen, ok := m.chooser.selected()
	if !ok {
		t.Fatal("the row is not there")
	}
	if !chosen.grey() {
		t.Fatalf("a 59 GiB model on a 6 GiB machine is not greyed: %+v", chosen.verdict)
	}
	asked, _ := press(t, m, "enter")
	if asked.modal == nil {
		t.Fatal("enter fetched it without asking")
	}
	view := plain(asked.modal.view(100))
	for _, want := range []string{"fetch it anyway?", "GPT-OSS 120B", "memory", "%", "act"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the question does not say %q:\n%s", want, view)
		}
	}
	bar := ui.BarStyleNamed(ui.DefaultBarStyle)
	if !strings.Contains(view, bar.Full) {
		t.Fatalf("the question has no picture of what it would take:\n%s", view)
	}
	if asked.ai.busy != "" {
		t.Fatal("the download started before the question was answered")
	}
}

func TestSayingYesToTheAnywayQuestionFetchesIt(t *testing.T) {
	m := opened4Chooser(t)
	m.ai.memory = 6 << 30
	m.chooser.offers = m.offers()
	m.chooser = m.chooser.at("gpt-oss-120b")
	asked, _ := press(t, m, "enter")
	said, cmd := press(t, asked, "enter")
	if cmd == nil {
		t.Fatal("saying yes did nothing")
	}
	fetching, _ := said.Update(cmd())
	started := fetching.(Model)
	defer settled(t, started)
	if started.pending != "gpt-oss-120b" {
		t.Fatalf("pending = %q, want the model kept for when it lands", started.pending)
	}
	if started.ai.busy == "" {
		t.Fatal("saying yes started nothing")
	}
	if started.modal != nil {
		t.Fatal("the question is still up")
	}
}

// TestASharePastTheWholeIsFullAndRed is the case the picture exists for.
func TestASharePastTheWholeIsFullAndRed(t *testing.T) {
	over := share4Reading("memory", 19<<30, 10<<30)
	if over.Value != "190%" {
		t.Fatalf("Value = %q", over.Value)
	}
	if over.Severity != ui.SevCritical {
		t.Fatalf("severity = %v, want a reading past the whole to read as one", over.Severity)
	}
	full := ui.Default().Gauge(1, over.Severity)
	if ui.Default().Gauge(over.Ratio, over.Severity) != full {
		t.Fatal("a bar past its end is drawn with room still left in it")
	}
	tight := share4Reading("disk", 9<<30, 10<<30)
	if tight.Severity != ui.SevWarn {
		t.Fatalf("severity = %v, want ninety per cent of a machine to be a warning", tight.Severity)
	}
	if room := share4Reading("disk", 1<<30, 10<<30); room.Severity != ui.SevOK {
		t.Fatalf("severity = %v, want a tenth of a machine to be fine", room.Severity)
	}
}

// TestTheListDrawsItsMarks is the list read the way somebody looks at it: a dot
// on what is answering, a tick on what is here, and a greyed row for what this
// machine cannot run.
func TestTheListDrawsItsMarks(t *testing.T) {
	m := opened4Chooser(t)
	m.ai.memory = 6 << 30
	install4Model(t, m, "gemma-4-e2b-qat")
	m.session.Settings.AI.Active = "here"
	m.chooser.offers = m.offers()
	m.chooser.cursor = 0
	view := drawn(t, m)
	if !strings.Contains(view, "● Gemma 4 E4B") {
		t.Fatalf("the model that answers is not marked:\n%s", view)
	}
	if !strings.Contains(view, "✓ Gemma 4 E2B") {
		t.Fatalf("the model that is here is not marked:\n%s", view)
	}
	rows := map[string]string{}
	for _, item := range m.chooser.offers {
		rows[item.key] = m.row4Chooser(item, 60, false)
	}
	if rows["gpt-oss-120b"] == plain(rows["gpt-oss-120b"]) {
		t.Fatal("a model too big for this machine is drawn like every other row")
	}
	if !strings.Contains(rows["gpt-oss-120b"], plain(m.theme.Subtle.Render("GPT-OSS 120B"))) {
		t.Fatalf("a model too big for this machine is not greyed: %q", rows["gpt-oss-120b"])
	}
}

// TestRemovingTheModelInUseLetsGoOfItFirst is what keeps the memory honest:
// deleting the file under a model that is loaded would leave the program
// holding gigabytes it can no longer account for.
func TestRemovingTheModelInUseLetsGoOfItFirst(t *testing.T) {
	talk := &scripted{}
	m := opened4Chooser(t)
	entry := install4Model(t, m, "gemma-4-e4b-qat")
	m.talk.instance, m.talk.loaded, m.assistant = "here", true, talk
	m.chooser.offers = m.offers()
	m.chooser = m.chooser.at(entry.ID)

	done, _ := m.forgot(forgetMsg{id: entry.ID})
	after := done.(Model)
	if !talk.closed {
		t.Fatal("the weights went out from under a model that was still loaded")
	}
	if after.assistant != nil || after.talk.loaded {
		t.Fatal("the screen still thinks it holds a model")
	}
	if m.session.AI.Models.Has(entry.ID) {
		t.Fatal("the weights are still there")
	}
}

func TestRemovingSomethingThatIsNotOfferedIsReported(t *testing.T) {
	m := opened4Chooser(t)
	done, _ := m.forgot(forgetMsg{id: "a model nobody offers"})
	if !strings.Contains(done.(Model).ai.trouble, "no model named") {
		t.Fatalf("trouble = %q", done.(Model).ai.trouble)
	}
	m.session.AI.Models = nil
	if _, cmd := m.forgot(forgetMsg{id: "gemma-4-e4b-qat"}); cmd != nil {
		t.Fatal("something was removed with nowhere to remove it from")
	}
	m = opened4Chooser(t)
	if _, cmd := m.anyway(anywayMsg{id: "a model nobody offers"}); cmd != nil {
		t.Fatal("a model nobody offers was fetched")
	}
}

// TestAnInstanceThatCannotBeWrittenIsReported covers the failure nobody sees
// until it happens: an instance with no name cannot be saved, and the screen
// has to say so rather than close the modal on nothing.
func TestAnInstanceThatCannotBeWrittenIsReported(t *testing.T) {
	m := opened4Chooser(t)
	done, _ := m.wrote(config.AIInstance{Kind: "local", Model: "gemma-4-e2b-qat"})
	after := done.(Model)
	if after.ai.trouble == "" {
		t.Fatal("an instance that could not be written was written")
	}
	if after.chooser == nil {
		t.Fatal("the modal closed on a failure nobody would have read")
	}
}
