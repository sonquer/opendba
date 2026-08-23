package ai

import "testing"

func TestUsageVisibleOutput(t *testing.T) {
	cases := map[string]struct {
		usage Usage
		want  int
	}{
		"no reasoning":         {usage: Usage{Output: 120}, want: 120},
		"reasoning subtracted": {usage: Usage{Output: 120, Reasoning: 40}, want: 80},
		"never negative":       {usage: Usage{Output: 10, Reasoning: 40}, want: 0},
		"empty":                {usage: Usage{}, want: 0},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := test.usage.VisibleOutput(); got != test.want {
				t.Fatalf("VisibleOutput() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestUsageBalanced(t *testing.T) {
	cases := map[string]struct {
		usage Usage
		want  bool
	}{
		"parts add up": {
			usage: Usage{Input: 100, NonCachedInput: 60, CacheRead: 30, CacheWrite: 10},
			want:  true,
		},
		"parts do not add up": {
			usage: Usage{Input: 100, NonCachedInput: 60},
			want:  false,
		},
		"empty is balanced": {usage: Usage{}, want: true},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := test.usage.Balanced(); got != test.want {
				t.Fatalf("Balanced() = %v, want %v", got, test.want)
			}
		})
	}
}
