package semver

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		fail bool
	}{
		{name: "release", in: "0.1.0", want: "0.1.0"},
		{name: "surrounding space", in: "  1.2.3\n", want: "1.2.3"},
		{name: "multiple digits", in: "10.20.30", want: "10.20.30"},
		{name: "prerelease", in: "1.2.3-rc.1", want: "1.2.3-rc.1"},
		{name: "nightly prerelease", in: "0.1.1-nightly.20260824.aea1fe3", want: "0.1.1-nightly.20260824.aea1fe3"},
		{name: "empty", in: "   ", fail: true},
		{name: "too few components", in: "1.2", fail: true},
		{name: "too many components", in: "1.2.3.4", fail: true},
		{name: "not numbers", in: "a.b.c", fail: true},
		{name: "empty component", in: "1..3", fail: true},
		{name: "leading zero", in: "01.2.3", fail: true},
		{name: "empty prerelease", in: "1.2.3-", fail: true},
		{name: "prerelease with underscore", in: "1.2.3-rc_1", fail: true},
		{name: "leading dash", in: "-1.2.3", fail: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse(c.in)
			if c.fail {
				if err == nil {
					t.Fatalf("Parse(%q) = %v, want an error", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q): %v", c.in, err)
			}
			if got.String() != c.want {
				t.Errorf("Parse(%q) = %q, want %q", c.in, got.String(), c.want)
			}
		})
	}
}

func TestBump(t *testing.T) {
	cases := []struct {
		name string
		from string
		kind Kind
		want string
	}{
		{name: "patch", from: "0.1.0", kind: Patch, want: "0.1.1"},
		{name: "minor resets patch", from: "0.1.4", kind: Minor, want: "0.2.0"},
		{name: "major resets the rest", from: "1.4.9", kind: Major, want: "2.0.0"},
		{name: "patch releases a prerelease", from: "0.2.0-rc.1", kind: Patch, want: "0.2.0"},
		{name: "minor drops a prerelease", from: "0.2.0-rc.1", kind: Minor, want: "0.3.0"},
		{name: "major drops a prerelease", from: "0.2.0-rc.1", kind: Major, want: "1.0.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			from, err := Parse(c.from)
			if err != nil {
				t.Fatal(err)
			}
			if got := from.Bump(c.kind).String(); got != c.want {
				t.Errorf("%s bumped %v = %q, want %q", c.from, c.kind, got, c.want)
			}
		})
	}
}

func TestParseKind(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		kind  Kind
		exact string
		fail  bool
	}{
		{name: "patch", in: "patch", kind: Patch},
		{name: "minor", in: "minor", kind: Minor},
		{name: "major", in: " major ", kind: Major},
		{name: "exact", in: "1.2.3", kind: Exact, exact: "1.2.3"},
		{name: "exact with a v", in: "v1.2.3", kind: Exact, exact: "1.2.3"},
		{name: "exact prerelease", in: "0.0.1-rc.1", kind: Exact, exact: "0.0.1-rc.1"},
		{name: "nonsense", in: "bigger", fail: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kind, exact, err := ParseKind(c.in)
			if c.fail {
				if err == nil {
					t.Fatalf("ParseKind(%q) = %v, want an error", c.in, kind)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseKind(%q): %v", c.in, err)
			}
			if kind != c.kind {
				t.Errorf("ParseKind(%q) kind = %v, want %v", c.in, kind, c.kind)
			}
			if c.exact != "" && exact.String() != c.exact {
				t.Errorf("ParseKind(%q) version = %q, want %q", c.in, exact.String(), c.exact)
			}
		})
	}
}

func TestNames(t *testing.T) {
	v, err := Parse("0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.Tag(), "v0.1.0"; got != want {
		t.Errorf("Tag() = %q, want %q", got, want)
	}
	if got, want := v.ModuleTag(), "src/cli/v0.1.0"; got != want {
		t.Errorf("ModuleTag() = %q, want %q", got, want)
	}
	if got, want := v.Branch(), "release/v0.1.0"; got != want {
		t.Errorf("Branch() = %q, want %q", got, want)
	}
}

func TestSnapshotSortsAfterTheReleaseItWasCutFrom(t *testing.T) {
	v, err := Parse("0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := v.Snapshot("20260824", "aea1fe3")
	if got, want := snapshot.String(), "0.1.1-nightly.20260824.aea1fe3"; got != want {
		t.Fatalf("Snapshot() = %q, want %q", got, want)
	}
	if snapshot.Patch <= v.Patch {
		t.Errorf("snapshot patch %d does not follow %d", snapshot.Patch, v.Patch)
	}
	if _, err := Parse(snapshot.String()); err != nil {
		t.Errorf("the snapshot version does not parse back: %v", err)
	}
}
