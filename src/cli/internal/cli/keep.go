package cli

import (
	"sync"

	"github.com/sonquer/opendba/src/cli/internal/chats"
	"github.com/sonquer/opendba/src/cli/internal/config"
	"github.com/sonquer/opendba/src/cli/internal/history"
)

// Keep is where the things that belong to the program rather than to any one
// connection are opened once. The statements that have been run and the
// conversations that have been had are one file each, whichever connection is in
// front, and a handle per connection is a handle too many: each of those files
// takes one writer at a time, so several connections writing at once wait on
// each other and then fail.
//
// A nil Keep opens a handle of its own every time, which is what a command that
// holds one connection and then leaves wants.
type Keep struct {
	mu            sync.Mutex
	history       *history.Store
	conversations *chats.Store
}

// NewKeep returns a Keep with nothing opened yet.
func NewKeep() *Keep { return &Keep{} }

// History opens the query history the first time it is asked for and hands the
// same store back afterwards, answering to the settings as they are now.
func (k *Keep) History(path string, settings config.HistorySettings) (*history.Store, string) {
	if k == nil {
		return openHistory(path, settings)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.history != nil {
		return k.history.Following(settings), ""
	}
	store, trouble := openHistory(path, settings)
	k.history = store
	return store, trouble
}

// Chats opens the conversations the first time they are asked for and hands the
// same store back afterwards.
func (k *Keep) Chats(path string, settings config.ChatSettings) (*chats.Store, string) {
	if k == nil {
		return openChats(path, settings)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.conversations != nil {
		return k.conversations.Following(settings), ""
	}
	store, trouble := openChats(path, settings)
	k.conversations = store
	return store, trouble
}

// Close gives back whatever was opened. Closing twice closes nothing twice.
func (k *Keep) Close() error {
	if k == nil {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	var first error
	if k.history != nil {
		first = k.history.Close()
		k.history = nil
	}
	if k.conversations != nil {
		if err := k.conversations.Close(); err != nil && first == nil {
			first = err
		}
		k.conversations = nil
	}
	return first
}

func openHistory(path string, settings config.HistorySettings) (*history.Store, string) {
	store, err := history.Open(path, settings)
	if err != nil {
		return nil, "the history is not being kept: " + err.Error()
	}
	return store, ""
}

func openChats(path string, settings config.ChatSettings) (*chats.Store, string) {
	store, err := chats.Open(path, settings)
	if err != nil {
		return nil, "conversations are not being kept: " + err.Error()
	}
	return store, ""
}
