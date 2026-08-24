package history

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/sonquer/opendba/src/cli/internal/config"
)

const redactedStatement = "(not recorded)"

// Redacted is what stands in for a statement when the settings say to record
// that something ran without recording what it was.
func Redacted() string { return redactedStatement }

const schema = `
CREATE TABLE IF NOT EXISTS queries (
	id integer PRIMARY KEY AUTOINCREMENT,
	connection_id text NOT NULL,
	connection_name text NOT NULL,
	statement text NOT NULL,
	kind text NOT NULL,
	rows integer NOT NULL DEFAULT 0,
	duration_ms integer NOT NULL DEFAULT 0,
	failure text NOT NULL DEFAULT '',
	favorite integer NOT NULL DEFAULT 0,
	ran_at integer NOT NULL
);
CREATE INDEX IF NOT EXISTS queries_ran_at_idx ON queries (ran_at DESC);
CREATE INDEX IF NOT EXISTS queries_connection_idx ON queries (connection_id, ran_at DESC);`

type Entry struct {
	ID             int64
	ConnectionID   string
	ConnectionName string
	Statement      string
	Kind           string
	Rows           int
	Duration       time.Duration
	Failure        string
	Favorite       bool
	RanAt          time.Time
}

func (e Entry) Succeeded() bool { return e.Failure == "" }

func (e Entry) Snippet(width int) string {
	statement := strings.Join(strings.Fields(e.Statement), " ")
	if width <= 0 || len(statement) <= width {
		return statement
	}
	return statement[:width-1] + "…"
}

type Store struct {
	db       *sql.DB
	settings config.HistorySettings
	now      func() time.Time
}

func Open(path string, settings config.HistorySettings) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create the history directory: %w", err)
	}
	database, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(2000)")
	if err != nil {
		return nil, fmt.Errorf("open the history: %w", err)
	}
	database.SetMaxOpenConns(1)
	if _, err := database.Exec(schema); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("prepare the history: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("protect the history: %w", err)
	}
	return &Store{db: database, settings: settings, now: time.Now}, nil
}

func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close the history: %w", err)
	}
	return nil
}

func (s *Store) Record(ctx context.Context, entry Entry) error {
	if !s.settings.Enabled {
		return nil
	}
	statement := entry.Statement
	if !s.settings.StoreSQL {
		statement = redactedStatement
	}
	if entry.RanAt.IsZero() {
		entry.RanAt = s.now()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO queries
		(connection_id, connection_name, statement, kind, rows, duration_ms, failure, favorite, ran_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ConnectionID, entry.ConnectionName, statement, entry.Kind, entry.Rows,
		entry.Duration.Milliseconds(), entry.Failure, boolean(entry.Favorite), entry.RanAt.UnixMilli())
	if err != nil {
		return fmt.Errorf("record the query: %w", err)
	}
	return s.trim(ctx)
}

func (s *Store) trim(ctx context.Context) error {
	if s.settings.Limit <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM queries WHERE favorite = 0 AND id NOT IN (
		SELECT id FROM queries ORDER BY favorite DESC, ran_at DESC LIMIT ?)`, s.settings.Limit)
	if err != nil {
		return fmt.Errorf("trim the history: %w", err)
	}
	return nil
}

func (s *Store) Recent(ctx context.Context, connectionID string, limit int) ([]Entry, error) {
	query := `SELECT id, connection_id, connection_name, statement, kind, rows, duration_ms, failure, favorite, ran_at
		FROM queries`
	args := []any{}
	if connectionID != "" {
		query += " WHERE connection_id = ?"
		args = append(args, connectionID)
	}
	query += " ORDER BY ran_at DESC LIMIT ?"
	args = append(args, pageSize(limit))
	return s.list(ctx, query, args...)
}

func (s *Store) Search(ctx context.Context, term string, limit int) ([]Entry, error) {
	query := `SELECT id, connection_id, connection_name, statement, kind, rows, duration_ms, failure, favorite, ran_at
		FROM queries WHERE statement LIKE ? ORDER BY ran_at DESC LIMIT ?`
	return s.list(ctx, query, "%"+term+"%", pageSize(limit))
}

func (s *Store) Favorites(ctx context.Context, limit int) ([]Entry, error) {
	query := `SELECT id, connection_id, connection_name, statement, kind, rows, duration_ms, failure, favorite, ran_at
		FROM queries WHERE favorite = 1 ORDER BY ran_at DESC LIMIT ?`
	return s.list(ctx, query, pageSize(limit))
}

func (s *Store) list(ctx context.Context, query string, args ...any) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read the history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []Entry
	for rows.Next() {
		var (
			entry      Entry
			durationMS int64
			favorite   int
			ranAt      int64
		)
		if err := rows.Scan(&entry.ID, &entry.ConnectionID, &entry.ConnectionName, &entry.Statement,
			&entry.Kind, &entry.Rows, &durationMS, &entry.Failure, &favorite, &ranAt); err != nil {
			return nil, fmt.Errorf("read a history entry: %w", err)
		}
		entry.Duration = time.Duration(durationMS) * time.Millisecond
		entry.Favorite = favorite == 1
		entry.RanAt = time.UnixMilli(ranAt)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the history: %w", err)
	}
	return entries, nil
}

func (s *Store) SetFavorite(ctx context.Context, id int64, favorite bool) error {
	if _, err := s.db.ExecContext(ctx, "UPDATE queries SET favorite = ? WHERE id = ?", boolean(favorite), id); err != nil {
		return fmt.Errorf("mark the query: %w", err)
	}
	return nil
}

func (s *Store) Clear(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM queries"); err != nil {
		return fmt.Errorf("clear the history: %w", err)
	}
	return nil
}

func (s *Store) Count(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM queries").Scan(&count); err != nil {
		return 0, fmt.Errorf("count the history: %w", err)
	}
	return count, nil
}

func pageSize(limit int) int {
	if limit <= 0 {
		return 50
	}
	return limit
}

func boolean(value bool) int {
	if value {
		return 1
	}
	return 0
}
