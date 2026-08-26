package tuitest

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

// Seed builds a database at path by running the statements in a .sql file,
// replacing whatever was there before.
func Seed(path, script string) error {
	statements, err := os.ReadFile(script)
	if err != nil {
		return fmt.Errorf("read the seed %s: %w", script, err)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("clear %s: %w", path, err)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() { _ = database.Close() }()
	if _, err := database.Exec(string(statements)); err != nil {
		return fmt.Errorf("run the seed %s: %w", script, err)
	}
	return nil
}
