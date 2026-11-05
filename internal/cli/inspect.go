package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/mohamed-foly/berthkeep/internal/adapters/browsers"
	"github.com/mohamed-foly/berthkeep/internal/config"
	"github.com/mohamed-foly/berthkeep/internal/port"
	"github.com/mohamed-foly/berthkeep/internal/ports"
	"github.com/mohamed-foly/berthkeep/internal/session"
)

func init() {
	register("browsers", "list detected browsers", runBrowsers)
	register("detect", "list running browsers that expose a debugging port", runDetect)
}

func customBrowsers(c config.Config) []port.Browser {
	out := make([]port.Browser, 0, len(c.Browsers.Custom))
	for _, b := range c.Browsers.Custom {
		out = append(out, port.Browser{ID: b.ID, Name: b.Name, Path: b.Path})
	}
	return out
}

func runBrowsers(e *Env, args []string) error {
	fs := flag.NewFlagSet("browsers", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	cfg, err := config.Load(e.Home)
	if err != nil {
		return err
	}
	list := browsers.New(customBrowsers(cfg)).Browsers()
	if *asJSON {
		return json.NewEncoder(e.Out).Encode(list)
	}
	if len(list) == 0 {
		fmt.Fprintln(e.Out, "no Chromium-family browser found; add one under browsers.custom in config.json")
		return nil
	}
	w := tabwriter.NewWriter(e.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tPATH")
	for _, b := range list {
		fmt.Fprintf(w, "%s\t%s\t%s\n", b.ID, b.Name, b.Path)
	}
	return w.Flush()
}

func detect(ctx context.Context, e *Env) ([]session.Detected, error) {
	svc, err := service(e)
	if err != nil {
		return nil, err
	}
	return svc.Detect(ctx)
}

func runDetect(e *Env, args []string) error {
	fs := flag.NewFlagSet("detect", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	list, err := detect(ctx, e)
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(e.Out).Encode(list)
	}
	if len(list) == 0 {
		fmt.Fprintln(e.Out, "no browser with a debugging port is running")
		return nil
	}
	w := tabwriter.NewWriter(e.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PID\tPORT\tLOCK\tSESSION\tPROFILE")
	for _, d := range list {
		fmt.Fprintf(w, "%d\t%d\t%s\t%s\t%s\n", d.PID, d.Port, d.Lock, orDash(d.Session), d.UserDataDir)
	}
	return w.Flush()
}

func init() {
	register("doctor", "check the environment (home, config, browsers, ports)", runDoctor)
}

func runDoctor(e *Env, _ []string) error {
	ok := func(good bool) string {
		if good {
			return "ok  "
		}
		return "WARN"
	}
	fmt.Fprintf(e.Out, "%s home        %s\n", ok(true), e.Home)
	cfg, err := config.Load(e.Home)
	fmt.Fprintf(e.Out, "%s config      %v\n", ok(err == nil), errOr(err, "valid"))
	list := browsers.New(customBrowsers(cfg)).Browsers()
	names := make([]string, 0, len(list))
	for _, b := range list {
		names = append(names, b.ID)
	}
	fmt.Fprintf(e.Out, "%s browsers    %v\n", ok(len(list) > 0), names)
	free := 0
	for p := cfg.Ports.RangeStart; p <= cfg.Ports.RangeEnd; p++ {
		if ports.IsFree(p) {
			free++
		}
	}
	fmt.Fprintf(e.Out, "%s port range  %d-%d (%d free)\n", ok(free > 0), cfg.Ports.RangeStart, cfg.Ports.RangeEnd, free)
	fmt.Fprintf(e.Out, "%s ui port     %d %s\n", ok(ports.IsFree(cfg.Server.Port)), cfg.Server.Port, map[bool]string{true: "free", false: "in use (service running?)"}[ports.IsFree(cfg.Server.Port)])
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if d, err := detect(ctx, e); err == nil {
		fmt.Fprintf(e.Out, "%s detected    %d browser(s) with a debugging port\n", ok(true), len(d))
	}
	return nil
}

func errOr(err error, s string) string {
	if err != nil {
		return err.Error()
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
