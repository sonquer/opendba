package grammar

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const DefaultVersion = "4.13.2"

func JarName(version string) string { return "antlr-" + version + "-complete.jar" }

func JarURL(version string) string {
	return "https://www.antlr.org/download/" + JarName(version)
}

func CacheDir(home string) string { return filepath.Join(home, ".cache", "tui4db-dev") }

type JarFetcher struct {
	Client *http.Client
	URL    string
}

func (f JarFetcher) Ensure(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download antlr: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download antlr: unexpected status %s", response.Status)
	}
	temp := path + ".part"
	file, err := os.Create(temp)
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(temp), err)
	}
	if _, err := io.Copy(file, response.Body); err != nil {
		_ = file.Close()
		_ = os.Remove(temp)
		return fmt.Errorf("write antlr jar: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("close antlr jar: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("install antlr jar: %w", err)
	}
	return nil
}
