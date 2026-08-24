package local

import (
	"fmt"
	"strings"

	"github.com/sonquer/opendba/src/cli/internal/ai"
)

// CallOpen and CallClose bracket a tool call in a local model's output.
const (
	CallOpen  = "<tool_call>"
	CallClose = "</tool_call>"
)

const grammarBody = `
call ::= "{" ws "\"name\"" ws ":" ws name ws "," ws "\"arguments\"" ws ":" ws object ws "}"
object ::= "{" ws ( member ( ws "," ws member )* )? ws "}"
member ::= string ws ":" ws value
array ::= "[" ws ( value ( ws "," ws value )* )? ws "]"
value ::= object | array | string | number | "true" | "false" | "null"
string ::= "\"" char* "\""
char ::= [^"\\] | "\\" escape
escape ::= ["\\/bfnrt] | "u" hex hex hex hex
hex ::= [0-9a-fA-F]
number ::= "-"? int frac? exp?
int ::= "0" | [1-9] [0-9]*
frac ::= "." [0-9]+
exp ::= [eE] [-+]? [0-9]+
ws ::= [ \t\n]*
`

// Grammar returns a GBNF grammar that a tool call has to match.
func Grammar(tools []ai.Tool) (string, error) {
	if len(tools) == 0 {
		return "", fmt.Errorf("a grammar needs at least one tool")
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if err := validName(tool.Name); err != nil {
			return "", err
		}
		names = append(names, tool.Name)
	}
	root := fmt.Sprintf("root ::= %q ws call ws %q", CallOpen, CallClose)
	name := "name ::= " + strings.Join(quoted(names), " | ")
	return root + "\n" + name + grammarBody, nil
}

// TriggerPatterns is what turns the grammar on.
func TriggerPatterns() []string { return []string{"^.*?" + CallOpen} }

func quoted(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, fmt.Sprintf("%q", name))
	}
	return out
}

func validName(name string) error {
	if name == "" {
		return fmt.Errorf("a tool needs a name")
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return fmt.Errorf("tool name %q may only hold lowercase letters, digits and underscores", name)
		}
	}
	return nil
}
