package app

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/config"
)

// crash is what is left behind when the program ends in a way nobody planned.
type crash struct {
	paths   config.Paths
	version string
}

// wrote records a failure and returns the path it was written to.
func (c crash) wrote(doing string, cause any, stack []byte) string {
	if c.paths.State == "" {
		return ""
	}
	path := c.paths.CrashFile(stamp())
	if err := os.WriteFile(path, []byte(c.account(doing, cause, stack)), 0o600); err != nil {
		return ""
	}
	return path
}

func (c crash) account(doing string, cause any, stack []byte) string {
	said := []string{
		"opendba " + c.version,
		"when: " + time.Now().Format(time.RFC3339),
		"doing: " + doing,
		fmt.Sprintf("what happened: %v", cause),
		"",
		string(stack),
	}
	if tail := c.engineLog(); tail != "" {
		said = append(said, "", "the last the inference library said:", "", tail)
	}
	return strings.Join(said, "\n")
}

// engineLog is the end of what llama.cpp wrote.
func (c crash) engineLog() string {
	read, err := os.ReadFile(c.paths.EngineLog())
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(read), "\n"), "\n")
	if len(lines) > engineLogTail {
		lines = lines[len(lines)-engineLogTail:]
	}
	return strings.Join(lines, "\n")
}

const engineLogTail = 40

// stamp names a crash by when it happened, so one does not overwrite the last.
func stamp() string { return time.Now().Format("20060102-150405") }

// crashedMsg is a piece of work that fell over.
type crashedMsg struct {
	doing string
	cause string
	where string
}

// guard runs work that must not take the program down with it.
func (m Model) guard(doing string, work func() tea.Msg) tea.Cmd {
	report := crash{paths: m.workspace.Setup().Store.Paths, version: m.session.Version}
	return func() (msg tea.Msg) {
		defer func() {
			if cause := recover(); cause != nil {
				msg = crashedMsg{
					doing: doing,
					cause: fmt.Sprintf("%v", cause),
					where: report.wrote(doing, cause, debug.Stack()),
				}
			}
		}()
		return work()
	}
}

// crashed puts a failure that would have ended the program on the screen.
func (m Model) crashed(msg crashedMsg) (tea.Model, tea.Cmd) {
	m.talk.busy, m.talk.loading = false, false
	m.ai.busy = ""
	m.stopAsk, m.stopFetch, m.stopLoad = nil, nil, nil
	m.stopQuery, m.inflight = nil, false
	m.stopExport, m.exporter, m.plan, m.chats = nil, nil, nil, nil
	m.switcher, m.catalog, m.preferences = nil, nil, nil
	said := "something went wrong while " + msg.doing + ": " + msg.cause
	if msg.where != "" {
		said += "; what happened is written in " + msg.where
	}
	m.talk.trouble = said
	m.ai.trouble = said
	return m, m.notify("something went wrong while " + msg.doing)
}

// fell turns a panic that happened away from the screen into an error the
// screen can show, with the file to look in for the rest of it.
func fell(doing string, cause any, where string) error {
	if where == "" {
		return fmt.Errorf("something went wrong while %s: %v", doing, cause)
	}
	return fmt.Errorf("something went wrong while %s: %v; what happened is written in %s",
		doing, cause, where)
}
