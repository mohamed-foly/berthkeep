// Command berthkeep manages Chromium CDP sessions.
package main

import (
	"os"

	"github.com/mohamed-foly/berthkeep/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

func init() { cli.Version = version }
