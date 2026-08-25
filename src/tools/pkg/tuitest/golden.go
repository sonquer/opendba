package tuitest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GoldenPath is where the frame for one screen at one size is kept. Goldens is
// resolved against the repository rather than against the suite, because the
// screens belong to the product and the suite only describes how to reach them.
func (s Suite) GoldenPath(size Size, shot string) string {
	return filepath.Join(s.Goldens, size.String(), shot+".txt")
}

// Compare reads the frame that was kept for a screen and reports how it differs
// from the one just drawn. It returns an empty string when they agree.
func Compare(path, drawn string) (string, error) {
	kept, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("no screen has been kept at %s yet", path), nil
		}
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	before := strings.TrimRight(string(kept), "\n")
	if before == drawn {
		return "", nil
	}
	return Difference(before, drawn), nil
}

// Write keeps a frame as the one to compare against next time.
func Write(path, drawn string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("make room for %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(drawn+"\n"), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Difference shows the lines that moved, with the line numbers, which is what
// somebody looking at a broken screen needs.
func Difference(before, after string) string {
	was := strings.Split(before, "\n")
	now := strings.Split(after, "\n")
	var lines []string
	for i := range max(len(was), len(now)) {
		left, right := at(was, i), at(now, i)
		if left == right {
			continue
		}
		lines = append(lines, fmt.Sprintf("%4d - %s", i+1, left))
		lines = append(lines, fmt.Sprintf("%4d + %s", i+1, right))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}
