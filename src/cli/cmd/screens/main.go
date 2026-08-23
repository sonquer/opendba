// Command screens renders the interface without a terminal, so a change can be
// looked at rather than guessed at. It builds the same model the program runs,
// feeds it keys, and prints what it draws.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	_ "modernc.org/sqlite"

	"github.com/sonquer/tui4db/src/cli/internal/app"
	"github.com/sonquer/tui4db/src/cli/internal/cli"
	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
	"github.com/sonquer/tui4db/src/cli/pkg/sqldialect"
)

const (
	settle      = 400 * time.Millisecond
	fixtureName = "screens.db"
)

type options struct {
	connection string
	file       string
	size       string
	keys       string
	out        string
	plain      bool
	bars       bool
}

func main() {
	var opts options
	flag.StringVar(&opts.connection, "connection", "", "profile to open; a seeded fixture when empty")
	flag.StringVar(&opts.file, "file", "", "sqlite file to open instead of a profile")
	flag.StringVar(&opts.size, "size", "120x36", "terminal size to render at")
	flag.StringVar(&opts.keys, "keys", "", "keys to press, comma separated, with shot:name where a screen is wanted")
	flag.StringVar(&opts.out, "out", "", "directory to write the screens into instead of standard output")
	flag.BoolVar(&opts.plain, "plain", false, "strip the styling")
	flag.BoolVar(&opts.bars, "bars", false, "print every bar style side by side and stop")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(opts options) error {
	width, height, err := size(opts.size)
	if err != nil {
		return err
	}
	if opts.bars {
		fmt.Print(catalogue(opts.plain))
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	session, workspace, closer, err := open(ctx, opts)
	if err != nil {
		return err
	}
	defer closer()

	model := tea.Model(app.NewModel(session, workspace).Settled())
	model = settled(model, model.Init())
	model, _ = model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	model = settled(model, nil)

	shots := map[string]string{}
	order := []string{}
	for _, key := range strings.Split(opts.keys, ",") {
		key = strings.TrimSpace(key)
		switch {
		case key == "":
			continue
		case strings.HasPrefix(key, "shot:"):
			name := strings.TrimPrefix(key, "shot:")
			shots[name] = draw(model, opts.plain)
			order = append(order, name)
		default:
			var cmd tea.Cmd
			model, cmd = model.Update(press(key))
			model = settled(model, cmd)
		}
	}
	if len(order) == 0 {
		shots["screen"] = draw(model, opts.plain)
		order = append(order, "screen")
	}
	return write(opts.out, order, shots)
}

func write(dir string, order []string, shots map[string]string) error {
	if dir == "" {
		for _, name := range order {
			fmt.Printf("=== %s ===\n%s\n", name, shots[name])
		}
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, name := range order {
		path := filepath.Join(dir, name+".txt")
		if err := os.WriteFile(path, []byte(shots[name]+"\n"), 0o644); err != nil {
			return err
		}
		fmt.Println("wrote", path)
	}
	return nil
}

func draw(model tea.Model, plain bool) string {
	view := model.View()
	content := view.Content
	if plain {
		return ansi.Strip(content)
	}
	return content
}

// settled runs the commands a model returns until they stop answering, which is
// how a screen reaches the state it would have in front of someone.
func settled(model tea.Model, cmd tea.Cmd) tea.Model {
	pending := []tea.Cmd{cmd}
	for round := 0; round < 8 && len(pending) > 0; round++ {
		var next []tea.Cmd
		for _, current := range pending {
			for _, msg := range answers(current) {
				updated, produced := model.Update(msg)
				model = updated
				if produced != nil {
					next = append(next, produced)
				}
			}
		}
		pending = next
	}
	return model
}

// answers runs one command and collects what it produced, giving up on the ones
// that are waiting for time to pass.
func answers(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		batch, ok := msg.(tea.BatchMsg)
		if !ok {
			return []tea.Msg{msg}
		}
		var produced []tea.Msg
		for _, sub := range batch {
			produced = append(produced, answers(sub)...)
		}
		return produced
	case <-time.After(settle):
		return nil
	}
}

func press(name string) tea.KeyPressMsg {
	named := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab,
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		"space": tea.KeySpace, "backspace": tea.KeyBackspace,
		"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "f5": tea.KeyF5,
	}
	msg := tea.KeyPressMsg{}
	parts := strings.Split(name, "+")
	for _, part := range parts[:len(parts)-1] {
		switch part {
		case "ctrl":
			msg.Mod |= tea.ModCtrl
		case "alt":
			msg.Mod |= tea.ModAlt
		case "shift":
			msg.Mod |= tea.ModShift
		}
	}
	last := parts[len(parts)-1]
	if code, ok := named[last]; ok {
		msg.Code = code
		if last == "space" {
			msg.Text = " "
		}
		return msg
	}
	for _, r := range last {
		msg.Code = r
		break
	}
	if msg.Mod == 0 {
		msg.Text = last
	}
	return msg
}

func size(value string) (int, int, error) {
	width, height, found := strings.Cut(value, "x")
	if !found {
		return 0, 0, fmt.Errorf("a size looks like 120x36, not %q", value)
	}
	columns, err := strconv.Atoi(width)
	if err != nil {
		return 0, 0, fmt.Errorf("read the width: %w", err)
	}
	rows, err := strconv.Atoi(height)
	if err != nil {
		return 0, 0, fmt.Errorf("read the height: %w", err)
	}
	return columns, rows, nil
}

func open(ctx context.Context, opts options) (cli.Session, cli.Workspace, func(), error) {
	store, err := config.Open()
	if err != nil {
		return cli.Session{}, nil, nil, err
	}
	application := cli.App{
		Store:    store,
		Registry: cli.Registry(),
		Secrets:  cli.Secrets(store.Paths, app.PassphrasePrompt),
		Dialects: sqldialect.Default(),
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Version:  "screens",
	}
	if opts.connection != "" {
		session, cleanup, err := application.Open(ctx, opts.connection)
		return session, application, cleanup, err
	}

	file := opts.file
	if file == "" {
		file, err = fixture()
		if err != nil {
			return cli.Session{}, nil, nil, err
		}
	}
	fixed, err := fixedStore(store, file)
	if err != nil {
		return cli.Session{}, nil, nil, err
	}
	application.Store = fixed
	session, cleanup, err := application.Open(ctx, "screens")
	return session, application, cleanup, err
}

// fixedStore keeps the fixture profile out of the real configuration, so
// rendering screens never writes to what the person is using.
func fixedStore(store config.Store, file string) (config.Store, error) {
	paths := config.Paths{Config: filepath.Join(os.TempDir(), "tui4db-screens"), State: store.Paths.State}
	if err := paths.Ensure(); err != nil {
		return config.Store{}, err
	}
	fixed := config.NewStore(paths)
	profiles := config.Profiles{}
	if err := profiles.Upsert(config.Connection{
		ID:     "screens",
		Name:   "screens",
		Driver: "sqlite",
		File:   file,
		Mode:   config.ReadOnly,
		Color:  "green",
	}); err != nil {
		return config.Store{}, err
	}
	return fixed, fixed.SaveProfiles(profiles)
}

// fixture is a small database with enough in it to fill every screen.
func fixture() (string, error) {
	path := filepath.Join(os.TempDir(), fixtureName)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()

	_, err = db.Exec(`
		CREATE TABLE users (id integer primary key, email text not null, created_at text);
		CREATE TABLE orders (id integer primary key, user_id integer references users(id),
			total integer, currency text, status text, placed_at text, note text);
		CREATE INDEX orders_user_id ON orders(user_id);
		CREATE INDEX orders_status ON orders(status);
		INSERT INTO users (email, created_at) VALUES ('ada@example.com', '2026-01-04'),
			('grace@example.com', '2026-02-11'), ('alan@example.com', '2026-03-19');
		INSERT INTO orders (user_id, total, currency, status, placed_at, note)
			VALUES (1, 4200, 'PLN', 'paid', '2026-08-01', 'first'),
			       (2, 199, 'EUR', 'pending', '2026-08-02', 'second'),
			       (3, 15000, 'PLN', 'paid', '2026-08-03', 'third');`)
	return path, err
}

// catalogue prints every shape a bar can take, at the ratios and severities a
// dashboard actually shows, so a style can be chosen by looking at it in the
// font it will be drawn in rather than by reading its name.
func catalogue(plain bool) string {
	ratios := []float64{0, 0.05, 0.42, 0.61, 0.87, 1}
	severities := []ui.Severity{ui.SevOK, ui.SevWarn, ui.SevCritical, ui.SevInfo}
	var out strings.Builder
	for _, style := range ui.BarStyles {
		theme := ui.Default()
		theme.Bars(style.Name)
		out.WriteString("\n  " + theme.SectionHead.Render(strings.ToUpper(style.Name)) + "\n\n")
		for i, ratio := range ratios {
			severity := severities[i%len(severities)]
			row := fmt.Sprintf("  %5.0f%%  %s\n", ratio*100, theme.Gauge(ratio, severity))
			out.WriteString(row)
		}
	}
	rendered := ui.Default().Base.Render(out.String())
	if plain {
		return ansi.Strip(rendered) + "\n"
	}
	return rendered + "\n"
}
