package local

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const manifestName = "manifest.json"

// Manifest is what was fetched, written beside it.
type Manifest struct {
	ID       string `json:"id"`
	Repo     string `json:"repo"`
	File     string `json:"file"`
	Revision string `json:"revision"`
	Bytes    int64  `json:"bytes"`
	SHA256   string `json:"sha256"`
}

// Store is the models on this machine. Each one lives in a directory of its
// own, named for the catalogue entry, holding the file and its manifest.
type Store struct{ dir string }

// NewStore reads and writes models under a directory.
func NewStore(dir string) *Store { return &Store{dir: dir} }

// Dir is where the models are kept.
func (s *Store) Dir() string { return s.dir }

// Path is where a model would live.
func (s *Store) Path(entry Entry) string {
	return filepath.Join(s.dir, entry.ID, entry.File)
}

// Find returns a model that has been downloaded, with the settings the
// catalogue says to run it with.
func (s *Store) Find(id string) (Installed, error) {
	manifest, err := s.manifest(id)
	if err != nil {
		return Installed{}, err
	}
	path := filepath.Join(s.dir, id, manifest.File)
	info, err := os.Stat(path)
	if err != nil {
		return Installed{}, fmt.Errorf("the model %q is recorded but its file is missing: %w", id, err)
	}
	if manifest.Bytes > 0 && info.Size() != manifest.Bytes {
		return Installed{}, fmt.Errorf("the file of %q is %d bytes and should be %d", id, info.Size(), manifest.Bytes)
	}
	return installed(id, path), nil
}

// Installed lists what has been downloaded, in name order.
func (s *Store) Installed() ([]Installed, error) {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.dir, err)
	}
	found := []Installed{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		model, err := s.Find(entry.Name())
		if err != nil {
			continue
		}
		found = append(found, model)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].ID < found[j].ID })
	return found, nil
}

// Has reports whether a model has been downloaded and is whole.
func (s *Store) Has(id string) bool {
	_, err := s.Find(id)
	return err == nil
}

// Remove deletes a model and everything kept with it.
func (s *Store) Remove(id string) error {
	if _, err := s.manifest(id); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(s.dir, id)); err != nil {
		return fmt.Errorf("remove %s: %w", id, err)
	}
	return nil
}

func (s *Store) manifest(id string) (Manifest, error) {
	if id == "" {
		return Manifest{}, fmt.Errorf("no model was named")
	}
	read, err := os.ReadFile(filepath.Join(s.dir, id, manifestName))
	if os.IsNotExist(err) {
		return Manifest{}, fmt.Errorf("the model %q has not been downloaded", id)
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("read the manifest of %q: %w", id, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(read, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("read the manifest of %q: %w", id, err)
	}
	if manifest.File == "" {
		return Manifest{}, fmt.Errorf("the manifest of %q does not say which file it describes", id)
	}
	return manifest, nil
}

// Write records a model that has been fetched.
func (s *Store) Write(manifest Manifest) error {
	if manifest.ID == "" || manifest.File == "" {
		return fmt.Errorf("a manifest needs a name and a file")
	}
	directory := filepath.Join(s.dir, manifest.ID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", directory, err)
	}
	written, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("write the manifest of %q: %w", manifest.ID, err)
	}
	path := filepath.Join(directory, manifestName)
	if err := os.WriteFile(path, written, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// installed puts the settings the catalogue holds together with the file on
// disk.
func installed(id, path string) Installed {
	model := Installed{
		ID:        id,
		Path:      path,
		Context:   DefaultContext,
		MaxTokens: DefaultMaxTokens,
	}
	entry, err := Offered(id)
	if err != nil {
		return model
	}
	model.Template = entry.Template
	model.Temperature = entry.Temperature
	model.TopP = entry.TopP
	model.TopK = entry.TopK
	if entry.Context > 0 && entry.Context < model.Context {
		model.Context = entry.Context
	}
	return model
}
