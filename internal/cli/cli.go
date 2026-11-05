// Package cli implements the berthkeep / bkeep command line.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mohamed-foly/berthkeep/internal/domain"
	"github.com/mohamed-foly/berthkeep/internal/paths"
	"github.com/mohamed-foly/berthkeep/internal/session"
)

// Version is injected by main.
var Version = "dev"

// Exit codes.
const (
	ExitOK       = 0
	ExitUsage    = 1
	ExitNotFound = 2
	ExitConflict = 3
	ExitStart    = 4
)

// Env carries what a command needs: resolved home and output streams.
type Env struct {
	Home   string
	Out    io.Writer
	Err    io.Writer
	Global *flag.FlagSet
}

type command struct {
	summary string
	run     func(e *Env, args []string) error
}

var commands = map[string]command{}

func register(name, summary string, run func(e *Env, args []string) error) {
	commands[name] = command{summary: summary, run: run}
}

func init() {
	register("version", "print the version", func(e *Env, _ []string) error {
		fmt.Fprintln(e.Out, "berthkeep", Version)
		return nil
	})
	register("home", "print the data directory", func(e *Env, _ []string) error {
		fmt.Fprintln(e.Out, e.Home)
		return nil
	})
}

// Run parses global flags, resolves the home and dispatches to a subcommand.
func Run(args []string, out, errOut io.Writer) int {
	global := flag.NewFlagSet("berthkeep", flag.ContinueOnError)
	global.SetOutput(errOut)
	homeFlag := global.String("home", "", "data directory (overrides "+paths.EnvHome+")")
	global.Usage = func() { usage(errOut) }
	if err := global.Parse(args); err != nil {
		return ExitUsage
	}
	rest := global.Args()
	if len(rest) == 0 {
		rest = []string{"ui"} // double-click or bare command opens the UI
	}
	name := rest[0]
	cmd, ok := commands[name]
	if !ok {
		fmt.Fprintf(errOut, "unknown command %q\n\n", name)
		usage(errOut)
		return ExitUsage
	}
	home, err := paths.Home(*homeFlag)
	if err != nil {
		fmt.Fprintln(errOut, "error:", err)
		return ExitUsage
	}
	env := &Env{Home: home, Out: out, Err: errOut, Global: global}
	if err := cmd.run(env, rest[1:]); err != nil {
		fmt.Fprintln(errOut, "error:", err)
		return exitCode(err)
	}
	return ExitOK
}

func exitCode(err error) int {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return ExitNotFound
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrBusy):
		return ExitConflict
	case errors.Is(err, ErrStart), errors.Is(err, session.ErrStartFailed):
		return ExitStart
	case errors.Is(err, flag.ErrHelp), errors.Is(err, domain.ErrInvalid), errors.Is(err, errUsage):
		return ExitUsage
	}
	return ExitUsage
}

// ErrStart marks a browser that failed to start (exit code 4).
var ErrStart = errors.New("browser failed to start")

var errUsage = errors.New("usage")

func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage: berthkeep [--home DIR] <command> [args]   (alias: bkeep)")
	fmt.Fprintln(w)
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(w, "  %-10s %s\n", n, commands[n].summary)
	}
	fmt.Fprintln(w, "\n"+strings.TrimSpace("Exit codes: 0 ok, 1 usage, 2 not found, 3 conflict, 4 start failed."))
}
