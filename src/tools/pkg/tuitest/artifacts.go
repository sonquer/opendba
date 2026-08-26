package tuitest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/sonquer/opendba/src/tools/pkg/tuitest/shot"
)

// Grid is the screen as cells, which is what a picture of it is drawn from.
func (s *Session) Grid() [][]shot.Cell {
	width, height := s.term.Width(), s.term.Height()
	grid := make([][]shot.Cell, 0, height)
	for y := range height {
		row := make([]shot.Cell, 0, width)
		for x := range width {
			row = append(row, translate(s.term.CellAt(x, y)))
		}
		grid = append(grid, row)
	}
	return grid
}

func translate(cell *uv.Cell) shot.Cell {
	if cell == nil {
		return shot.Cell{Content: " "}
	}
	return shot.Cell{
		Content:   cell.Content,
		Fg:        cell.Style.Fg,
		Bg:        cell.Style.Bg,
		Bold:      cell.Style.Attrs&uv.AttrBold != 0,
		Underline: cell.Style.Underline != uv.UnderlineNone,
	}
}

// Keep writes everything somebody looking at a broken screen needs: the frame
// as text, the frame in the colours it had, a picture of it, and a recording of
// the whole run.
func (r Result) Keep(dir string, grid [][]shot.Cell, stamp time.Time) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("make room for the artifacts: %w", err)
	}
	files := map[string]string{
		"frame.txt": r.Frame.Plain() + "\n",
		"frame.ans": r.Frame.Styled,
		"steps.log": r.log(),
	}
	if detail := r.diff(); detail != "" {
		files["diff.txt"] = detail
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	if err := r.picture(filepath.Join(dir, "frame.svg"), grid); err != nil {
		return err
	}
	return r.recording(filepath.Join(dir, "session.cast"), stamp)
}

func (r Result) picture(path string, grid [][]shot.Cell) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("write the picture: %w", err)
	}
	defer func() { _ = file.Close() }()
	return shot.SVG(file, grid, nil, nil)
}

func (r Result) recording(path string, stamp time.Time) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("write the recording: %w", err)
	}
	defer func() { _ = file.Close() }()
	frames := make([]shot.Frame, 0, len(r.Chunks))
	for _, chunk := range r.Chunks {
		frames = append(frames, shot.Frame{At: chunk.At, Data: chunk.Data})
	}
	title := r.Scenario + " at " + r.Size.String()
	return shot.Cast(file, title, r.Size.Width, r.Size.Height, stamp, frames)
}

func (r Result) log() string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s at %s, exit %d, %s\n", r.Scenario, r.Size, r.Exit, r.Elapsed.Round(time.Millisecond))
	for _, failure := range r.Failures {
		fmt.Fprintln(&out, failure.String())
	}
	return out.String()
}

func (r Result) diff() string {
	var out strings.Builder
	for _, failure := range r.Failures {
		if failure.Action == "shot" && failure.Detail != "" {
			fmt.Fprintf(&out, "%s\n%s\n", failure.Reason, failure.Detail)
		}
	}
	return out.String()
}
