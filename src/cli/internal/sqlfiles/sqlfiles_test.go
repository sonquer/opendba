package sqlfiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sonquer/opendba/src/cli/internal/config"
)

func TestTheRootIsTheSettingOrTheDataDirectory(t *testing.T) {
	data := config.Paths{Data: filepath.Join("/data", "opendba")}
	for _, want := range []struct {
		name       string
		paths      config.Paths
		setting    string
		connection config.Connection
		root       string
	}{
		{"the data directory", data, "", config.Connection{Name: "production-eu"},
			filepath.Join("/data", "opendba", "sql", "production-eu")},
		{"the setting", data, filepath.Join("/elsewhere"), config.Connection{Name: "staging"},
			filepath.Join("/elsewhere", "staging")},
		{"a name a directory cannot hold", data, "", config.Connection{Name: "eu/west"},
			filepath.Join("/data", "opendba", "sql", "eu-west")},
		{"a name that is nothing but punctuation", data, "", config.Connection{Name: "///", ID: "01J"},
			filepath.Join("/data", "opendba", "sql", "01J")},
		{"nowhere to keep them", config.Paths{}, "", config.Connection{Name: "x"}, ""},
		{"a setting that is not a full path", data, "sql", config.Connection{Name: "x"}, ""},
	} {
		t.Run(want.name, func(t *testing.T) {
			if got := Root(want.paths, want.setting, want.connection); got != want.root {
				t.Errorf("Root = %q, want %q", got, want.root)
			}
		})
	}
}

func TestANameDoesNotBecomeAPath(t *testing.T) {
	for _, want := range []struct {
		name   string
		typed  string
		file   string
		refuse string
	}{
		{"a bare name", "report", "report.sql", ""},
		{"a name that already says so", "report.sql", "report.sql", ""},
		{"a name in capitals", "Report.SQL", "Report.SQL", ""},
		{"space around it", "  report  ", "report.sql", ""},
		{"nothing at all", "   ", "", "needs a name"},
		{"a way out", "../escape", "", "is a path"},
		{"a directory", "reports/monthly", "", "is a path"},
		{"a windows directory", `reports\monthly`, "", "is a path"},
		{"a hidden file", ".secret", "", "starts with a dot"},
		{"a name with a space in it", "my report", "", "cannot be a file name"},
		{"a name with a wildcard", "report*", "", "cannot be a file name"},
	} {
		t.Run(want.name, func(t *testing.T) {
			got, err := Named(want.typed)
			if want.refuse != "" {
				if err == nil || !strings.Contains(err.Error(), want.refuse) {
					t.Fatalf("Named(%q) = %q, %v, want a refusal saying %q", want.typed, got, err, want.refuse)
				}
				return
			}
			if err != nil {
				t.Fatalf("Named(%q): %v", want.typed, err)
			}
			if got != want.file {
				t.Errorf("Named(%q) = %q, want %q", want.typed, got, want.file)
			}
		})
	}
}

func TestAWorkspaceListsOnlyItsStatements(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"b.sql", "a.sql", "notes.txt", "C.SQL"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("SELECT 1"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "nested.sql"), 0o700); err != nil {
		t.Fatal(err)
	}
	files, err := List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := make([]string, 0, len(files))
	for _, file := range files {
		got = append(got, file.Name)
	}
	if strings.Join(got, ",") != "C.SQL,a.sql,b.sql" {
		t.Errorf("List = %v, want the three statements in order", got)
	}
	if files[0].Path != filepath.Join(root, "C.SQL") {
		t.Errorf("path = %q", files[0].Path)
	}
}

func TestAWorkspaceThatIsNotThereIsEmpty(t *testing.T) {
	files, err := List(filepath.Join(t.TempDir(), "not-yet"))
	if err != nil || files != nil {
		t.Errorf("List = %v, %v, want nothing and no failure", files, err)
	}
	if files, err := List(""); err != nil || files != nil {
		t.Errorf("List(nowhere) = %v, %v", files, err)
	}
}

func TestAWorkspaceThatIsAFileIsReported(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.WriteFile(root, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := List(root); err == nil {
		t.Error("a workspace that is a file must be reported")
	}
}

func TestWritingMakesTheWorkspace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sql", "production")
	path, err := Write(root, "report.sql", "SELECT 1")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	held, err := Read(root, "report.sql")
	if err != nil || held != "SELECT 1" {
		t.Fatalf("Read = %q, %v", held, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
	}
	directory, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if directory.Mode().Perm() != 0o700 {
		t.Errorf("directory mode = %v, want 0700", directory.Mode().Perm())
	}
}

func TestWritingLeavesNoTemporaryFileBehind(t *testing.T) {
	root := t.TempDir()
	if _, err := Write(root, "report.sql", "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(root, "report.sql", "SELECT 2"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "report.sql" {
		t.Errorf("the workspace holds %d entries, want only the file", len(entries))
	}
	held, _ := Read(root, "report.sql")
	if held != "SELECT 2" {
		t.Errorf("Read = %q, want the second statement", held)
	}
}

func TestCreatingRefusesToReplace(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, "report.sql", "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(root, "report.sql", "SELECT 2"); err == nil {
		t.Fatal("creating over a file that is already there must be refused")
	}
	if held, _ := Read(root, "report.sql"); held != "SELECT 1" {
		t.Errorf("the file was changed anyway, holds %q", held)
	}
}

func TestRemovingAFileThatIsNotThere(t *testing.T) {
	root := t.TempDir()
	if _, err := Write(root, "report.sql", "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if err := Remove(root, "report.sql"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := Remove(root, "report.sql"); err == nil {
		t.Error("removing what is not there must be reported")
	}
}

func TestReadingOutsideTheWorkspaceIsRefused(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join("..", "escape.sql")
	if _, err := Read(root, outside); err == nil {
		t.Error("Read outside the workspace must be refused")
	}
	if _, err := Write(root, outside, "SELECT 1"); err == nil {
		t.Error("Write outside the workspace must be refused")
	}
	if _, err := Create(root, outside, "SELECT 1"); err == nil {
		t.Error("Create outside the workspace must be refused")
	}
	if err := Remove(root, outside); err == nil {
		t.Error("Remove outside the workspace must be refused")
	}
	if _, err := Inside(root, "."); err == nil {
		t.Error("the workspace itself is not a file in it")
	}
	if _, err := Inside("", "report.sql"); err == nil {
		t.Error("nowhere to keep files must be refused")
	}
}

func TestAFileThatCannotBeReadIsReported(t *testing.T) {
	root := t.TempDir()
	if _, err := Read(root, "missing.sql"); err == nil {
		t.Error("reading what is not there must be reported")
	}
	blocked := filepath.Join(root, "report.sql")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(blocked, "x.sql", "SELECT 1"); err == nil {
		t.Error("a workspace under a file must be reported")
	}
}
