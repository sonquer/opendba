// Package chats keeps conversations with the assistant, so that one is still
// there tomorrow.
//
// What is kept is every message, tool results included. A conversation stored
// without them reads back as a transcript and cannot be carried on: the model
// would answer the next question without the rows it read to answer the last
// one. That means the rows an assistant looked at are written to a file, which
// is why keeping them is a setting and why clearing them is a screen.
package chats

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
	"github.com/sonquer/tui4db/src/cli/internal/config"
)

const schema = `
CREATE TABLE IF NOT EXISTS chats (
	id integer PRIMARY KEY AUTOINCREMENT,
	connection_id text NOT NULL,
	connection_name text NOT NULL,
	instance text NOT NULL,
	title text NOT NULL,
	questions integer NOT NULL DEFAULT 0,
	started_at integer NOT NULL,
	updated_at integer NOT NULL
);
CREATE INDEX IF NOT EXISTS chats_updated_idx ON chats (updated_at DESC);
CREATE INDEX IF NOT EXISTS chats_connection_idx ON chats (connection_id, updated_at DESC);
CREATE TABLE IF NOT EXISTS chat_messages (
	chat_id integer NOT NULL,
	position integer NOT NULL,
	role text NOT NULL,
	content text NOT NULL DEFAULT '',
	reasoning text NOT NULL DEFAULT '',
	calls text NOT NULL DEFAULT '',
	result text NOT NULL DEFAULT '',
	PRIMARY KEY (chat_id, position)
);`

// Chat is one conversation: what it was about, and everything that was said.
// Messages is empty in a listing, because a list of conversations does not need
// what is in them.
type Chat struct {
	ID             int64
	ConnectionID   string
	ConnectionName string
	Instance       string
	Title          string
	StartedAt      time.Time
	UpdatedAt      time.Time
	Messages       []ai.Message

	// asked is how many questions were put in this conversation. It is written
	// down rather than counted from the messages, because a listing does not
	// read them and a list that cannot say how long a conversation was is a
	// list of titles.
	asked int
}

// Questions is how many times somebody asked something, which is the length of
// a conversation in the only unit that means anything to the person who had it.
func (c Chat) Questions() int {
	if len(c.Messages) == 0 {
		return c.asked
	}
	asked := 0
	for _, message := range c.Messages {
		if message.Role == ai.RoleUser {
			asked++
		}
	}
	return asked
}

// Snippet is the title cut to a width, for a list that has to fit.
func (c Chat) Snippet(width int) string {
	title := strings.Join(strings.Fields(c.Title), " ")
	if width <= 0 || len([]rune(title)) <= width {
		return title
	}
	return string([]rune(title)[:width-1]) + "…"
}

// Title is what a conversation is called: the first thing that was asked in it,
// cut short. A conversation nobody has asked anything in yet has no name, and
// saying so beats inventing one.
func Title(messages []ai.Message, width int) string {
	for _, message := range messages {
		if message.Role != ai.RoleUser || strings.TrimSpace(message.Content) == "" {
			continue
		}
		return Chat{Title: message.Content}.Snippet(width)
	}
	return "a conversation with nothing in it"
}

type Store struct {
	db       *sql.DB
	settings config.ChatSettings
	now      func() time.Time
}

func Open(path string, settings config.ChatSettings) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create the conversations directory: %w", err)
	}
	database, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(2000)")
	if err != nil {
		return nil, fmt.Errorf("open the conversations: %w", err)
	}
	database.SetMaxOpenConns(1)
	if _, err := database.Exec(schema); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("prepare the conversations: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("protect the conversations: %w", err)
	}
	return &Store{db: database, settings: settings, now: time.Now}, nil
}

func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close the conversations: %w", err)
	}
	return nil
}

// Save writes a conversation and hands back what it is called by. Everything a
// conversation holds is written at once, inside a transaction: saving after
// every turn then costs nothing to get wrong, because a save that fails leaves
// what was already there rather than half of what is there now.
func (s *Store) Save(ctx context.Context, chat Chat) (int64, error) {
	if !s.settings.Enabled || len(chat.Messages) == 0 {
		return chat.ID, nil
	}
	if chat.StartedAt.IsZero() {
		chat.StartedAt = s.now()
	}
	chat.UpdatedAt = s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return chat.ID, fmt.Errorf("save the conversation: %w", err)
	}
	id, err := s.write(ctx, tx, chat)
	if err != nil {
		_ = tx.Rollback()
		return chat.ID, err
	}
	if err := tx.Commit(); err != nil {
		return chat.ID, fmt.Errorf("save the conversation: %w", err)
	}
	return id, s.trim(ctx)
}

func (s *Store) write(ctx context.Context, tx *sql.Tx, chat Chat) (int64, error) {
	id, err := thread(ctx, tx, chat)
	if err != nil {
		return chat.ID, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM chat_messages WHERE chat_id = ?`, id); err != nil {
		return id, fmt.Errorf("save the conversation: %w", err)
	}
	for at, message := range chat.Messages {
		calls, result, err := encode(message)
		if err != nil {
			return id, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO chat_messages
			(chat_id, position, role, content, reasoning, calls, result)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, at, string(message.Role), message.Content, message.Reasoning,
			calls, result); err != nil {
			return id, fmt.Errorf("save a message: %w", err)
		}
	}
	return id, nil
}

// thread inserts or updates the conversation itself and returns its id.
func thread(ctx context.Context, tx *sql.Tx, chat Chat) (int64, error) {
	if chat.ID > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE chats
			SET instance = ?, title = ?, questions = ?, updated_at = ? WHERE id = ?`,
			chat.Instance, chat.Title, chat.Questions(),
			chat.UpdatedAt.UnixMilli(), chat.ID); err != nil {
			return chat.ID, fmt.Errorf("save the conversation: %w", err)
		}
		return chat.ID, nil
	}
	outcome, err := tx.ExecContext(ctx, `INSERT INTO chats
		(connection_id, connection_name, instance, title, questions, started_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		chat.ConnectionID, chat.ConnectionName, chat.Instance, chat.Title,
		chat.Questions(), chat.StartedAt.UnixMilli(), chat.UpdatedAt.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("save the conversation: %w", err)
	}
	id, err := outcome.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("save the conversation: %w", err)
	}
	return id, nil
}

// encode writes the parts of a message that are not text as JSON, which is what
// a map of arguments is and what a column is not.
func encode(message ai.Message) (string, string, error) {
	calls := ""
	if len(message.Calls) > 0 {
		written, err := json.Marshal(message.Calls)
		if err != nil {
			return "", "", fmt.Errorf("save a tool call: %w", err)
		}
		calls = string(written)
	}
	result := ""
	if message.Result != nil {
		written, err := json.Marshal(message.Result)
		if err != nil {
			return "", "", fmt.Errorf("save a tool result: %w", err)
		}
		result = string(written)
	}
	return calls, result, nil
}

// trim drops the conversations past the limit, oldest first.
func (s *Store) trim(ctx context.Context) error {
	if s.settings.Limit <= 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM chat_messages WHERE chat_id NOT IN (
		SELECT id FROM chats ORDER BY updated_at DESC, id DESC LIMIT ?)`, s.settings.Limit); err != nil {
		return fmt.Errorf("trim the conversations: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM chats WHERE id NOT IN (
		SELECT id FROM chats ORDER BY updated_at DESC, id DESC LIMIT ?)`, s.settings.Limit); err != nil {
		return fmt.Errorf("trim the conversations: %w", err)
	}
	return nil
}

// listing reads a conversation without what was said in it. Every listing is
// ordered by when it was last touched and then by id, because two conversations
// saved in the same millisecond are a tie, and a tie broken differently on every
// read is a list that shuffles itself while somebody is looking at it.
const listing = `SELECT id, connection_id, connection_name, instance, title, questions, started_at, updated_at
	FROM chats`

func (s *Store) Recent(ctx context.Context, connectionID string, limit int) ([]Chat, error) {
	query := listing
	args := []any{}
	if connectionID != "" {
		query += " WHERE connection_id = ?"
		args = append(args, connectionID)
	}
	query += " ORDER BY updated_at DESC, id DESC LIMIT ?"
	return s.list(ctx, query, append(args, pageSize(limit))...)
}

func (s *Store) Search(ctx context.Context, term string, limit int) ([]Chat, error) {
	query := listing + " WHERE title LIKE ? ORDER BY updated_at DESC, id DESC LIMIT ?"
	return s.list(ctx, query, "%"+term+"%", pageSize(limit))
}

func (s *Store) list(ctx context.Context, query string, args ...any) ([]Chat, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read the conversations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var found []Chat
	for rows.Next() {
		var (
			chat      Chat
			startedAt int64
			updatedAt int64
		)
		if err := rows.Scan(&chat.ID, &chat.ConnectionID, &chat.ConnectionName,
			&chat.Instance, &chat.Title, &chat.asked, &startedAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("read a conversation: %w", err)
		}
		chat.StartedAt = time.UnixMilli(startedAt)
		chat.UpdatedAt = time.UnixMilli(updatedAt)
		found = append(found, chat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the conversations: %w", err)
	}
	return found, nil
}

// Load reads one conversation with everything that was said in it.
func (s *Store) Load(ctx context.Context, id int64) (Chat, error) {
	found, err := s.list(ctx, listing+" WHERE id = ?", id)
	if err != nil {
		return Chat{}, err
	}
	if len(found) == 0 {
		return Chat{}, fmt.Errorf("there is no conversation %d", id)
	}
	chat := found[0]
	chat.Messages, err = s.messages(ctx, id)
	if err != nil {
		return Chat{}, err
	}
	return chat, nil
}

func (s *Store) messages(ctx context.Context, id int64) ([]ai.Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT role, content, reasoning, calls, result
		FROM chat_messages WHERE chat_id = ? ORDER BY position`, id)
	if err != nil {
		return nil, fmt.Errorf("read the conversation: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var said []ai.Message
	for rows.Next() {
		var (
			message ai.Message
			role    string
			calls   string
			result  string
		)
		if err := rows.Scan(&role, &message.Content, &message.Reasoning,
			&calls, &result); err != nil {
			return nil, fmt.Errorf("read a message: %w", err)
		}
		message.Role = ai.Role(role)
		if err := decode(&message, calls, result); err != nil {
			return nil, err
		}
		said = append(said, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the conversation: %w", err)
	}
	return said, nil
}

func decode(message *ai.Message, calls, result string) error {
	if calls != "" {
		if err := json.Unmarshal([]byte(calls), &message.Calls); err != nil {
			return fmt.Errorf("read a tool call: %w", err)
		}
	}
	if result != "" {
		if err := json.Unmarshal([]byte(result), &message.Result); err != nil {
			return fmt.Errorf("read a tool result: %w", err)
		}
	}
	return nil
}

// Remove throws one conversation away.
func (s *Store) Remove(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM chat_messages WHERE chat_id = ?`, id); err != nil {
		return fmt.Errorf("remove the conversation: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM chats WHERE id = ?`, id); err != nil {
		return fmt.Errorf("remove the conversation: %w", err)
	}
	return nil
}

// Clear throws all of them away.
func (s *Store) Clear(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM chat_messages`); err != nil {
		return fmt.Errorf("clear the conversations: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM chats`); err != nil {
		return fmt.Errorf("clear the conversations: %w", err)
	}
	return nil
}

func (s *Store) Count(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM chats`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count the conversations: %w", err)
	}
	return count, nil
}

func pageSize(limit int) int {
	if limit <= 0 {
		return 50
	}
	return limit
}
