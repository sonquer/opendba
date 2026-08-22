package main

import (
	"os"

	"github.com/sonquer/tui4db/src/tools/internal/cli"
)

func main() { os.Exit(cli.Main("comments", append([]string{"comments"}, os.Args[1:]...))) }
