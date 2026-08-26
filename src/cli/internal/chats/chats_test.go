package chats

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sonquer/opendba/src/cli/internal/ai"
	"github.com/sonquer/opendba/src/cli/internal/config"
)

func kept(t *testing.T, settings config.ChatSettings) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "chats.db"), settings)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func on() config.ChatSettings { return config.ChatSettings{Enabled: true, Limit: 100} }

// said is a conversation with every shape of message in it: a question, an
// answer that reasoned, a tool call, and the result the tool gave back.
func said() []ai.Message {
	return []ai.Message{
		{Role: ai.RoleUser, Content: "how many users are there?"},
		{
			Role:      ai.RoleAssistant,
			Content:   "I will count them.",
			Reasoning: "counting is a select",
			Calls: []ai.ToolCall{{
				ID:        "call-1",
				Name:      "run_select",
				Arguments: map[string]any{"sql": "SELECT count(*) FROM users", "limit": float64(10)},
			}},
		},
		{
			Role:   ai.RoleTool,
			Result: &ai.ToolResult{ID: "call-1", Name: "run_select", Content: "count\n2"},
		},
		{Role: ai.RoleAssistant, Content: "There are two."},
	}
}

func chatWith(messages []ai.Message) Chat {
	return Chat{
		ConnectionID:   "production-eu",
		ConnectionName: "production-eu",
		Instance:       "claude",
		Title:          Title(messages, 60),
		Messages:       messages,
	}
}

// A conversation comes back exactly as it went in, tool results and all,
// because anything less reads back but cannot be carried on.
func TestAConversationComesBackWhole(t *testing.T) {
	store := kept(t, on())
	id, err := store.Save(context.Background(), chatWith(said()))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id == 0 {
		t.Fatal("a saved conversation must have something to call it by")
	}

	loaded, err := store.Load(context.Background(), id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Title != "how many users are there?" {
		t.Errorf("title = %q, a conversation is called after the first thing asked",
			loaded.Title)
	}
	if loaded.ConnectionName != "production-eu" || loaded.Instance != "claude" {
		t.Errorf("chat = %+v, it must remember where and who", loaded)
	}
	if len(loaded.Messages) != len(said()) {
		t.Fatalf("messages = %d, want %d", len(loaded.Messages), len(said()))
	}
	for i, want := range said() {
		got := loaded.Messages[i]
		if got.Role != want.Role || got.Content != want.Content ||
			got.Reasoning != want.Reasoning {
			t.Errorf("message %d = %+v, want %+v", i, got, want)
		}
	}
	call := loaded.Messages[1].Calls
	if len(call) != 1 || call[0].Name != "run_select" ||
		call[0].Arguments["sql"] != "SELECT count(*) FROM users" {
		t.Errorf("calls = %+v, a tool call must survive whole", call)
	}
	result := loaded.Messages[2].Result
	if result == nil || result.Content != "count\n2" {
		t.Errorf("result = %+v, the rows the model saw are the point", result)
	}
	if loaded.Questions() != 1 {
		t.Errorf("questions = %d, want 1", loaded.Questions())
	}
}

// Saving the same conversation again replaces what was there rather than
// writing it twice, which is what saving after every turn needs.
func TestSavingAgainReplacesRatherThanRepeats(t *testing.T) {
	store := kept(t, on())
	chat := chatWith(said())
	id, err := store.Save(context.Background(), chat)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	chat.ID = id
	chat.Messages = append(chat.Messages,
		ai.Message{Role: ai.RoleUser, Content: "and how many orders?"})
	again, err := store.Save(context.Background(), chat)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if again != id {
		t.Errorf("id = %d, want the same conversation %d", again, id)
	}

	count, err := store.Count(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("count = %d (%v), saving again must not make a second one", count, err)
	}
	loaded, err := store.Load(context.Background(), id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Messages) != 5 {
		t.Errorf("messages = %d, want 5", len(loaded.Messages))
	}
	if loaded.Questions() != 2 {
		t.Errorf("questions = %d, want 2", loaded.Questions())
	}
}

// A listing says what a conversation was without reading what was in it.
func TestAListingLeavesTheMessagesAlone(t *testing.T) {
	store := kept(t, on())
	for _, title := range []string{"about users", "about orders"} {
		chat := chatWith([]ai.Message{{Role: ai.RoleUser, Content: title}})
		if _, err := store.Save(context.Background(), chat); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	elsewhere := chatWith([]ai.Message{{Role: ai.RoleUser, Content: "about staging"}})
	elsewhere.ConnectionID = "staging"
	if _, err := store.Save(context.Background(), elsewhere); err != nil {
		t.Fatalf("Save: %v", err)
	}

	here, err := store.Recent(context.Background(), "production-eu", 50)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(here) != 2 {
		t.Fatalf("conversations = %d, a listing is about one connection", len(here))
	}
	if here[0].Title != "about orders" {
		t.Errorf("first = %q, the newest comes first", here[0].Title)
	}
	if len(here[0].Messages) != 0 {
		t.Error("a listing must not read what is in them")
	}

	all, err := store.Recent(context.Background(), "", 50)
	if err != nil || len(all) != 3 {
		t.Errorf("all = %d (%v), no connection means every one", len(all), err)
	}

	found, err := store.Search(context.Background(), "orders", 50)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(found) != 1 || found[0].Title != "about orders" {
		t.Errorf("found = %+v", found)
	}
}

// One conversation can be thrown away, and so can all of them.
func TestAConversationCanBeThrownAway(t *testing.T) {
	store := kept(t, on())
	first, err := store.Save(context.Background(),
		chatWith([]ai.Message{{Role: ai.RoleUser, Content: "one"}}))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := store.Save(context.Background(),
		chatWith([]ai.Message{{Role: ai.RoleUser, Content: "two"}})); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := store.Remove(context.Background(), first); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if count, _ := store.Count(context.Background()); count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if _, err := store.Load(context.Background(), first); err == nil {
		t.Error("a conversation that was removed is not there")
	}

	if err := store.Clear(context.Background()); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	count, err := store.Count(context.Background())
	if err != nil || count != 0 {
		t.Errorf("count = %d (%v), clearing leaves none", count, err)
	}
}

// Past the limit the oldest conversations go, and their messages go with them.
func TestPastTheLimitTheOldestGo(t *testing.T) {
	store := kept(t, config.ChatSettings{Enabled: true, Limit: 2})
	moment := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	store.now = func() time.Time {
		moment = moment.Add(time.Minute)
		return moment
	}
	for _, title := range []string{"first", "second", "third"} {
		if _, err := store.Save(context.Background(),
			chatWith([]ai.Message{{Role: ai.RoleUser, Content: title}})); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	found, err := store.Recent(context.Background(), "", 50)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("conversations = %d, the limit is 2", len(found))
	}
	if found[0].Title != "third" || found[1].Title != "second" {
		t.Errorf("kept = %q and %q, the oldest goes", found[0].Title, found[1].Title)
	}
	var orphans int
	if err := store.db.QueryRow(`SELECT count(*) FROM chat_messages
		WHERE chat_id NOT IN (SELECT id FROM chats)`).Scan(&orphans); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if orphans != 0 {
		t.Errorf("orphans = %d, the messages go with the conversation", orphans)
	}
}

// A limit of nothing keeps everything.
func TestNoLimitKeepsEverything(t *testing.T) {
	store := kept(t, config.ChatSettings{Enabled: true})
	for _, title := range []string{"first", "second", "third"} {
		if _, err := store.Save(context.Background(),
			chatWith([]ai.Message{{Role: ai.RoleUser, Content: title}})); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	if count, _ := store.Count(context.Background()); count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

// A store that has been told not to keep anything keeps nothing, and says so by
// changing nothing rather than by failing.
func TestAStoreToldNotToKeepAnythingKeepsNothing(t *testing.T) {
	store := kept(t, config.ChatSettings{Enabled: false, Limit: 100})
	id, err := store.Save(context.Background(), chatWith(said()))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id != 0 {
		t.Errorf("id = %d, nothing was written", id)
	}
	if count, _ := store.Count(context.Background()); count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

// A conversation with nothing said in it is not a conversation.
func TestAnEmptyConversationIsNotSaved(t *testing.T) {
	store := kept(t, on())
	id, err := store.Save(context.Background(), chatWith(nil))
	if err != nil || id != 0 {
		t.Errorf("Save = %d (%v), there is nothing to keep", id, err)
	}
	if count, _ := store.Count(context.Background()); count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

// A conversation is called after the first thing asked in it, cut to fit.
func TestAConversationIsCalledAfterTheFirstQuestion(t *testing.T) {
	long := strings.Repeat("x", 200)
	for _, want := range []struct {
		name     string
		messages []ai.Message
		title    string
	}{
		{"nothing said", nil, "a conversation with nothing in it"},
		{
			name:     "only the assistant",
			messages: []ai.Message{{Role: ai.RoleAssistant, Content: "hello"}},
			title:    "a conversation with nothing in it",
		},
		{
			name:     "a question",
			messages: []ai.Message{{Role: ai.RoleUser, Content: "how many users?"}},
			title:    "how many users?",
		},
		{
			name: "the first of two",
			messages: []ai.Message{
				{Role: ai.RoleUser, Content: "first"},
				{Role: ai.RoleUser, Content: "second"},
			},
			title: "first",
		},
		{
			name:     "wrapped over lines",
			messages: []ai.Message{{Role: ai.RoleUser, Content: "how many\n  users?"}},
			title:    "how many users?",
		},
		{
			name:     "blank",
			messages: []ai.Message{{Role: ai.RoleUser, Content: "   "}},
			title:    "a conversation with nothing in it",
		},
		{
			name:     "far too long",
			messages: []ai.Message{{Role: ai.RoleUser, Content: long}},
			title:    strings.Repeat("x", 59) + "…",
		},
	} {
		t.Run(want.name, func(t *testing.T) {
			if got := Title(want.messages, 60); got != want.title {
				t.Errorf("Title = %q, want %q", got, want.title)
			}
		})
	}
	if got := (Chat{Title: "short"}).Snippet(0); got != "short" {
		t.Errorf("Snippet = %q, no width means no cutting", got)
	}
}

// A conversation store is not readable by anyone else, and one that cannot be
// made says why.
func TestTheConversationsAreNotReadableByAnyoneElse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chats.db")
	store, err := Open(path, on())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()
	held, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if runtime.GOOS != "windows" && held.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, the rows a model read are in here", held.Mode().Perm())
	}
}

func TestAStoreThatCannotBeOpenedSaysWhy(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Open(filepath.Join(blocked, "chats.db"), on()); err == nil {
		t.Error("a directory that cannot be made is not somewhere to keep anything")
	}
}

// A conversation that is not there is not a conversation.
func TestAConversationThatIsNotThere(t *testing.T) {
	store := kept(t, on())
	if _, err := store.Load(context.Background(), 42); err == nil {
		t.Error("want an error")
	}
}

// A page of nothing is a page of fifty.
func TestAPageOfNothingIsAPageOfFifty(t *testing.T) {
	for _, want := range []struct{ asked, given int }{
		{0, 50}, {-1, 50}, {10, 10},
	} {
		if got := pageSize(want.asked); got != want.given {
			t.Errorf("pageSize(%d) = %d, want %d", want.asked, got, want.given)
		}
	}
}

// A closed store says so rather than pretending.
func TestAClosedStoreSaysSo(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "chats.db"), on())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := store.Save(context.Background(), chatWith(said())); err == nil {
		t.Error("Save must say the store is closed")
	}
	if _, err := store.Recent(context.Background(), "", 10); err == nil {
		t.Error("Recent must say so too")
	}
	if _, err := store.Load(context.Background(), 1); err == nil {
		t.Error("and Load")
	}
	if _, err := store.Count(context.Background()); err == nil {
		t.Error("and Count")
	}
	if err := store.Remove(context.Background(), 1); err == nil {
		t.Error("and Remove")
	}
	if err := store.Clear(context.Background()); err == nil {
		t.Error("and Clear")
	}
}

// A tool call whose arguments cannot be written down stops the save rather than
// writing half a conversation.
func TestAToolCallThatCannotBeWrittenStopsTheSave(t *testing.T) {
	store := kept(t, on())
	chat := chatWith([]ai.Message{
		{Role: ai.RoleUser, Content: "go on"},
		{Role: ai.RoleAssistant, Calls: []ai.ToolCall{{
			ID: "call-1", Name: "run_select",
			Arguments: map[string]any{"sql": make(chan int)},
		}}},
	})
	if _, err := store.Save(context.Background(), chat); err == nil {
		t.Fatal("want an error")
	}
	if count, _ := store.Count(context.Background()); count != 0 {
		t.Errorf("count = %d, a save that failed leaves nothing behind", count)
	}
}

// A conversation on disk that has been damaged says so rather than coming back
// as something else.
func TestADamagedConversationSaysSo(t *testing.T) {
	for _, want := range []struct {
		name          string
		calls, result string
	}{
		{"a broken tool call", "{not json", ""},
		{"a broken tool result", "", "{not json"},
	} {
		t.Run(want.name, func(t *testing.T) {
			store := kept(t, on())
			id, err := store.Save(context.Background(),
				chatWith([]ai.Message{{Role: ai.RoleUser, Content: "hello"}}))
			if err != nil {
				t.Fatalf("Save: %v", err)
			}
			if _, err := store.db.Exec(
				`UPDATE chat_messages SET calls = ?, result = ? WHERE chat_id = ?`,
				want.calls, want.result, id); err != nil {
				t.Fatalf("damage it: %v", err)
			}
			if _, err := store.Load(context.Background(), id); err == nil {
				t.Error("want an error")
			}
		})
	}
}

// Closing a store that is already closed says so.
func TestClosingTwiceSaysSo(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "chats.db"), on())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Log("closing twice is quiet on this platform")
	}
}

// A store whose file cannot be protected does not pretend it was.
func TestAStoreThatCannotBeProtectedSaysSo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not enforced on windows")
	}
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.MkdirAll(locked, 0o500); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if _, err := Open(filepath.Join(locked, "chats.db"), on()); err == nil {
		t.Error("a file that cannot be made is not a store")
	}
}

// Trimming a store that has gone away says so rather than quietly keeping
// everything.
func TestTrimmingAStoreThatHasGoneAwaySaysSo(t *testing.T) {
	store := kept(t, config.ChatSettings{Enabled: true, Limit: 1})
	if _, err := store.Save(context.Background(),
		chatWith([]ai.Message{{Role: ai.RoleUser, Content: "one"}})); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := store.trim(context.Background()); err == nil {
		t.Error("want an error")
	}
}

// damaged is a store somebody has taken a table out of, which is how the second
// statement of a two-statement job is made to fail while the first succeeds.
func damaged(t *testing.T, table string) *Store {
	t.Helper()
	store := kept(t, on())
	if _, err := store.Save(context.Background(), chatWith(said())); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := store.db.Exec("DROP TABLE " + table); err != nil {
		t.Fatalf("drop %s: %v", table, err)
	}
	return store
}

// A store that has been damaged says so at every turn rather than reporting
// success for the half of the job that still works.
func TestADamagedStoreSaysSo(t *testing.T) {
	for _, want := range []struct {
		name  string
		table string
		run   func(*Store) error
	}{
		{"removing one", "chats", func(s *Store) error {
			return s.Remove(context.Background(), 1)
		}},
		{"clearing them", "chats", func(s *Store) error {
			return s.Clear(context.Background())
		}},
		{"saving the messages", "chat_messages", func(s *Store) error {
			_, err := s.Save(context.Background(), chatWith(said()))
			return err
		}},
		{"saving over one", "chats", func(s *Store) error {
			chat := chatWith(said())
			chat.ID = 1
			_, err := s.Save(context.Background(), chat)
			return err
		}},
		{"listing them", "chats", func(s *Store) error {
			_, err := s.Recent(context.Background(), "", 10)
			return err
		}},
		{"reading one", "chat_messages", func(s *Store) error {
			_, err := s.Load(context.Background(), 1)
			return err
		}},
		{"counting them", "chats", func(s *Store) error {
			_, err := s.Count(context.Background())
			return err
		}},
	} {
		t.Run(want.name, func(t *testing.T) {
			if err := want.run(damaged(t, want.table)); err == nil {
				t.Error("want an error")
			}
		})
	}
}

// A file that is not a database is not a store.
func TestAFileThatIsNotADatabaseIsNotAStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chats.db")
	if err := os.WriteFile(path, []byte("this is not a database"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Open(path, on()); err == nil {
		t.Error("want an error")
	}
}

// Settings are a fact of the file rather than of one connection, so a store
// shared by several of them answers to what the settings say now.
func TestAStoreFollowsTheSettings(t *testing.T) {
	shared := kept(t, config.ChatSettings{Enabled: true, Limit: 10})
	same := shared.Following(config.ChatSettings{Enabled: true, Limit: 99})
	if same != shared {
		t.Error("following the settings must not open a second handle")
	}
	if shared.settings.Limit != 99 {
		t.Errorf("settings = %+v", shared.settings)
	}
}
