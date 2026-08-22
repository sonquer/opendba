package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePolicy(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, FileName), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDefaultPolicy(t *testing.T) {
	policy := Default()
	if policy.Coverage.Total != DefaultTotal {
		t.Errorf("total = %v", policy.Coverage.Total)
	}
	if policy.ModuleThreshold("cli") != DefaultTotal {
		t.Error("modules must fall back to the total")
	}
	if policy.PackageThreshold("cli/internal/ui") != 0 {
		t.Error("per package gates are off by default")
	}
	if policy.Exempt("anything") {
		t.Error("nothing is exempt by default")
	}
}

func TestLoadWithoutAFile(t *testing.T) {
	policy, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if policy.Coverage.Total != DefaultTotal {
		t.Errorf("total = %v", policy.Coverage.Total)
	}
}

func TestLoadReadsThresholds(t *testing.T) {
	root := writePolicy(t, `
[coverage]
total = 90
packages = 80
exempt = ["cli/internal/parser/generated", "tools/cmd"]

[coverage.modules]
cli = 95

[package]
"cli/pkg/sqlguard" = 100
"cli/pkg" = 85
`)
	policy, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if policy.Coverage.Total != 90 {
		t.Errorf("total = %v", policy.Coverage.Total)
	}
	if policy.ModuleThreshold("cli") != 95 {
		t.Errorf("cli threshold = %v", policy.ModuleThreshold("cli"))
	}
	if policy.ModuleThreshold("tools") != 90 {
		t.Errorf("tools threshold = %v", policy.ModuleThreshold("tools"))
	}
	if got := policy.PackageThreshold("cli/pkg/sqlguard"); got != 100 {
		t.Errorf("longest prefix must win, got %v", got)
	}
	if got := policy.PackageThreshold("cli/pkg/secretref"); got != 85 {
		t.Errorf("prefix match = %v", got)
	}
	if got := policy.PackageThreshold("cli/internal/ui"); got != 80 {
		t.Errorf("unmatched packages use the default floor, got %v", got)
	}
	if !policy.Exempt("cli/internal/parser/generated/postgresql") {
		t.Error("exempt prefixes must cover their children")
	}
	if !policy.Exempt("tools/cmd") {
		t.Error("an exact exempt path must match")
	}
	if policy.Exempt("cli/internal/parser") {
		t.Error("a parent of an exempt path is not exempt")
	}
	if policy.Exempt("tools/cmdx") {
		t.Error("prefixes must match on a path boundary")
	}
}

func TestLoadRejectsBrokenFiles(t *testing.T) {
	cases := map[string]string{
		"invalid toml":     "[coverage",
		"total too high":   "[coverage]\ntotal = 120\n",
		"negative total":   "[coverage]\ntotal = -1\n",
		"bad module value": "[coverage.modules]\ncli = 101\n",
		"bad package":      "[package]\n\"cli/pkg\" = -5\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writePolicy(t, content))
			if err == nil {
				t.Fatal("want error")
			}
			if !strings.Contains(err.Error(), FileName) && !strings.Contains(err.Error(), "between 0 and 100") {
				t.Errorf("error should name the problem: %v", err)
			}
		})
	}
}

func TestLoadReportsUnreadableFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, FileName), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("want read error")
	}
}

func TestWithTotalOverridesEveryModule(t *testing.T) {
	policy := Policy{Coverage: Coverage{Total: 90, Modules: map[string]float64{"cli": 99}}}
	overridden := policy.WithTotal(50)
	if overridden.ModuleThreshold("cli") != 50 || overridden.Coverage.Total != 50 {
		t.Fatalf("override = %+v", overridden.Coverage)
	}
	if policy.ModuleThreshold("cli") != 99 {
		t.Error("the original policy must not change")
	}
}
