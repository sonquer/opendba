package main

import (
	"os"

	"github.com/sonquer/opendba/src/tools/internal/cli"
)

func main() { os.Exit(cli.Main("comments", append([]string{"comments"}, os.Args[1:]...))) }
