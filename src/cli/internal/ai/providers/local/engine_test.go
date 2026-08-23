package local

import (
	"testing"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

func TestNormalise(t *testing.T) {
	cases := map[string]struct {
		given ai.EngineOptions
		want  ai.EngineOptions
	}{
		"everything at zero": {
			given: ai.EngineOptions{},
			want:  ai.EngineOptions{Context: DefaultContext, MaxTokens: DefaultMaxTokens, GPULayers: AllGPULayers},
		},
		"a context below the floor is replaced": {
			given: ai.EngineOptions{Context: 64},
			want:  ai.EngineOptions{Context: DefaultContext, MaxTokens: DefaultMaxTokens, GPULayers: AllGPULayers},
		},
		"what was chosen is kept": {
			given: ai.EngineOptions{Context: 32768, MaxTokens: 200, GPULayers: 12},
			want:  ai.EngineOptions{Context: 32768, MaxTokens: 200, GPULayers: 12},
		},
		"a zero temperature is left alone": {
			given: ai.EngineOptions{Context: 4096, MaxTokens: 10, GPULayers: 1, Temperature: 0, TopK: 0},
			want:  ai.EngineOptions{Context: 4096, MaxTokens: 10, GPULayers: 1},
		},
		"layers can be forced onto the processor": {
			given: ai.EngineOptions{Context: 4096, MaxTokens: 10, GPULayers: -1},
			want:  ai.EngineOptions{Context: 4096, MaxTokens: 10, GPULayers: -1},
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Normalise(test.given); got != test.want {
				t.Fatalf("Normalise() = %+v, want %+v", got, test.want)
			}
		})
	}
}
