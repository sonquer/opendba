package main

import (
	"os"

	"github.com/sonquer/tui4db/src/tools/internal/cli"
)

func main() { os.Exit(cli.Main("dev", os.Args[1:])) }
