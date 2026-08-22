package grammar

import "strings"

const (
	lexerReceiver  = "l."
	parserReceiver = "p."
	javaReceiver   = "this."
)

func Transform(content string, lexer bool) string {
	lines := strings.SplitAfter(content, "\n")
	for i, line := range lines {
		if !strings.Contains(line, javaReceiver) {
			continue
		}
		receiver := parserReceiver
		if lexer && !strings.Contains(line, "}?") {
			receiver = lexerReceiver
		}
		lines[i] = strings.ReplaceAll(line, javaReceiver, receiver)
	}
	return strings.Join(lines, "")
}

func NeedsTransform(content string) bool { return strings.Contains(content, javaReceiver) }
