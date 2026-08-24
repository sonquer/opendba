package local

import (
	_ "embed"
	"fmt"
	"slices"

	"github.com/BurntSushi/toml"
)

//go:embed models.toml
var catalogue []byte

// Entry is one model the program knows how to fetch and how to run.
type Entry struct {
	ID          string  `toml:"id"`
	Title       string  `toml:"title"`
	Licence     string  `toml:"licence"`
	Repo        string  `toml:"repo"`
	File        string  `toml:"file"`
	Revision    string  `toml:"revision"`
	Bytes       int64   `toml:"bytes"`
	Context     int     `toml:"context"`
	Template    string  `toml:"template"`
	Tools       bool    `toml:"tools"`
	Temperature float64 `toml:"temperature"`
	TopP        float64 `toml:"top_p"`
	TopK        int     `toml:"top_k"`
	Note        string  `toml:"note"`

	// Layers, KVHeads and HeadDim are what the cache arithmetic needs.
	Layers  int `toml:"layers,omitempty"`
	KVHeads int `toml:"kv_heads,omitempty"`
	HeadDim int `toml:"head_dim,omitempty"`
}

// Catalogue is every model this program offers, in the order the file lists.
func Catalogue() ([]Entry, error) {
	var document struct {
		Models []Entry `toml:"model"`
	}
	if err := toml.Unmarshal(catalogue, &document); err != nil {
		return nil, fmt.Errorf("read the model catalogue: %w", err)
	}
	for _, entry := range document.Models {
		if err := entry.validate(); err != nil {
			return nil, err
		}
	}
	return document.Models, nil
}

// Offered returns the catalogue entry a name belongs to.
func Offered(id string) (Entry, error) {
	entries, err := Catalogue()
	if err != nil {
		return Entry{}, err
	}
	at := slices.IndexFunc(entries, func(entry Entry) bool { return entry.ID == id })
	if at < 0 {
		return Entry{}, fmt.Errorf("no model named %q is offered", id)
	}
	return entries[at], nil
}

func (e Entry) validate() error {
	switch {
	case e.ID == "":
		return fmt.Errorf("a catalogue entry has no name")
	case e.Repo == "" || e.File == "":
		return fmt.Errorf("the catalogue entry %q says nothing about where to fetch it", e.ID)
	case e.Revision == "":
		return fmt.Errorf("the catalogue entry %q is not pinned to a revision", e.ID)
	case e.Bytes <= 0:
		return fmt.Errorf("the catalogue entry %q has no measured size", e.ID)
	}
	return nil
}

// URL is where the file lives, pinned to the revision this program was built
// against rather than to whatever is newest.
func (e Entry) URL() string {
	return "https://huggingface.co/" + e.Repo + "/resolve/" + e.Revision + "/" + e.File
}
