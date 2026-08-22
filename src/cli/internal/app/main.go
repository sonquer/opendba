package app

import (
	"context"
	"fmt"
	"os"

	"github.com/sonquer/tui4db/src/cli/internal/cli"
	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/pkg/sqldialect"
)

func Main(version string, args []string) int {
	store, err := config.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return cli.ExitFailure
	}
	application := cli.App{
		Store:    store,
		Registry: cli.Registry(),
		Secrets:  cli.Secrets(store.Paths, PassphrasePrompt),
		Dialects: sqldialect.Default(),
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Version:  version,
		Launch:   Launch,
		Wizard:   RunSetup,
		Prompt:   Prompt,
	}
	return application.Run(context.Background(), args)
}
