package main

import (
	"os"

	"github.com/sonquer/tui4db/src/cli/internal/app"
)

var version = "dev"

func main() { os.Exit(app.Main(version, os.Args[1:])) }
