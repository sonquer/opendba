package tuitest

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestASandboxKeepsTheRunAwayFromWhatIsBeingUsed(t *testing.T) {
	root := t.TempDir()
	box, err := NewSandbox(root)
	if err != nil {
		t.Fatalf("NewSandbox = %v", err)
	}
	for _, dir := range []string{box.config(), box.state(), box.data(), box.Databases()} {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
	}
	environment := strings.Join(box.Environment(), "\n")
	for _, want := range []string{
		"HOME=" + root,
		"XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
		"XDG_STATE_HOME=" + filepath.Join(root, "state"),
		"XDG_DATA_HOME=" + filepath.Join(root, "data"),
		"TERM=xterm-256color",
	} {
		if !strings.Contains(environment, want) {
			t.Errorf("the environment is missing %q", want)
		}
	}
}

func TestASandboxCannotBeLaidOutUnderAFile(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatalf("set up = %v", err)
	}
	if _, err := NewSandbox(filepath.Join(blocked, "under")); err == nil {
		t.Error("a sandbox was laid out under a file")
	}
}

func TestProfilesAreWrittenWhereOnlyTheirOwnerCanReadThem(t *testing.T) {
	box, err := NewSandbox(t.TempDir())
	if err != nil {
		t.Fatalf("NewSandbox = %v", err)
	}
	err = box.WriteProfiles([]Profile{
		{ID: "one", Name: "screens", Driver: "sqlite", File: "db/core.db", Mode: "readonly", Color: "green"},
		{ID: "two", Name: "remote", Driver: "postgres", Mode: "readonly", Color: "red"},
	})
	if err != nil {
		t.Fatalf("WriteProfiles = %v", err)
	}
	path := filepath.Join(box.config(), "profiles.toml")
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat = %v", err)
		}
		if info.Mode().Perm() != configMode {
			t.Errorf("profiles.toml is %v, and the program refuses anything looser", info.Mode().Perm())
		}
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read = %v", err)
	}
	body := string(written)
	for _, want := range []string{`[[connection]]`, `name = "screens"`, `file = "db/core.db"`, `name = "remote"`} {
		if !strings.Contains(body, want) {
			t.Errorf("profiles.toml is missing %q:\n%s", want, body)
		}
	}
	if strings.Count(body, "file = ") != 1 {
		t.Error("a profile that names no file was given one")
	}
}

func TestSettingsPinWhatWouldOtherwiseDrift(t *testing.T) {
	box, err := NewSandbox(t.TempDir())
	if err != nil {
		t.Fatalf("NewSandbox = %v", err)
	}
	if err := box.WriteSettings("dots"); err != nil {
		t.Fatalf("WriteSettings = %v", err)
	}
	written, err := os.ReadFile(filepath.Join(box.config(), "settings.toml"))
	if err != nil {
		t.Fatalf("read = %v", err)
	}
	body := string(written)
	for _, want := range []string{`bar = "dots"`, `mouse = "off"`, "[history]\nenabled = false"} {
		if !strings.Contains(body, want) {
			t.Errorf("settings.toml is missing %q:\n%s", want, body)
		}
	}
}

func TestWritingFailsWhenThereIsNowhereToWrite(t *testing.T) {
	box := Sandbox{Root: filepath.Join(t.TempDir(), "never made")}
	if err := box.WriteProfiles(nil); err == nil {
		t.Error("profiles were written into a sandbox that was never laid out")
	}
}

func TestCrashesAreWhatTheProgramLeftBehind(t *testing.T) {
	box, err := NewSandbox(t.TempDir())
	if err != nil {
		t.Fatalf("NewSandbox = %v", err)
	}
	found, err := box.Crashes()
	if err != nil || found != nil {
		t.Errorf("Crashes() = %v, %v", found, err)
	}
	report := filepath.Join(box.StateDir(), "crash-2026.log")
	if err := os.WriteFile(report, []byte("it fell over"), 0o600); err != nil {
		t.Fatalf("write = %v", err)
	}
	if err := os.WriteFile(filepath.Join(box.StateDir(), "engine.log"), nil, 0o600); err != nil {
		t.Fatalf("write = %v", err)
	}
	found, err = box.Crashes()
	if err != nil {
		t.Fatalf("Crashes = %v", err)
	}
	if len(found) != 1 || found[0] != report {
		t.Errorf("Crashes() = %v", found)
	}
}

func TestCrashesAreNoneWhenThereIsNoStateAtAll(t *testing.T) {
	box := Sandbox{Root: filepath.Join(t.TempDir(), "never made")}
	found, err := box.Crashes()
	if err != nil || found != nil {
		t.Errorf("Crashes() = %v, %v", found, err)
	}
}
