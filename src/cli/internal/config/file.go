package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

func readSecureFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if err := verifyMode(path, info.Mode().Perm()); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	return data, nil
}

func writeSecureFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	temp := filepath.Join(dir, "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(temp), err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return nil
}

func decodeTOML(data []byte, target any) error {
	if err := toml.Unmarshal(data, target); err != nil {
		return fmt.Errorf("parse configuration: %w", err)
	}
	return nil
}

func encodeTOML(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := toml.NewEncoder(&buffer)
	encoder.Indent = "  "
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("encode configuration: %w", err)
	}
	return buffer.Bytes(), nil
}
