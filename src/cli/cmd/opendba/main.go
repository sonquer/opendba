package main

import (
	"os"

	"github.com/sonquer/opendba/src/cli/internal/app"
)

var version = "dev"

func main() { os.Exit(app.Main(version, os.Args[1:])) }
