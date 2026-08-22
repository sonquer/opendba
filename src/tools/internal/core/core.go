package core

import (
	"context"
	"fmt"
	"time"
)

type Status int

const (
	StatusPass Status = iota
	StatusFail
	StatusSkip
	StatusRunning
	StatusPending
)

func (s Status) String() string {
	switch s {
	case StatusPass:
		return "pass"
	case StatusFail:
		return "fail"
	case StatusSkip:
		return "skip"
	case StatusRunning:
		return "run"
	default:
		return "wait"
	}
}

func (s Status) Glyph() string {
	switch s {
	case StatusPass:
		return "✓"
	case StatusFail:
		return "✗"
	case StatusSkip:
		return "○"
	case StatusRunning:
		return "▸"
	default:
		return "·"
	}
}

func (s Status) OK() bool { return s != StatusFail }

type Row struct {
	Label  string
	Value  string
	Note   string
	Status Status
}

type Report struct {
	Check    string
	Status   Status
	Summary  string
	Rows     []Row
	Detail   []string
	Duration time.Duration
}

func (r Report) Title() string {
	return fmt.Sprintf("%s %s", r.Status.Glyph(), r.Check)
}

type Check interface {
	Name() string
	Describe() string
	Run(ctx context.Context) (Report, error)
}

type Suite []Check

func (s Suite) Names() []string {
	names := make([]string, 0, len(s))
	for _, c := range s {
		names = append(names, c.Name())
	}
	return names
}

func (s Suite) Find(name string) (Check, bool) {
	for _, c := range s {
		if c.Name() == name {
			return c, true
		}
	}
	return nil, false
}

func Aggregate(reports []Report) Status {
	worst := StatusPass
	for _, r := range reports {
		if r.Status == StatusFail {
			return StatusFail
		}
		if r.Status == StatusSkip && worst == StatusPass {
			worst = StatusSkip
		}
	}
	return worst
}
