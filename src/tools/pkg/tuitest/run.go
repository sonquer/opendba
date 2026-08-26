package tuitest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sonquer/opendba/src/tools/pkg/tuitest/shot"
)

// Failure is one step that did not do what the scenario said it would.
type Failure struct {
	Step   int
	Action string
	Reason string
	Detail string
}

// String names the step and what went wrong with it.
func (f Failure) String() string {
	if f.Detail == "" {
		return fmt.Sprintf("step %d (%s): %s", f.Step, f.Action, f.Reason)
	}
	return fmt.Sprintf("step %d (%s): %s\n%s", f.Step, f.Action, f.Reason, f.Detail)
}

// Result is what running one scenario at one size proved.
type Result struct {
	Scenario  string
	Size      Size
	Failures  []Failure
	Updated   []string
	Frame     Frame
	Chunks    []Chunk
	Exit      int
	Elapsed   time.Duration
	Artifacts string
	Grid      [][]shot.Cell
}

// OK reports whether the scenario did what it said it would.
func (r Result) OK() bool { return len(r.Failures) == 0 }

// Runner runs scenarios against a built program.
type Runner struct {
	Suite    Suite
	Binary   string
	Update   bool
	Workdir  string
	Artifact string
	Env      []string
	Now      func() time.Time
}

// Run walks one scenario at one size, from a fresh sandbox to a stopped program.
func (r Runner) Run(scenario Scenario, size Size) (Result, error) {
	now := r.Now
	if now == nil {
		now = time.Now
	}
	started := now()
	result := Result{Scenario: scenario.Name, Size: size}

	root, err := os.MkdirTemp(r.Workdir, "tuitest-")
	if err != nil {
		return result, fmt.Errorf("make a sandbox: %w", err)
	}
	defer func() { _ = os.RemoveAll(root) }()

	box, err := NewSandbox(root)
	if err != nil {
		return result, err
	}
	fixture := filepath.Join(box.Databases(), scenario.Seed+".db")
	if err := Seed(fixture, r.Suite.SeedFile(scenario)); err != nil {
		return result, err
	}
	relative := filepath.Join("db", scenario.Seed+".db")
	second := filepath.Join(r.Suite.Root, seedsDir, "second.sql")
	if _, err := os.Stat(second); err == nil {
		if err := Seed(filepath.Join(box.Databases(), "second.db"), second); err != nil {
			return result, err
		}
	}
	connection := scenario.Connection
	if connection == "" {
		connection = "screens"
	}
	if !scenario.Setup {
		if err := box.WriteProfiles(profilesFor(connection, relative)); err != nil {
			return result, err
		}
	}
	if err := box.WriteSettings(r.Suite.Bar); err != nil {
		return result, err
	}

	session, err := Start(Options{
		Binary: r.Binary,
		Args:   arguments(scenario, connection),
		Dir:    root,
		Env:    append(box.Environment(), r.Env...),
		Width:  size.Width,
		Height: size.Height,
		Quiet:  r.Suite.Quiet.Every(),
		Now:    now,
	})
	if err != nil {
		return result, err
	}

	deadline := now().Add(r.Suite.Timeout.Every())
	walker := &walker{suite: r.Suite, update: r.Update}
	result.Failures = walker.walk(session, scenario, size, deadline)
	result.Updated = walker.updated
	result.Frame = session.Frame()
	result.Chunks = session.Chunks()
	result.Grid = session.Grid()

	if scenario.Exit == nil {
		_ = session.Send([]byte{0x03})
	}
	exit, _ := session.Close()
	result.Exit = exit
	if scenario.Exit != nil && exit != *scenario.Exit {
		result.Failures = append(result.Failures, Failure{
			Action: "exit",
			Reason: fmt.Sprintf("the program left with %d, not %d", exit, *scenario.Exit),
		})
	}
	result.Elapsed = now().Sub(started)

	crashes, err := box.Crashes()
	if err != nil {
		return result, err
	}
	for _, crash := range crashes {
		report, _ := os.ReadFile(crash)
		result.Failures = append(result.Failures, Failure{
			Action: "crash",
			Reason: "the program left an account of a failure behind",
			Detail: string(report),
		})
	}
	if !result.OK() && r.Artifact != "" {
		result.Artifacts = filepath.Join(r.Artifact, scenario.Name, size.String())
		if err := result.Keep(result.Artifacts, result.Grid, now()); err != nil {
			return result, err
		}
	}
	return result, nil
}

// arguments starts the program the way the scenario wants it started: a
// scenario about the first run names no connection, because there is none.
func arguments(scenario Scenario, connection string) []string {
	if scenario.Setup {
		return []string{"tui"}
	}
	return []string{"tui", "--connection", connection}
}

// profilesFor names the databases by a path relative to the directory the
// program is started in, so that a screen which shows where a database lives
// shows the same thing on every machine.
func profilesFor(connection, fixture string) []Profile {
	return []Profile{
		{ID: connection, Name: connection, Driver: "sqlite", File: fixture, Mode: "readonly", Color: "green"},
		{
			ID: connection + "-eu", Name: "production-eu", Driver: "sqlite",
			File: filepath.Join("db", "second.db"), Mode: "readonly", Color: "red",
		},
	}
}

// walker carries what one pass through a scenario collects, so that scenarios
// running beside each other never share it.
type walker struct {
	suite   Suite
	update  bool
	updated []string
}

func (r *walker) walk(session *Session, scenario Scenario, size Size, deadline time.Time) []Failure {
	var failures []Failure
	for i, step := range scenario.Steps {
		action, _ := step.Action()
		failure := r.take(session, step, size, deadline)
		if failure == nil {
			continue
		}
		failure.Step = i + 1
		failure.Action = action
		failures = append(failures, *failure)
		break
	}
	if leaked := r.suite.Leaked(session.Frame().Plain()); len(leaked) > 0 {
		failures = append(failures, Failure{
			Action: "forbid",
			Reason: "the screen showed something that must never be drawn",
			Detail: strings.Join(leaked, ", "),
		})
	}
	return failures
}

func (r *walker) take(session *Session, step Step, size Size, deadline time.Time) *Failure {
	switch {
	case step.Key != "":
		return sent(session.Key(step.Key), session, deadline)
	case len(step.Keys) > 0:
		for _, name := range step.Keys {
			if failure := sent(session.Key(name), session, deadline); failure != nil {
				return failure
			}
		}
		return nil
	case step.Type != "":
		return sent(session.Type(step.Type), session, deadline)
	case step.Wait != "":
		return r.await(session, deadline, step.Wait, true)
	case step.WaitGone != "":
		return r.await(session, deadline, step.WaitGone, false)
	case len(step.Expect) > 0:
		return r.expect(session, deadline, step.Expect)
	case len(step.ExpectAbsent) > 0:
		return r.absent(session, deadline, step.ExpectAbsent)
	case step.Match != "":
		return r.match(session, deadline, step.Match)
	case step.Shot != "":
		return r.shot(session, deadline, size, step.Shot)
	case step.Resize != "":
		return r.resize(session, deadline, step.Resize)
	}
	return &Failure{Reason: "the step does nothing"}
}

func sent(err error, session *Session, deadline time.Time) *Failure {
	if err != nil {
		return &Failure{Reason: err.Error()}
	}
	session.Settle(deadline)
	return nil
}

func (r *walker) await(session *Session, deadline time.Time, text string, wanted bool) *Failure {
	frame, ok := session.Await(deadline, func(f Frame) bool { return f.Contains(text) == wanted })
	if ok {
		return nil
	}
	verb := "never appeared"
	if !wanted {
		verb = "never went away"
	}
	return &Failure{Reason: fmt.Sprintf("%q %s", text, verb), Detail: frame.Plain()}
}

func (r *walker) expect(session *Session, deadline time.Time, wanted []string) *Failure {
	frame, ok := session.Await(deadline, func(f Frame) bool {
		for _, text := range wanted {
			if !f.Contains(text) {
				return false
			}
		}
		return true
	})
	if ok {
		return nil
	}
	var missing []string
	for _, text := range wanted {
		if !frame.Contains(text) {
			missing = append(missing, fmt.Sprintf("%q", text))
		}
	}
	return &Failure{
		Reason: "the screen never showed " + strings.Join(missing, ", "),
		Detail: frame.Plain(),
	}
}

func (r *walker) absent(session *Session, deadline time.Time, unwanted []string) *Failure {
	session.Settle(deadline)
	frame := session.Frame()
	var present []string
	for _, text := range unwanted {
		if frame.Contains(text) {
			present = append(present, fmt.Sprintf("%q", text))
		}
	}
	if len(present) == 0 {
		return nil
	}
	return &Failure{
		Reason: "the screen showed " + strings.Join(present, ", "),
		Detail: frame.Plain(),
	}
}

func (r *walker) match(session *Session, deadline time.Time, pattern string) *Failure {
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return &Failure{Reason: err.Error()}
	}
	frame, ok := session.Await(deadline, func(f Frame) bool { return compiled.MatchString(f.Plain()) })
	if ok {
		return nil
	}
	return &Failure{Reason: fmt.Sprintf("nothing on the screen matched %q", pattern), Detail: frame.Plain()}
}

func (r *walker) resize(session *Session, deadline time.Time, value string) *Failure {
	size, err := ParseSize(value)
	if err != nil {
		return &Failure{Reason: err.Error()}
	}
	if err := session.Resize(size.Width, size.Height); err != nil {
		return &Failure{Reason: err.Error()}
	}
	session.Settle(deadline)
	return nil
}

func (r *walker) shot(session *Session, deadline time.Time, size Size, name string) *Failure {
	session.Settle(deadline)
	drawn := r.suite.Apply(session.Frame().Plain())
	path := r.suite.GoldenPath(size, name)
	if r.update {
		if err := Write(path, drawn); err != nil {
			return &Failure{Reason: err.Error()}
		}
		r.updated = append(r.updated, path)
		return nil
	}
	difference, err := Compare(path, drawn)
	if err != nil {
		return &Failure{Reason: err.Error()}
	}
	if difference == "" {
		return nil
	}
	return &Failure{Reason: "the screen is not the one that was kept", Detail: difference}
}
