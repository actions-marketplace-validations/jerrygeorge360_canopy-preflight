package main

import (
	"os"

	"github.com/jerrygeorge360/canopy-preflight/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, version))
}
