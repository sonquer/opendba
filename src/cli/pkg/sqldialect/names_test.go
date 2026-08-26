package sqldialect

import (
	"sort"
	"testing"

	"github.com/antlr4-go/antlr/v4"
)

// TestEveryNamedRuleExists is what stops a refusal from being switched off by a
// typo: a rule name that no grammar has never matches, and the statement it was
// meant to refuse would be classified by something else.
func TestEveryNamedRuleExists(t *testing.T) {
	for _, dialect := range Default().dialects {
		grammar := dialect.(grammar)
		t.Run(grammar.name, func(t *testing.T) {
			known := ruleNames(grammar)
			var named []string
			named = append(named, grammar.statements...)
			for name := range grammar.rules {
				named = append(named, name)
			}
			for name := range grammar.refinements {
				named = append(named, name)
			}
			if grammar.analyzeRule != "" {
				named = append(named, grammar.analyzeRule)
			}
			sort.Strings(named)
			for _, name := range named {
				if !known[name] {
					t.Errorf("%s names a rule its grammar does not have: %q", grammar.name, name)
				}
			}
		})
	}
}

func ruleNames(g grammar) map[string]bool {
	_, names := g.parse(antlr.NewInputStream(""), &errorCollector{})
	known := map[string]bool{}
	for _, name := range names {
		known[name] = true
	}
	return known
}

// TestEveryPrefixRuleMatchesSomething is the other half: a prefix that matches
// no rule name is a classification that never happens.
func TestEveryPrefixRuleMatchesSomething(t *testing.T) {
	for _, dialect := range Default().dialects {
		grammar := dialect.(grammar)
		t.Run(grammar.name, func(t *testing.T) {
			known := ruleNames(grammar)
			for _, prefix := range grammar.prefixes {
				matched := false
				for name := range known {
					if _, ok := grammar.lookup(name); ok && len(name) > len(prefix.prefix) {
						matched = matched || name[:len(prefix.prefix)] == prefix.prefix
					}
				}
				if !matched {
					t.Errorf("%s has a prefix rule %q that matches no rule name", grammar.name, prefix.prefix)
				}
			}
		})
	}
}
