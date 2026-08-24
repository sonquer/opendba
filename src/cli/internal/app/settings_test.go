package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
	"github.com/sonquer/tui4db/src/cli/internal/ai/providers"
	"github.com/sonquer/tui4db/src/cli/internal/ai/providers/local"
	"github.com/sonquer/tui4db/src/cli/internal/cli"
	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

func configured(t *testing.T) Model {
	t.Helper()
	m := loaded(t, healthy())
	root := t.TempDir()
	settings := m.session.Settings
	settings.AI = config.AISettings{
		Enabled: true,
		Active:  "claude",
		Context: 8192,
		Instances: []config.AIInstance{
			{Name: "claude", Kind: "anthropic", Model: "claude-sonnet-5"},
			{Name: "here", Kind: "local", Model: "gemma-4-e4b-qat"},
		},
	}
	m.session.Settings = settings
	m.session.AI = cli.Assistant{
		Settings: settings.AI,
		Models:   local.NewStore(root + "/models"),
		Library:  local.NewLibrary(root + "/lib"),
		Registry: providers.All(local.NewStore(root + "/models")),
	}
	opened, _ := m.show(viewAI)
	return opened.(Model)
}

// settled stops a download and waits for the goroutine behind it, so a test
// that started one does not leave it writing into a directory the framework is
// about to remove. Stopping is not waiting: the cancellation is noticed between
// files, and the file being written when it arrives is finished first.
func settled(t *testing.T, m Model) {
	t.Helper()
	m.stopFetching()
	if m.ai.running.progress == nil {
		return
	}
	for range m.ai.running.progress {
		continue
	}
	if m.ai.running.failed != nil {
		<-m.ai.running.failed
	}
}

func shown(t *testing.T, m Model) string {
	t.Helper()
	return plain(m.aiBody())
}

func TestAISettingsLists(t *testing.T) {
	m := configured(t)
	body := shown(t, m)
	for _, want := range []string{
		"INSTANCES", "claude", "anthropic", "claude-sonnet-5", "active",
		"MODELS", "Gemma 4 E4B", "Apache-2.0", "library b10587 · included",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body does not hold %q:\n%s", want, body)
		}
	}
}

func TestAISettingsWalksBothLists(t *testing.T) {
	m := configured(t)
	if m.ai.pane != paneInstances {
		t.Fatal("the screen did not open on the instances")
	}
	down, _ := press(t, m, "down")
	if down.ai.at[paneInstances] != 1 {
		t.Fatalf("cursor = %d, want it moved", down.ai.at[paneInstances])
	}
	past, _ := press(t, down, "down")
	if past.ai.at[paneInstances] != 1 {
		t.Fatal("the cursor walked past the end of the list")
	}
	up, _ := press(t, past, "up")
	if up.ai.at[paneInstances] != 0 {
		t.Fatal("the cursor did not walk back")
	}
	before, _ := press(t, up, "up")
	if before.ai.at[paneInstances] != 0 {
		t.Fatal("the cursor walked past the start of the list")
	}

	models, _ := press(t, up, "tab")
	if models.ai.pane != paneModels {
		t.Fatal("tab did not move to the models")
	}
	moved, _ := press(t, models, "down")
	if moved.ai.at[paneModels] != 1 {
		t.Fatalf("cursor = %d, want it moved in the other list", moved.ai.at[paneModels])
	}
	back, _ := press(t, moved, "tab")
	if back.ai.pane != paneInstances {
		t.Fatal("tab did not move back")
	}
}

func TestAISettingsSwitchesInstance(t *testing.T) {
	m := configured(t)
	moved, _ := press(t, m, "down")
	chosen, _ := press(t, moved, "enter")

	if chosen.ai.active != "here" {
		t.Fatalf("active = %q, want the one under the cursor", chosen.ai.active)
	}
	if chosen.session.Settings.AI.Active != "here" {
		t.Fatal("the choice was not kept on the session")
	}
	if chosen.talk.instance != "here" {
		t.Fatal("the conversation still names the instance that was replaced")
	}
	if chosen.assistant != nil {
		t.Fatal("the assistant that was open was not let go of")
	}
	if !strings.Contains(shown(t, chosen), "here") {
		t.Fatalf("body = %q", shown(t, chosen))
	}

	written, err := chosen.workspace.Setup().Store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings() error = %v", err)
	}
	if written.AI.Active != "here" {
		t.Fatalf("settings.toml says %q, want the choice written down", written.AI.Active)
	}
}

func TestAISettingsWithNothingConfigured(t *testing.T) {
	m := loaded(t, healthy())
	opened, _ := m.show(viewAI)
	body := shown(t, opened.(Model))
	if !strings.Contains(body, "none yet") {
		t.Fatalf("body = %q, want it to say where instances are written", body)
	}
	unchanged, _ := press(t, opened.(Model), "enter")
	if unchanged.ai.active != "" {
		t.Fatal("something was activated where nothing is configured")
	}
}

func TestAISettingsRemovesAModel(t *testing.T) {
	m := configured(t)
	entry, err := local.Offered("gemma-4-e4b-qat")
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
	m = m.read4AI()
	if !m.ai.models[0].installed {
		t.Fatal("the model that was put there is not listed as downloaded")
	}
	if !strings.Contains(shown(t, m), "downloaded") {
		t.Fatalf("body = %q", shown(t, m))
	}

	models, _ := press(t, m, "tab")
	removed, _ := press(t, models, "d")
	if removed.ai.models[0].installed {
		t.Fatal("the model was not removed")
	}
	if store.Has(entry.ID) {
		t.Fatal("the files were left on disk")
	}
}

func TestAISettingsWillNotRemoveWhatIsNotThere(t *testing.T) {
	m := configured(t)
	models, _ := press(t, m, "tab")
	unchanged, _ := press(t, models, "d")
	if unchanged.ai.trouble != "" {
		t.Fatalf("trouble = %q, want removing nothing to be quiet", unchanged.ai.trouble)
	}
	instances, _ := press(t, m, "d")
	if instances.ai.trouble != "" {
		t.Fatal("d on the instances did something")
	}
}

func TestAISettingsFetches(t *testing.T) {
	m := configured(t)
	started, cmd := m.started4AI(jobModel, "fetching", "Gemma 4 E4B", func(ctx context.Context, out chan<- local.Progress) error {
		for _, told := range []local.Progress{
			{ID: "gemma", Bytes: 1 << 20, Total: 4 << 20},
			{ID: "gemma", Bytes: 4 << 20, Total: 4 << 20, Done: true},
		} {
			select {
			case out <- told:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	running := started.(Model)
	if running.ai.busy == "" {
		t.Fatal("the screen does not think it is fetching")
	}
	done := converse(t, running, cmd)
	if done.ai.busy != "" {
		t.Fatalf("busy = %q, want the download finished", done.ai.busy)
	}
	if done.ai.trouble != "" {
		t.Fatalf("trouble = %q, want none", done.ai.trouble)
	}
	if !strings.Contains(done.ai.note, "is here") {
		t.Fatalf("note = %q, want it to say what arrived", done.ai.note)
	}
}

func TestAISettingsShowsHowFarADownloadHasGot(t *testing.T) {
	m := configured(t)
	m.ai.busy, m.ai.doing, m.ai.subject = "fetching Gemma 4 E4B", "fetching", "Gemma 4 E4B"
	m.ai.progress = local.Progress{Bytes: 1 << 20, Total: 4 << 20}
	if !strings.Contains(shown(t, m), "25%") {
		t.Fatalf("body = %q, want it to say how far it has got", shown(t, m))
	}
	m.ai.progress = local.Progress{Bytes: 1 << 20}
	if strings.Contains(shown(t, m), "%") {
		t.Fatal("a share was worked out of a total nobody reported")
	}
}

func TestAISettingsReportsAFetchThatFailed(t *testing.T) {
	broken := errors.New("github answered 404")
	m := configured(t)
	started, cmd := m.started4AI(jobModel, "fetching", "it", func(context.Context, chan<- local.Progress) error {
		return broken
	})
	done := converse(t, started.(Model), cmd)
	if !strings.Contains(done.ai.trouble, "404") {
		t.Fatalf("trouble = %q, want the failure said out loud", done.ai.trouble)
	}
	if !strings.Contains(shown(t, done), "404") {
		t.Fatalf("body = %q", shown(t, done))
	}
}

func TestAISettingsStopsAFetch(t *testing.T) {
	m := configured(t)
	held := make(chan struct{})
	started, cmd := m.started4AI(jobModel, "fetching", "it", func(ctx context.Context, out chan<- local.Progress) error {
		select {
		case <-held:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	running := started.(Model)
	stopped, _ := press(t, running, "esc")
	if stopped.view != viewAI {
		t.Fatal("esc left the screen instead of stopping the download")
	}
	done := converse(t, stopped, cmd)
	if done.ai.busy != "" {
		t.Fatal("the download was not stopped")
	}
}

func TestAISettingsIgnoresAStaleFetch(t *testing.T) {
	m := configured(t)
	m.ai.token = 4
	updated, _ := m.Update(fetchProgressMsg{progress: local.Progress{Bytes: 9}, token: 1})
	if updated.(Model).ai.progress.Bytes != 0 {
		t.Fatal("a report from a download that was abandoned reached the screen")
	}
	ended, _ := m.Update(fetchEndedMsg{err: errors.New("from before"), token: 1})
	if ended.(Model).ai.trouble != "" {
		t.Fatal("an ending from a download that was abandoned reached the screen")
	}
}

func TestAISettingsWillNotFetchTwiceAtOnce(t *testing.T) {
	m := configured(t)
	m.ai.busy = "already fetching"
	unchanged, cmd := press(t, m, "enter")
	if cmd != nil {
		t.Fatal("a second download was started while one was running")
	}
	if unchanged.ai.busy != "already fetching" {
		t.Fatal("the download that was running was forgotten")
	}
}

func TestAISettingsSaysWhenAModelWillNotFit(t *testing.T) {
	m := configured(t)
	m.ai.library.present = true
	m.ai.pane = paneModels
	m.ai.models[0].verdict = local.Verdict{Reason: "there is not enough room"}
	told, cmd := press(t, m, "enter")
	if cmd != nil {
		t.Fatal("a model that will not fit was fetched anyway")
	}
	if !strings.Contains(told.ai.note, "not enough room") {
		t.Fatalf("note = %q, want the reason", told.ai.note)
	}
}

func TestAISettingsSaysWhenAModelIsAlreadyHere(t *testing.T) {
	m := configured(t)
	m.ai.library.present = true
	m.ai.pane = paneModels
	m.ai.models[0].installed = true
	told, cmd := press(t, m, "enter")
	if cmd != nil {
		t.Fatal("a model that is already here was fetched again")
	}
	if !strings.Contains(told.ai.note, "already here") {
		t.Fatalf("note = %q", told.ai.note)
	}
}

func TestAISettingsGoesBack(t *testing.T) {
	m := configured(t)
	back, _ := press(t, m, "esc")
	if back.view != viewDashboard {
		t.Fatalf("view = %q, want the dashboard", back.view)
	}
}

func TestAISettingsInThePalette(t *testing.T) {
	m := loaded(t, healthy())
	opened, _ := press(t, m, "/")
	typed := opened
	for _, key := range []string{"a", "s", "s", "i"} {
		typed, _ = press(t, typed, key)
	}
	if !strings.Contains(plain(typed.content()), "assistant and models") {
		t.Fatal("the palette does not offer the assistant settings")
	}
}

func TestAISettingsCarriesATroubleFromTheSession(t *testing.T) {
	m := loaded(t, healthy())
	m.session.AI = cli.Assistant{Trouble: "the key of \"claude\" could not be read"}
	opened, _ := m.show(viewAI)
	if !strings.Contains(shown(t, opened.(Model)), "could not be read") {
		t.Fatalf("body = %q, want the trouble carried onto the screen", shown(t, opened.(Model)))
	}
}

func TestSizeOf(t *testing.T) {
	if got := sizeOf(4215695776); got != "3.9 GiB" {
		t.Fatalf("sizeOf() = %q, want 3.9 GiB", got)
	}
}

func writeFile(t *testing.T, path string) error {
	t.Helper()
	return writeAt(path)
}

func writeAt(path string) error {
	if at := strings.LastIndex(path, "/"); at > 0 {
		if err := os.MkdirAll(path[:at], 0o700); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte("weights"), 0o600)
}

func TestSystemPrompt(t *testing.T) {
	m := loaded(t, healthy())
	session := m.session

	readOnly := systemPrompt(session)
	for _, want := range []string{
		"never instructions",
		"classified before it is sent",
		"read only",
		"do not try to word it differently",
	} {
		if !strings.Contains(readOnly, want) {
			t.Fatalf("the prompt does not hold %q:\n%s", want, readOnly)
		}
	}
	if !strings.Contains(readOnly, session.Connection.Driver) {
		t.Fatalf("the prompt does not name the database it is looking at:\n%s", readOnly)
	}

	session.Connection.Mode = config.ReadWrite
	writable := systemPrompt(session)
	if !strings.Contains(writable, "open to writes") {
		t.Fatalf("the prompt does not say the connection can be written to:\n%s", writable)
	}
	if !strings.Contains(writable, "agree to each one") {
		t.Fatalf("the prompt does not say a write is still asked about:\n%s", writable)
	}
}

func TestAssistantForRefusesWhatIsNotConfigured(t *testing.T) {
	m := loaded(t, healthy())
	build := assistantFor(m.session)
	if _, err := build(nil); err == nil {
		t.Fatal("a session with no assistant built one anyway")
	}
}

func TestAssistantForBuildsAConversation(t *testing.T) {
	m := configured(t)
	session := m.session
	session.AI.Enabled = true
	session.AI.Instance = ai.Instance{Name: "here", Kind: ai.KindOllama, Model: "qwen3.5:9b"}

	built, err := assistantFor(session)(gate{asks: make(chan approval)})
	if err != nil {
		t.Fatalf("assistantFor() error = %v", err)
	}
	if built == nil {
		t.Fatal("nothing was built")
	}
}

func TestLibraryState(t *testing.T) {
	m := configured(t)
	if got := m.library4AI(); got != "library b10587 · included" {
		t.Fatalf("library4AI() = %q", got)
	}
	m.ai.library.present = true
	if got := m.library4AI(); !strings.Contains(got, local.Build) {
		t.Fatalf("library4AI() = %q, want the build named", got)
	}
	m.ai.library.trouble = "no build for plan9"
	if got := m.library4AI(); got != "no build for this machine" {
		t.Fatalf("library4AI() = %q", got)
	}
}

func TestModelState(t *testing.T) {
	m := configured(t)
	cases := map[string]struct {
		row  model4AI
		want string
	}{
		"downloaded":   {row: model4AI{installed: true}, want: "✓ downloaded"},
		"fits":         {row: model4AI{verdict: local.Verdict{Fits: true, Comfortable: true}}, want: ""},
		"tight":        {row: model4AI{verdict: local.Verdict{Fits: true}}, want: "tight"},
		"will not fit": {row: model4AI{}, want: "too big for this machine"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := plain(m.state4Model(test.row)); got != test.want {
				t.Fatalf("state4Model() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAISettingsWillNotFetchALibraryItHasNoBuildFor(t *testing.T) {
	m := configured(t)
	m.ai.library.trouble = "no build for this machine"
	m.ai.pane = paneModels
	_, cmd := press(t, m, "enter")
	if cmd != nil {
		t.Fatal("a library was fetched for a machine nobody publishes for")
	}
}

func TestAISettingsFetchesTheLibraryFirst(t *testing.T) {
	m := configured(t)
	m.ai.pane = paneModels
	started, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatal("nothing was started")
	}
	if !strings.Contains(started.ai.busy, local.Build) {
		t.Fatalf("busy = %q, want the library on disk before any model", started.ai.busy)
	}
	settled(t, started)
}

func TestAISettingsFetchesAModelOnceTheLibraryIsHere(t *testing.T) {
	m := configured(t)
	m.ai.library.present = true
	m.ai.pane = paneModels

	started, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatal("nothing was started")
	}
	if !strings.Contains(started.ai.busy, "Gemma") {
		t.Fatalf("busy = %q, want the weights fetched once the library is here", started.ai.busy)
	}
	settled(t, started)
}

func TestAISettingsFetchesNothingWithoutSomewhereToPutIt(t *testing.T) {
	m := configured(t)
	m.ai.library.present = true
	m.session.AI.Models = nil
	m.ai.pane = paneModels
	if _, cmd := press(t, m, "enter"); cmd != nil {
		t.Fatal("a download started with nowhere to put it")
	}
	if _, cmd := press(t, m, "d"); cmd != nil {
		t.Fatal("a removal started with nothing to remove from")
	}
}

func TestAISettingsRefusesAnInstanceThatWentAway(t *testing.T) {
	m := configured(t)
	m.session.Settings.AI.Instances = nil
	broken, _ := press(t, m, "enter")
	if broken.ai.trouble == "" {
		t.Fatal("activating an instance that is no longer configured said nothing")
	}
}

func TestAISettingsWalksNothing(t *testing.T) {
	m := loaded(t, healthy())
	opened, _ := m.show(viewAI)
	empty := opened.(Model)
	empty.ai.instances = nil
	empty.ai.models = nil
	if walked, _ := press(t, empty, "down"); walked.ai.at[paneInstances] != 0 {
		t.Fatal("the cursor moved in an empty list")
	}
	models, _ := press(t, empty, "tab")
	if walked, _ := press(t, models, "up"); walked.ai.at[paneModels] != 0 {
		t.Fatal("the cursor moved in the other empty list")
	}
}

func TestAISettingsListsNothingWhenTheCatalogueIsEmpty(t *testing.T) {
	m := configured(t)
	m.ai.models = nil
	if !strings.Contains(shown(t, m), "MODELS") {
		t.Fatalf("body = %q, want the section even with nothing in it", shown(t, m))
	}
}

func TestHowLongIsLeftIsSaidInWholeUnits(t *testing.T) {
	cases := map[time.Duration]string{
		7 * time.Second:                             "7s left",
		500 * time.Millisecond:                      "1s left",
		90 * time.Second:                            "1m 30s left",
		2*time.Hour + 5*time.Minute:                 "2h 5m left",
		time.Duration(7.004 * float64(time.Second)): "7s left",
	}
	for left, want := range cases {
		if got := waiting(left); got != want {
			t.Errorf("waiting(%s) = %q, want %q", left, got, want)
		}
	}
}

func TestADownloadSaysHowFastAndHowLong(t *testing.T) {
	m := configured(t)
	m.ai.first, m.ai.since = 0, time.Now().Add(-2*time.Second)
	m.ai.progress = local.Progress{Bytes: 2 << 20, Total: 6 << 20}
	said := m.arrived4AI()
	for _, want := range []string{"33%", "2.0 MiB of 6.0 MiB", "KiB/s", "left"} {
		if !strings.Contains(said, want) {
			t.Fatalf("arrived4AI() = %q, want it to say %q", said, want)
		}
	}
	m.ai.first = -1
	if said := m.arrived4AI(); strings.Contains(said, "/s") {
		t.Fatalf("arrived4AI() = %q, want no rate before anything has arrived", said)
	}
}

// finished is a download of a model that has ended, with the weights where a
// finished download leaves them.
func finished(t *testing.T, m Model, id string) Model {
	t.Helper()
	install4Model(t, m, id)
	m.pending = id
	m.ai.job, m.ai.doing, m.ai.subject = jobModel, "fetching", id
	m.ai.busy = "fetching " + id
	return m
}

// TestAFinishedDownloadEndsInTheConversation is the whole point of downloading
// one: what was wanted was something to ask, not a full disk.
func TestAFinishedDownloadEndsInTheConversation(t *testing.T) {
	m := configured(t)
	m.session.Settings.AI.Instances = []config.AIInstance{
		{Name: "claude", Kind: "anthropic", Model: "claude-sonnet-5"},
	}
	m.session.AI.Settings = m.session.Settings.AI
	m = finished(t, m, "gemma-4-e2b-qat")

	done, _ := m.doneFetching(fetchEndedMsg{token: m.ai.token})
	after := done.(Model)
	if after.view != viewAsk {
		t.Fatalf("view = %s, want the conversation", after.view)
	}
	if after.pending != "" {
		t.Fatalf("pending = %q, want nothing left waiting", after.pending)
	}
	if after.session.Settings.AI.Active != cli.InstanceName(ai.KindLocal, "gemma-4-e2b-qat") {
		t.Fatalf("active = %q, want the model that just arrived", after.session.Settings.AI.Active)
	}
	if after.build == nil {
		t.Fatal("the conversation has nothing to talk to")
	}
	if after.chooser != nil {
		t.Fatal("the modal is still up")
	}
	written, err := after.workspace.Setup().Store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings() error = %v", err)
	}
	if !written.AI.Enabled {
		t.Fatal("settings.toml does not say the assistant is on")
	}
}

// TestADownloadThatLeftNothingSaysSoOnce is the loop that must not happen: a
// download that reported no failure and left nothing the store will admit to is
// a fault to report, not a reason to fetch gigabytes again.
func TestADownloadThatLeftNothingSaysSoOnce(t *testing.T) {
	m := configured(t)
	m.pending = "gemma-4-e2b-qat"
	m.ai.job, m.ai.subject = jobModel, "Gemma 4 E2B"
	m.ai.busy = "fetching Gemma 4 E2B"

	done, cmd := m.doneFetching(fetchEndedMsg{token: m.ai.token})
	after := done.(Model)
	if cmd != nil || after.ai.busy != "" {
		t.Fatal("it started fetching the same thing again")
	}
	if after.pending != "" {
		t.Fatalf("pending = %q", after.pending)
	}
	if !strings.Contains(after.ai.trouble, "has not been downloaded") {
		t.Fatalf("trouble = %q, want the store's own account of it", after.ai.trouble)
	}
}

// TestTheLibraryStepIsFollowedByTheWeights is the ordinary case of the same
// code: the first thing chosen fetches the library, and the weights follow it
// without anybody pressing anything.
func TestTheLibraryStepIsFollowedByTheWeights(t *testing.T) {
	m := configured(t)
	m.pending = "gemma-4-e2b-qat"
	m.ai.job, m.ai.subject = jobLibrary, "llama.cpp "+local.Build
	m.ai.busy = "unpacking llama.cpp"

	done, cmd := m.doneFetching(fetchEndedMsg{token: m.ai.token})
	after := done.(Model)
	defer settled(t, after)
	if cmd == nil {
		t.Fatal("the weights were not fetched after the library")
	}
	if after.ai.subject != "Gemma 4 E2B" {
		t.Fatalf("subject = %q, want the weights next", after.ai.subject)
	}
	if after.pending != "gemma-4-e2b-qat" {
		t.Fatalf("pending = %q, want the model still waiting on its weights", after.pending)
	}
}

func TestADownloadOnTheSettingsScreenIsDrawnWithABar(t *testing.T) {
	m := configured(t)
	m.ai.busy, m.ai.doing, m.ai.subject = "fetching Gemma 4 E2B", "fetching", "Gemma 4 E2B"
	m.ai.first, m.ai.since = 0, time.Now().Add(-time.Second)
	m.ai.progress = local.Progress{Bytes: 1 << 20, Total: 4 << 20}
	body := shown(t, m)
	if !strings.Contains(body, ui.BarStyleNamed(ui.DefaultBarStyle).Full) {
		t.Fatalf("the screen says how far it has got and does not draw it:\n%s", body)
	}
	if !strings.Contains(body, "25%") {
		t.Fatalf("body = %q", body)
	}
}
