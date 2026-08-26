package main

import (
	"os"

	"github.com/sonquer/opendba/src/cli/internal/app"
)

var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() { os.Exit(app.Main(release(), os.Args[1:])) }

func release() string {
	if commit == "" && date == "" {
		return version
	}
	return version + " (" + commit + ", " + date + ")"
}
