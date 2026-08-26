package local

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogue(t *testing.T) {
	entries, err := Catalogue()
	if err != nil {
		t.Fatalf("Catalogue() error = %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("the catalogue is empty")
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if seen[entry.ID] {
			t.Fatalf("two entries are both named %q", entry.ID)
		}
		seen[entry.ID] = true
		if entry.Licence != "Apache-2.0" && entry.Licence != "MIT" {
			t.Fatalf("%s is under %q, and the catalogue only offers what needs no account and no lawyer", entry.ID, entry.Licence)
		}
		if len(entry.Revision) != 40 {
			t.Fatalf("%s is pinned to %q, which is not a commit", entry.ID, entry.Revision)
		}
		if entry.Bytes <= 0 || entry.Context <= 0 {
			t.Fatalf("%s = %+v, want a measured size and a context", entry.ID, entry)
		}
	}
}

func TestCatalogueHasTheDefault(t *testing.T) {
	entry, err := Offered("gemma-4-e4b-qat")
	if err != nil {
		t.Fatalf("Offered() error = %v", err)
	}
	if entry.Repo != "unsloth/gemma-4-E4B-it-qat-GGUF" {
		t.Fatalf("repo = %q", entry.Repo)
	}
	want := "https://huggingface.co/unsloth/gemma-4-E4B-it-qat-GGUF/resolve/" +
		entry.Revision + "/gemma-4-E4B-it-qat-UD-Q4_K_XL.gguf"
	if entry.URL() != want {
		t.Fatalf("URL() = %q, want %q", entry.URL(), want)
	}
	if entry.Template != "gemma" || !entry.Tools {
		t.Fatalf("entry = %+v, want the template it was trained with and tools", entry)
	}
}

func TestOfferedRefusesWhatIsNotThere(t *testing.T) {
	if _, err := Offered("llama-3-405b"); err == nil {
		t.Fatal("Offered() found a model that is not in the catalogue")
	}
}

func TestEntryValidation(t *testing.T) {
	full := Entry{ID: "x", Repo: "r", File: "f", Revision: "v", Bytes: 1}
	cases := map[string]struct {
		change func(Entry) Entry
		want   string
	}{
		"no name":     {change: func(e Entry) Entry { e.ID = ""; return e }, want: "no name"},
		"no repo":     {change: func(e Entry) Entry { e.Repo = ""; return e }, want: "where to fetch"},
		"no file":     {change: func(e Entry) Entry { e.File = ""; return e }, want: "where to fetch"},
		"no revision": {change: func(e Entry) Entry { e.Revision = ""; return e }, want: "pinned"},
		"no size":     {change: func(e Entry) Entry { e.Bytes = 0; return e }, want: "measured size"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			err := test.change(full).validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validate() error = %v, want it to mention %q", err, test.want)
			}
		})
	}
	if err := full.validate(); err != nil {
		t.Fatalf("validate() error = %v, want a complete entry accepted", err)
	}
}

func store(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir())
}

func install(t *testing.T, s *Store, id, file string, bytes int) {
	t.Helper()
	if err := s.Write(Manifest{ID: id, File: file, Bytes: int64(bytes), Repo: "r", Revision: "v"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	path := filepath.Join(s.Dir(), id, file)
	if err := os.WriteFile(path, make([]byte, bytes), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStoreFind(t *testing.T) {
	s := store(t)
	install(t, s, "gemma-4-e4b-qat", "gemma.gguf", 32)

	found, err := s.Find("gemma-4-e4b-qat")
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if found.Path != filepath.Join(s.Dir(), "gemma-4-e4b-qat", "gemma.gguf") {
		t.Fatalf("path = %q", found.Path)
	}
	if found.Temperature != 1.0 || found.TopK != 64 {
		t.Fatalf("found = %+v, want the settings the catalogue holds", found)
	}
	if !s.Has("gemma-4-e4b-qat") {
		t.Fatal("Has() says a model that is there is not")
	}
}

func TestStoreFindAModelNobodyOffers(t *testing.T) {
	s := store(t)
	install(t, s, "something-of-my-own", "mine.gguf", 8)

	found, err := s.Find("something-of-my-own")
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if found.Context != DefaultContext || found.MaxTokens != DefaultMaxTokens {
		t.Fatalf("found = %+v, want sensible defaults rather than a refusal", found)
	}
}

func TestStoreFindFailures(t *testing.T) {
	s := store(t)

	if _, err := s.Find(""); err == nil {
		t.Fatal("Find() must refuse an empty name")
	}
	if _, err := s.Find("never-downloaded"); err == nil {
		t.Fatal("Find() found a model that was never downloaded")
	}
	if s.Has("never-downloaded") {
		t.Fatal("Has() says a model that is not there is")
	}

	install(t, s, "half-there", "half.gguf", 32)
	if err := os.Remove(filepath.Join(s.Dir(), "half-there", "half.gguf")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Find("half-there"); err == nil || !strings.Contains(err.Error(), "file is missing") {
		t.Fatalf("Find() error = %v, want it to say the file is gone", err)
	}

	install(t, s, "wrong-size", "wrong.gguf", 32)
	if err := os.WriteFile(filepath.Join(s.Dir(), "wrong-size", "wrong.gguf"), make([]byte, 8), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Find("wrong-size"); err == nil || !strings.Contains(err.Error(), "should be") {
		t.Fatalf("Find() error = %v, want it to say the size is wrong", err)
	}
}

func TestStoreRefusesABrokenManifest(t *testing.T) {
	s := store(t)
	directory := filepath.Join(s.Dir(), "broken")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, manifestName), []byte("{not json}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Find("broken"); err == nil {
		t.Fatal("Find() read a manifest that is not json")
	}

	empty, err := json.Marshal(Manifest{ID: "nameless"})
	if err != nil {
		t.Fatal(err)
	}
	nameless := filepath.Join(s.Dir(), "nameless")
	if err := os.MkdirAll(nameless, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nameless, manifestName), empty, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Find("nameless"); err == nil || !strings.Contains(err.Error(), "which file") {
		t.Fatalf("Find() error = %v, want it to say the manifest names no file", err)
	}
}

func TestStoreInstalled(t *testing.T) {
	s := store(t)
	if models, err := s.Installed(); err != nil || len(models) != 0 {
		t.Fatalf("Installed() = %v, %v, want nothing before anything is downloaded", models, err)
	}
	install(t, s, "gemma-4-e4b-qat", "b.gguf", 8)
	install(t, s, "gemma-4-e2b-qat", "a.gguf", 8)
	if err := os.WriteFile(filepath.Join(s.Dir(), "loose-file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.Dir(), "empty"), 0o700); err != nil {
		t.Fatal(err)
	}

	models, err := s.Installed()
	if err != nil {
		t.Fatalf("Installed() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("Installed() = %+v, want the two that are whole", models)
	}
	if models[0].ID != "gemma-4-e2b-qat" {
		t.Fatalf("Installed() = %+v, want them in name order", models)
	}
}

func TestStoreRemove(t *testing.T) {
	s := store(t)
	install(t, s, "gemma-4-e4b-qat", "gemma.gguf", 8)

	if err := s.Remove("gemma-4-e4b-qat"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if s.Has("gemma-4-e4b-qat") {
		t.Fatal("the model is still there")
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "gemma-4-e4b-qat")); !os.IsNotExist(err) {
		t.Fatal("the directory was left behind")
	}
	if err := s.Remove("gemma-4-e4b-qat"); err == nil {
		t.Fatal("Remove() must refuse a model that is not there")
	}
}

func TestStoreWriteRefusesAnEmptyManifest(t *testing.T) {
	s := store(t)
	if err := s.Write(Manifest{}); err == nil {
		t.Fatal("Write() must refuse a manifest with nothing in it")
	}
	if err := s.Write(Manifest{ID: "x"}); err == nil {
		t.Fatal("Write() must refuse a manifest that names no file")
	}
}

func TestStorePath(t *testing.T) {
	s := store(t)
	entry := Entry{ID: "gemma-4-e4b-qat", File: "gemma.gguf"}
	if got := s.Path(entry); got != filepath.Join(s.Dir(), "gemma-4-e4b-qat", "gemma.gguf") {
		t.Fatalf("Path() = %q", got)
	}
}
