package main

import (
	"os"

	"github.com/sonquer/tui4db/src/tools/internal/grammar"
)

func main() { os.Exit(grammar.Main(os.Args[1:])) }
