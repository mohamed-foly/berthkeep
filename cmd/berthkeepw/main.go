// Command berthkeepw is the windowless launcher: double-click opens the UI.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/berthkeep/berthkeep/internal/cli"
	"github.com/berthkeep/berthkeep/internal/paths"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	cli.Version = version
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"ui"}
	}
	var out, errOut bytes.Buffer
	code := cli.Run(args, &out, &errOut)
	if errOut.Len() > 0 {
		logError(errOut.String())
	}
	os.Exit(code)
}

// logError appends to <home>/logs/berthkeep.log; there is no console.
func logError(msg string) {
	home, err := paths.Home("")
	if err != nil {
		return
	}
	dir := filepath.Join(home, "logs")
	_ = os.MkdirAll(dir, 0o700)
	f, err := os.OpenFile(filepath.Join(dir, "berthkeep.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), msg)
}
