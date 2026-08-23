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

type grammar struct {
	name          string
	statementRule string
	analyzeRule   string
	explainToken  string
	explainSafe   bool
	rules         map[string]semantics
	prefixes      []prefixRule
	parse         func(input antlr.CharStream, listener antlr.ErrorListener) (antlr.Tree, []string)
}

func (g grammar) lookup(name string) (semantics, bool) {
	if rule, ok := g.rules[name]; ok {
		return rule, true
	}
	for _, candidate := range g.prefixes {
		if strings.HasPrefix(name, candidate.prefix) && strings.HasSuffix(name, "stmt") {
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
	c.errors = append(c.errors, SyntaxError{Line: line, Column: column, Message: message})
}

type statementWalker struct {
	antlr.BaseParseTreeListener
	grammar    grammar
	ruleNames  []string
	statements []Statement
	open       bool
	analyzing  bool
	explained  bool
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
	if name == w.grammar.statementRule {
		w.begin(ctx)
		return
	}
	if !w.open {
		return
	}
	if name == w.grammar.analyzeRule && w.grammar.analyzeRule != "" {
		w.analyzing = true
		return
	}
	if rule, ok := w.grammar.lookup(name); ok {
		w.apply(rule)
	}
}

func (w *statementWalker) ExitEveryRule(ctx antlr.ParserRuleContext) {
	if w.ruleName(ctx) != w.grammar.statementRule || !w.open {
		return
	}
	w.close(ctx)
	w.finish()
	w.open = false
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

func (w *statementWalker) begin(ctx antlr.ParserRuleContext) {
	text := strings.TrimSpace(ctx.GetText())
	if text == "" {
		return
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
	w.open = true
	w.analyzing = false
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
