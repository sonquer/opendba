package app

import (
	"context"
	"fmt"
	"os"

	"github.com/sonquer/opendba/src/cli/internal/cli"
	"github.com/sonquer/opendba/src/cli/internal/config"
	"github.com/sonquer/opendba/src/cli/pkg/sqldialect"
)

func Main(version string, args []string) int {
	store, err := config.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return cli.ExitFailure
	}
	kept := cli.NewKeep()
	defer func() { _ = kept.Close() }()
	application := cli.App{
		Store:    store,
		Kept:     kept,
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
