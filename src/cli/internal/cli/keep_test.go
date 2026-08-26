package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sonquer/opendba/src/cli/internal/config"
)

// The history and the conversations are one file each whichever connection is in
// front, so every session shares the handle rather than opening its own: each
// file takes one writer at a time, and several handles wait on each other and
// then fail.
func TestOneHandleIsSharedByEverySession(t *testing.T) {
	dir := t.TempDir()
	kept := NewKeep()
	t.Cleanup(func() { _ = kept.Close() })
	settings := config.DefaultSettings()

	first, trouble := kept.History(filepath.Join(dir, "history.db"), settings.History)
	if first == nil {
		t.Fatalf("History: %s", trouble)
	}
	if second, _ := kept.History(filepath.Join(dir, "history.db"), settings.History); first != second {
		t.Error("a second session must be given the handle the first one opened")
	}

	talk, trouble := kept.Chats(filepath.Join(dir, "chats.db"), settings.Chats)
	if talk == nil {
		t.Fatalf("Chats: %s", trouble)
	}
	if again, _ := kept.Chats(filepath.Join(dir, "chats.db"), settings.Chats); talk != again {
		t.Error("and so must a second session asking for the conversations")
	}
}

// An App built without a Keep opens a handle of its own, which is what a command
// that holds one connection and then leaves wants.
func TestNoKeepOpensAHandleOfItsOwn(t *testing.T) {
	var kept *Keep
	settings := config.DefaultSettings()

	store, trouble := kept.History(filepath.Join(t.TempDir(), "history.db"), settings.History)
	if store == nil {
		t.Fatalf("History: %s", trouble)
	}
	t.Cleanup(func() { _ = store.Close() })
	talk, trouble := kept.Chats(filepath.Join(t.TempDir(), "chats.db"), settings.Chats)
	if talk == nil {
		t.Fatalf("Chats: %s", trouble)
	}
	t.Cleanup(func() { _ = talk.Close() })
	if err := kept.Close(); err != nil {
		t.Errorf("closing nothing is not an error: %v", err)
	}
}

// Closing twice closes nothing twice, so the program that built the Keep and a
// test that tidies up after it do not have to agree on which of them owns it.
func TestClosingAKeepTwiceIsQuiet(t *testing.T) {
	kept := NewKeep()
	dir := t.TempDir()
	settings := config.DefaultSettings()
	if _, trouble := kept.History(filepath.Join(dir, "history.db"), settings.History); trouble != "" {
		t.Fatalf("History: %s", trouble)
	}
	if _, trouble := kept.Chats(filepath.Join(dir, "chats.db"), settings.Chats); trouble != "" {
		t.Fatalf("Chats: %s", trouble)
	}
	if err := kept.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := kept.Close(); err != nil {
		t.Errorf("Close again: %v", err)
	}
}

// What cannot be opened is said out loud, and is not remembered as opened.
func TestAStoreThatWillNotOpenIsSaidOutLoud(t *testing.T) {
	kept := NewKeep()
	t.Cleanup(func() { _ = kept.Close() })
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, trouble := kept.History(filepath.Join(blocked, "history.db"),
		config.DefaultSettings().History)
	if store != nil || trouble == "" {
		t.Errorf("store = %v trouble = %q", store, trouble)
	}
	talk, trouble := kept.Chats(filepath.Join(blocked, "chats.db"),
		config.DefaultSettings().Chats)
	if talk != nil || trouble == "" {
		t.Errorf("store = %v trouble = %q", talk, trouble)
	}
}
