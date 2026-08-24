package local

import (
	"strings"
	"testing"

	"github.com/sonquer/opendba/src/cli/internal/ai"
)

func TestGrammar(t *testing.T) {
	grammar, err := Grammar([]ai.Tool{{Name: "list_tables"}, {Name: "run_select"}})
	if err != nil {
		t.Fatalf("Grammar() error = %v", err)
	}
	for _, want := range []string{
		`root ::= "<tool_call>" ws call ws "</tool_call>"`,
		`name ::= "list_tables" | "run_select"`,
		`char ::= [^"\\] | "\\" escape`,
	} {
		if !strings.Contains(grammar, want) {
			t.Fatalf("grammar does not hold %q:\n%s", want, grammar)
		}
	}
}

func TestGrammarRefusesWhatWouldBreakIt(t *testing.T) {
	cases := map[string]struct {
		tools []ai.Tool
		want  string
	}{
		"no tools":       {tools: nil, want: "at least one tool"},
		"empty name":     {tools: []ai.Tool{{Name: ""}}, want: "needs a name"},
		"uppercase":      {tools: []ai.Tool{{Name: "runSelect"}}, want: "lowercase"},
		"a digit first":  {tools: []ai.Tool{{Name: "2fast"}}, want: "lowercase"},
		"punctuation":    {tools: []ai.Tool{{Name: "run-select"}}, want: "lowercase"},
		"a quote":        {tools: []ai.Tool{{Name: `a" | "b`}}, want: "lowercase"},
		"one bad of two": {tools: []ai.Tool{{Name: "ok"}, {Name: "NOT"}}, want: "lowercase"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Grammar(test.tools)
			if err == nil {
				t.Fatal("Grammar() must refuse this")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want it to mention %q", err, test.want)
			}
		})
	}
}

func TestGrammarAcceptsDigitsAndUnderscores(t *testing.T) {
	if _, err := Grammar([]ai.Tool{{Name: "explain_query_2"}}); err != nil {
		t.Fatalf("Grammar() error = %v", err)
	}
}

func TestTriggerPatterns(t *testing.T) {
	patterns := TriggerPatterns()
	if len(patterns) != 1 || !strings.Contains(patterns[0], CallOpen) {
		t.Fatalf("TriggerPatterns() = %v, want one pattern naming the opening tag", patterns)
	}
}
