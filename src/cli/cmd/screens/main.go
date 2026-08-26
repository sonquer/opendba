// Command screens prints every shape a bar can take, so a style can be chosen
// by looking at it rather than by reading its name.
//
// Walking the interface itself is what `go run ./src/tools/cmd/dev e2e` does,
// through a real terminal and against the scenarios in tests/e2e.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/sonquer/opendba/src/cli/internal/ui"
)

func main() {
	plain := flag.Bool("plain", false, "strip the styling")
	flag.Parse()
	if _, err := fmt.Print(catalogue(*plain)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// catalogue prints every shape a bar can take, at the ratios and severities a
// dashboard actually shows, so a style can be chosen by looking at it in the
// font it will be drawn in rather than by reading its name.
func catalogue(plain bool) string {
	ratios := []float64{0, 0.05, 0.42, 0.61, 0.87, 1}
	severities := []ui.Severity{ui.SevOK, ui.SevWarn, ui.SevCritical, ui.SevInfo}
	var out strings.Builder
	for _, style := range ui.BarStyles {
		theme := ui.Default()
		theme.Bars(style.Name)
		out.WriteString("\n  " + theme.SectionHead.Render(strings.ToUpper(style.Name)) + "\n\n")
		for i, ratio := range ratios {
			severity := severities[i%len(severities)]
			row := fmt.Sprintf("  %5.0f%%  %s\n", ratio*100, theme.Gauge(ratio, severity))
			out.WriteString(row)
		}
	}
	rendered := ui.Default().Base.Render(out.String())
	if plain {
		return ansi.Strip(rendered) + "\n"
	}
	return rendered + "\n"
}
