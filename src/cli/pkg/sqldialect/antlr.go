package sqldialect

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
)

type semantics struct {
	kind         Kind
	mutating     bool
	locking      bool
	materializes bool
	refusal      string
}

type prefixRule struct {
	prefix string
	rule   semantics
}

// refinement is the semantics a rule only carries for some of the statements it
// matches, read off the parse tree when the rule name alone cannot tell them
// apart.
type refinement func(ctx antlr.ParserRuleContext) semantics

type grammar struct {
	name string

	// statements are the rules that begin a statement. A grammar that spells one
	// statement in more than one rule, as T-SQL does, names all of them.
	statements []string

	// suffix is what a rule name must end in for a prefix rule to match it, which
	// is how a prefix stops matching the options and clauses inside the statement
	// it names. An empty suffix is a grammar whose rule names carry no such
	// convention.
	suffix      string
	analyzeRule string

	explainToken string
	explainSafe  bool
	rules        map[string]semantics
	prefixes     []prefixRule
	refinements  map[string]refinement
	parse        func(input antlr.CharStream, listener antlr.ErrorListener) (antlr.Tree, []string)
}

// opens reports whether a rule is one of the rules that begin a statement.
func (g grammar) opens(name string) bool {
	for _, candidate := range g.statements {
		if candidate == name {
			return true
		}
	}
	return false
}

func (g grammar) lookup(name string) (semantics, bool) {
	if rule, ok := g.rules[name]; ok {
		return rule, true
	}
	for _, candidate := range g.prefixes {
		if strings.HasPrefix(name, candidate.prefix) && strings.HasSuffix(name, g.suffix) {
			return candidate.rule, true
		}
	}
	return semantics{}, false
}

func (g grammar) Name() string { return g.name }

func (g grammar) Analyze(sql string) Analysis {
	collector := &errorCollector{}
	tree, ruleNames := g.parse(antlr.NewInputStream(sql), collector)
	walker := &statementWalker{grammar: g, ruleNames: ruleNames}
	antlr.NewParseTreeWalker().Walk(walker, tree)
	return Analysis{Statements: walker.statements, Errors: collector.errors}
}

type errorCollector struct {
	*antlr.DefaultErrorListener
	errors []SyntaxError
}

func (c *errorCollector) SyntaxError(_ antlr.Recognizer, _ any, line, column int, message string, _ antlr.RecognitionException) {
	c.errors = append(c.errors, SyntaxError{Line: line, Column: column, Message: readable(message)})
}

// longestMessage is how much of a parser's complaint a person can use. A parser
// that lists every token it would have accepted lists more than a thousand of
// them, and the part worth reading is at the front.
const longestMessage = 90

// readable shortens a parser's complaint to the part that says what is wrong,
// dropping the list of everything that would have been right.
func readable(message string) string {
	if cut := strings.Index(message, " expecting {"); cut > 0 {
		message = message[:cut]
	}
	runes := []rune(message)
	if len(runes) <= longestMessage {
		return message
	}
	return string(runes[:longestMessage]) + "..."
}

type statementWalker struct {
	antlr.BaseParseTreeListener
	grammar    grammar
	ruleNames  []string
	statements []Statement

	// depth is how many statement rules are open at once. A grammar that nests a
	// statement inside another, as T-SQL nests one inside IF and BEGIN, is one
	// statement to the caller, and only the outermost is recorded.
	depth     int
	recording bool
	analyzing bool
	explained bool
}

func (w *statementWalker) startsWithExplain(ctx antlr.ParserRuleContext) bool {
	if w.grammar.explainToken == "" {
		return false
	}
	start := ctx.GetStart()
	if start == nil {
		return false
	}
	return strings.EqualFold(start.GetText(), w.grammar.explainToken)
}

func (w *statementWalker) EnterEveryRule(ctx antlr.ParserRuleContext) {
	name := w.ruleName(ctx)
	if w.grammar.opens(name) {
		w.depth++
		if w.depth > 1 {
			return
		}
		w.recording = w.begin(ctx)
	}
	if !w.recording {
		return
	}
	if name == w.grammar.analyzeRule && w.grammar.analyzeRule != "" {
		w.analyzing = true
		return
	}
	if rule, ok := w.grammar.lookup(name); ok {
		w.apply(rule)
	}
	if refine, ok := w.grammar.refinements[name]; ok {
		w.apply(refine(ctx))
	}
}

func (w *statementWalker) ExitEveryRule(ctx antlr.ParserRuleContext) {
	if !w.grammar.opens(w.ruleName(ctx)) || w.depth == 0 {
		return
	}
	w.depth--
	if w.depth > 0 || !w.recording {
		return
	}
	w.close(ctx)
	w.finish()
	w.recording = false
}

// close records where the statement ended, which is where the last token it
// holds ends.
func (w *statementWalker) close(ctx antlr.ParserRuleContext) {
	current := &w.statements[len(w.statements)-1]
	stop := ctx.GetStop()
	if stop == nil {
		return
	}
	if end := stop.GetStop(); end >= current.Start {
		current.Stop = end
	}
}

// at is where a token begins, or nought when the parser has no token to point
// at.
func at(token antlr.Token) int {
	if token == nil {
		return 0
	}
	return token.GetStart()
}

func (w *statementWalker) ruleName(ctx antlr.ParserRuleContext) string {
	index := ctx.GetRuleIndex()
	if index < 0 || index >= len(w.ruleNames) {
		return ""
	}
	return w.ruleNames[index]
}

// begin starts a statement, and reports whether there was one to start. A rule
// holding nothing but separators is a statement the grammar allows and a person
// did not write.
func (w *statementWalker) begin(ctx antlr.ParserRuleContext) bool {
	text := strings.TrimSpace(ctx.GetText())
	if strings.Trim(text, ";") == "" {
		return false
	}
	statement := Statement{
		Kind:    KindUnknown,
		Refusal: "statement was not recognised",
		Text:    text,
		Start:   at(ctx.GetStart()),
		Stop:    at(ctx.GetStart()),
	}
	w.explained = w.startsWithExplain(ctx)
	if w.explained {
		statement.Kind = KindExplain
		statement.Refusal = ""
	}
	w.statements = append(w.statements, statement)
	w.analyzing = false
	return true
}

func (w *statementWalker) finish() {
	current := &w.statements[len(w.statements)-1]
	if !w.explained && current.Kind != KindExplain {
		return
	}
	if w.analyzing && !w.grammar.explainSafe {
		return
	}
	current.Kind = KindExplain
	current.Mutating = false
	current.MutatedBy = ""
	current.Locking = false
	current.Materializes = false
	current.Refusal = ""
}

func (w *statementWalker) apply(rule semantics) {
	current := &w.statements[len(w.statements)-1]
	if w.explained {
		if rule.mutating && !current.Mutating {
			current.MutatedBy = rule.kind
		}
		current.Mutating = current.Mutating || rule.mutating
		current.Locking = current.Locking || rule.locking
		current.Materializes = current.Materializes || rule.materializes
		if rule.refusal != "" && current.Refusal == "" {
			current.Refusal = rule.refusal
		}
		return
	}
	if current.Kind == KindUnknown && rule.kind != "" {
		current.Kind = rule.kind
		current.Refusal = rule.refusal
	} else if rule.refusal != "" && current.Refusal == "" {
		current.Refusal = rule.refusal
	}
	if rule.mutating && !current.Mutating && rule.kind != current.Kind {
		current.MutatedBy = rule.kind
	}
	current.Mutating = current.Mutating || rule.mutating
	current.Locking = current.Locking || rule.locking
	current.Materializes = current.Materializes || rule.materializes
}
