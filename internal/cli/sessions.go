package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/berthkeep/berthkeep/internal/app"
	"github.com/berthkeep/berthkeep/internal/cdp"
	"github.com/berthkeep/berthkeep/internal/domain"
	"github.com/berthkeep/berthkeep/internal/session"
)

func init() {
	register("new", "create a session: new <title> [--temp|--ephemeral] [--port n] [--clone s] [--adopt dir] [--browser id] [--headless] [--label a,b] [--url u] [--start]", runNew)
	register("list", "list sessions: list [--json] [--label x]", runList)
	register("start", "start a session: start <session> [--headless|--headed]", runStart)
	register("stop", "stop a session: stop <session>|--all", runStop)
	register("restart", "restart a session: restart <session>", runRestart)
	register("mode", "switch mode: mode <session> headless|headed", runMode)
	register("persist", "keep an ephemeral session's profile: persist <session> [--now]", runPersist)
	register("ephemeral", "make a managed session ephemeral (wiped on stop): ephemeral <session>", runEphemeral)
	register("endpoint", "print the CDP endpoint: endpoint <session> [--ensure] [--ws] [--json]", runEndpoint)
	register("edit", "edit metadata: edit <session> [--title t] [--slug s] [--port n|auto] [--label-add a] [--label-remove b] [--notes …] [--url u]", runEdit)
	register("rm", "delete a session: rm <session> [--keep-profile]", runRm)
	register("tabs", "list open tabs: tabs <session>", runTabs)
	register("open", "open a URL in a session: open <session> <url>", runOpen)
	register("gc", "run pending post-stop clean-up", runGC)
}

// parseInterleaved parses flags that may appear before or after positional arguments.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

func newFlags(name string, e *Env) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.Err)
	return fs
}

type multi []string

func (m *multi) String() string { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error {
	for _, x := range strings.Split(v, ",") {
		if x = strings.TrimSpace(x); x != "" {
			*m = append(*m, x)
		}
	}
	return nil
}

func service(e *Env) (*session.Service, error) { return app.NewSessionService(e.Home) }

func ctxTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 90*time.Second)
}

func needOne(pos []string, what string) (string, error) {
	if len(pos) != 1 {
		return "", fmt.Errorf("expected %s: %w", what, errUsage)
	}
	return pos[0], nil
}

func runNew(e *Env, args []string) error {
	fs := newFlags("new", e)
	temp := fs.Bool("temp", false, "quick temp: ephemeral and removed when stopped")
	eph := fs.Bool("ephemeral", false, "wipe the profile on every stop")
	portS := fs.String("port", "auto", "fixed port or auto")
	clone := fs.String("clone", "", "clone the profile of this session")
	cloneLive := fs.Bool("live", false, "with --clone: copy while the source runs instead of stop→copy→restart")
	adopt := fs.String("adopt", "", "register an existing user-data-dir in place")
	browser := fs.String("browser", "", "browser id (see `browsers`)")
	browserPath := fs.String("browser-path", "", "custom browser executable")
	headless := fs.Bool("headless", false, "headless mode")
	notes := fs.String("notes", "", "notes")
	var labels, urls, extra multi
	fs.Var(&labels, "label", "labels (comma-separated, repeatable)")
	fs.Var(&urls, "url", "start URL (repeatable)")
	fs.Var(&extra, "arg", "extra browser flag (repeatable)")
	start := fs.Bool("start", false, "start it now")
	asJSON := fs.Bool("json", false, "JSON output")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return errUsage
	}
	title := strings.Join(pos, " ")
	in := session.CreateInput{Title: title, Labels: labels, Notes: *notes, Browser: *browser, BrowserPath: *browserPath,
		Headless: *headless, URLs: urls, ExtraArgs: extra, CloneFrom: *clone, CloneLive: *cloneLive, AdoptDir: *adopt, Start: *start}
	switch {
	case *temp:
		in.Persistence = "temp"
	case *eph:
		in.Persistence = "ephemeral"
	}
	if *portS != "auto" && *portS != "" {
		n, err := strconv.Atoi(*portS)
		if err != nil {
			return fmt.Errorf("--port must be a number or auto: %w", errUsage)
		}
		in.Port = n
	}
	svc, err := service(e)
	if err != nil {
		return err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	sess, err := svc.Create(ctx, in)
	if err != nil {
		return err
	}
	if *asJSON {
		v, _ := svc.Get(ctx, sess.ID)
		return json.NewEncoder(e.Out).Encode(v)
	}
	fmt.Fprintf(e.Out, "created %s (%s)\n", sess.Slug, sess.ID)
	if *start && sess.Runtime != nil {
		fmt.Fprintln(e.Out, session.Endpoint(sess.Runtime.Port))
	}
	return nil
}

func runList(e *Env, args []string) error {
	fs := newFlags("list", e)
	asJSON := fs.Bool("json", false, "JSON output")
	label := fs.String("label", "", "only sessions with this label")
	if _, err := parseInterleaved(fs, args); err != nil {
		return errUsage
	}
	svc, err := service(e)
	if err != nil {
		return err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	views, err := svc.List(ctx)
	if err != nil {
		return err
	}
	if *label != "" {
		filtered := views[:0]
		for _, v := range views {
			for _, l := range v.Labels {
				if strings.EqualFold(l, *label) {
					filtered = append(filtered, v)
					break
				}
			}
		}
		views = filtered
	}
	if *asJSON {
		if views == nil {
			views = []session.View{}
		}
		return json.NewEncoder(e.Out).Encode(views)
	}
	if len(views) == 0 {
		fmt.Fprintln(e.Out, "no sessions yet — create one with: berthkeep new \"My session\"")
		return nil
	}
	w := tabwriter.NewWriter(e.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SESSION\tSTATE\tPORT\tMODE\tPROFILE\tLABELS\tTITLE")
	for _, v := range views {
		portS := "auto"
		if p := v.FixedPort(); p != 0 {
			portS = strconv.Itoa(p)
		}
		if v.Status.Port != 0 && v.Status.Port != v.FixedPort() {
			portS += fmt.Sprintf(" (%d)", v.Status.Port)
		}
		persist := string(v.Persistence)
		if v.AutoRemove {
			persist = "temp"
		}
		state := string(v.Status.State)
		if len(v.Conflicts) > 0 {
			state += " ⚠"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", v.Slug, state, portS, v.Mode, persist, strings.Join(v.Labels, ","), v.Title)
	}
	return w.Flush()
}

func runStart(e *Env, args []string) error {
	fs := newFlags("start", e)
	headless := fs.Bool("headless", false, "start headless")
	headed := fs.Bool("headed", false, "start with a window")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return errUsage
	}
	ref, err := needOne(pos, "<session>")
	if err != nil {
		return err
	}
	var mode domain.Mode
	if *headless {
		mode = domain.Headless
	} else if *headed {
		mode = domain.Headed
	}
	svc, err := service(e)
	if err != nil {
		return err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	_, st, err := svc.Start(ctx, ref, mode)
	if err != nil {
		return err
	}
	fmt.Fprintln(e.Out, session.Endpoint(st.Port))
	return nil
}

func runStop(e *Env, args []string) error {
	fs := newFlags("stop", e)
	all := fs.Bool("all", false, "stop every running session")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return errUsage
	}
	svc, err := service(e)
	if err != nil {
		return err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	if *all {
		views, err := svc.List(ctx)
		if err != nil {
			return err
		}
		for _, v := range views {
			if v.Status.State == "running" || v.Status.State == "running-unverified" {
				if err := svc.Stop(ctx, v.ID); err != nil {
					fmt.Fprintf(e.Err, "%s: %v\n", v.Slug, err)
				} else {
					fmt.Fprintln(e.Out, "stopped", v.Slug)
				}
			}
		}
		return nil
	}
	ref, err := needOne(pos, "<session> or --all")
	if err != nil {
		return err
	}
	if err := svc.Stop(ctx, ref); err != nil {
		return err
	}
	fmt.Fprintln(e.Out, "stopped", ref)
	return nil
}

func runRestart(e *Env, args []string) error {
	ref, err := needOne(args, "<session>")
	if err != nil {
		return err
	}
	svc, err := service(e)
	if err != nil {
		return err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	_, st, err := svc.Restart(ctx, ref)
	if err != nil {
		return err
	}
	fmt.Fprintln(e.Out, session.Endpoint(st.Port))
	return nil
}

func runMode(e *Env, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: mode <session> headless|headed: %w", errUsage)
	}
	svc, err := service(e)
	if err != nil {
		return err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	sess, st, err := svc.SwitchMode(ctx, args[0], domain.Mode(args[1]))
	if err != nil {
		return err
	}
	if st.Port != 0 && (st.State == "running" || st.State == "running-unverified") {
		fmt.Fprintf(e.Out, "%s is %s at %s\n", sess.Slug, sess.Mode, session.Endpoint(st.Port))
	} else {
		fmt.Fprintf(e.Out, "%s will start %s\n", sess.Slug, sess.Mode)
	}
	return nil
}

func runPersist(e *Env, args []string) error {
	fs := newFlags("persist", e)
	now := fs.Bool("now", false, "restart now to move the profile immediately")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return errUsage
	}
	ref, err := needOne(pos, "<session>")
	if err != nil {
		return err
	}
	svc, err := service(e)
	if err != nil {
		return err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	sess, err := svc.Persist(ctx, ref, *now)
	if err != nil {
		return err
	}
	if sess.PendingRelocation != "" {
		fmt.Fprintf(e.Out, "%s is now persistent; its profile moves to %s when it stops\n", sess.Slug, sess.PendingRelocation)
	} else {
		fmt.Fprintf(e.Out, "%s is persistent (%s)\n", sess.Slug, sess.ProfileDir)
	}
	return nil
}

func runEphemeral(e *Env, args []string) error {
	ref, err := needOne(args, "<session>")
	if err != nil {
		return err
	}
	svc, err := service(e)
	if err != nil {
		return err
	}
	sess, err := svc.MakeEphemeral(ref)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Out, "%s is ephemeral: its profile is wiped on the next stop\n", sess.Slug)
	return nil
}

func runEndpoint(e *Env, args []string) error {
	fs := newFlags("endpoint", e)
	ensure := fs.Bool("ensure", false, "start the session if it isn't running")
	ws := fs.Bool("ws", false, "print the browser WebSocket URL")
	asJSON := fs.Bool("json", false, "JSON output")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return errUsage
	}
	ref, err := needOne(pos, "<session>")
	if err != nil {
		return err
	}
	svc, err := service(e)
	if err != nil {
		return err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	url, st, err := svc.EndpointFor(ctx, ref, *ensure)
	if err != nil {
		return err
	}
	wsURL := ""
	if v, err := cdp.GetVersion(ctx, st.Port); err == nil {
		wsURL = v.WebSocketDebuggerURL
	}
	switch {
	case *asJSON:
		return json.NewEncoder(e.Out).Encode(map[string]any{"http": url, "ws": wsURL, "port": st.Port})
	case *ws:
		fmt.Fprintln(e.Out, wsURL)
	default:
		fmt.Fprintln(e.Out, url)
	}
	return nil
}

func runEdit(e *Env, args []string) error {
	fs := newFlags("edit", e)
	title := fs.String("title", "", "new title")
	slug := fs.String("slug", "", "new slug")
	portS := fs.String("port", "", "fixed port, or auto")
	notes := fs.String("notes", "\x00", "notes")
	browser := fs.String("browser", "", "browser id")
	var add, remove, urls multi
	fs.Var(&add, "label-add", "add labels")
	fs.Var(&remove, "label-remove", "remove labels")
	fs.Var(&urls, "url", "replace start URLs")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return errUsage
	}
	ref, err := needOne(pos, "<session>")
	if err != nil {
		return err
	}
	svc, err := service(e)
	if err != nil {
		return err
	}
	sess, err := svc.Store.Modify(ref, func(x *domain.Session) error {
		if *title != "" {
			x.Title = *title
		}
		if *slug != "" {
			x.Slug = *slug
		}
		if *browser != "" {
			x.Browser = *browser
		}
		if *notes != "\x00" {
			x.Notes = *notes
		}
		switch *portS {
		case "":
		case "auto":
			x.Port = nil
		default:
			n, err := strconv.Atoi(*portS)
			if err != nil || n < 1024 || n > 65535 {
				return fmt.Errorf("--port must be 1024-65535 or auto: %w", domain.ErrInvalid)
			}
			x.Port = &n
		}
		for _, l := range add {
			if !contains(x.Labels, l) {
				x.Labels = append(x.Labels, l)
			}
		}
		kept := x.Labels[:0]
		for _, l := range x.Labels {
			if !contains(remove, l) {
				kept = append(kept, l)
			}
		}
		x.Labels = kept
		if len(urls) > 0 {
			x.StartURLs = urls
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Out, "updated %s\n", sess.Slug)
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

func runRm(e *Env, args []string) error {
	fs := newFlags("rm", e)
	keep := fs.Bool("keep-profile", false, "keep the profile folder")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return errUsage
	}
	ref, err := needOne(pos, "<session>")
	if err != nil {
		return err
	}
	svc, err := service(e)
	if err != nil {
		return err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	trashed, err := svc.Delete(ctx, ref, *keep)
	if err != nil {
		return err
	}
	if trashed != "" {
		fmt.Fprintf(e.Out, "deleted %s (profile moved to %s)\n", ref, trashed)
	} else {
		fmt.Fprintf(e.Out, "deleted %s\n", ref)
	}
	return nil
}

func runTabs(e *Env, args []string) error {
	ref, err := needOne(args, "<session>")
	if err != nil {
		return err
	}
	svc, err := service(e)
	if err != nil {
		return err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	_, st, err := svc.EndpointFor(ctx, ref, false)
	if err != nil {
		return err
	}
	pages, err := cdp.Pages(ctx, st.Port)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(e.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTITLE\tURL")
	for _, p := range pages {
		fmt.Fprintf(w, "%s\t%s\t%s\n", p.ID, truncate(p.Title, 40), p.URL)
	}
	return w.Flush()
}

func runOpen(e *Env, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: open <session> <url>: %w", errUsage)
	}
	svc, err := service(e)
	if err != nil {
		return err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	_, st, err := svc.EndpointFor(ctx, args[0], true)
	if err != nil {
		return err
	}
	id, err := cdp.OpenTab(ctx, st.Port, args[1])
	if err != nil {
		return err
	}
	fmt.Fprintln(e.Out, id)
	return nil
}

func runGC(e *Env, _ []string) error {
	svc, err := service(e)
	if err != nil {
		return err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	return svc.GC(ctx)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func init() {
	register("adopt", "register a running debug browser or folder: adopt <port|pid|dir> [--title t]", runAdopt)
}

func runAdopt(e *Env, args []string) error {
	fs := newFlags("adopt", e)
	title := fs.String("title", "", "session title")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return errUsage
	}
	ref, err := needOne(pos, "<port|pid|dir>")
	if err != nil {
		return err
	}
	in := session.AdoptInput{Title: *title}
	if n, err := strconv.Atoi(ref); err == nil {
		if n >= 1024 && n <= 65535 {
			in.Port = n
		}
		in.PID = n
	} else {
		in.Dir = ref
	}
	svc, err := service(e)
	if err != nil {
		return err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	sess, err := svc.Adopt(ctx, in)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Out, "adopted %s (%s)\n", sess.Slug, sess.ProfileDir)
	return nil
}
