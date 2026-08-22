package core

import (
	"context"
	"errors"
	"testing"
)

type stubCheck struct {
	name string
	rep  Report
	err  error
}

func (s stubCheck) Name() string     { return s.name }
func (s stubCheck) Describe() string { return "stub " + s.name }
func (s stubCheck) Run(context.Context) (Report, error) {
	return s.rep, s.err
}

func TestStatusStringAndGlyph(t *testing.T) {
	cases := []struct {
		status Status
		str    string
		glyph  string
	}{
		{StatusPass, "pass", "✓"},
		{StatusFail, "fail", "✗"},
		{StatusSkip, "skip", "○"},
		{StatusRunning, "run", "▸"},
		{StatusPending, "wait", "·"},
	}
	for _, c := range cases {
		if got := c.status.String(); got != c.str {
			t.Errorf("String() = %q, want %q", got, c.str)
		}
		if got := c.status.Glyph(); got != c.glyph {
			t.Errorf("Glyph() = %q, want %q", got, c.glyph)
		}
	}
}

func TestStatusOK(t *testing.T) {
	if !StatusPass.OK() || !StatusSkip.OK() {
		t.Error("pass and skip must be OK")
	}
	if StatusFail.OK() {
		t.Error("fail must not be OK")
	}
}

func TestReportTitle(t *testing.T) {
	r := Report{Check: "cover", Status: StatusFail}
	if got := r.Title(); got != "✗ cover" {
		t.Errorf("Title() = %q", got)
	}
}

func TestSuiteNamesAndFind(t *testing.T) {
	s := Suite{stubCheck{name: "a"}, stubCheck{name: "b"}}
	names := s.Names()
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("Names() = %v", names)
	}
	if c, ok := s.Find("b"); !ok || c.Name() != "b" {
		t.Fatalf("Find(b) = %v, %v", c, ok)
	}
	if _, ok := s.Find("missing"); ok {
		t.Fatal("Find(missing) must report false")
	}
}

func TestCheckContract(t *testing.T) {
	want := errors.New("boom")
	c := stubCheck{name: "x", rep: Report{Check: "x"}, err: want}
	rep, err := c.Run(context.Background())
	if !errors.Is(err, want) || rep.Check != "x" || c.Describe() != "stub x" {
		t.Fatalf("unexpected contract: %v %v", rep, err)
	}
}

func TestAggregate(t *testing.T) {
	cases := []struct {
		name    string
		reports []Report
		want    Status
	}{
		{"empty", nil, StatusPass},
		{"all pass", []Report{{Status: StatusPass}, {Status: StatusPass}}, StatusPass},
		{"one fail", []Report{{Status: StatusPass}, {Status: StatusFail}}, StatusFail},
		{"skip downgrades", []Report{{Status: StatusPass}, {Status: StatusSkip}}, StatusSkip},
		{"fail beats skip", []Report{{Status: StatusSkip}, {Status: StatusFail}}, StatusFail},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Aggregate(c.reports); got != c.want {
				t.Errorf("Aggregate() = %v, want %v", got, c.want)
			}
		})
	}
}
