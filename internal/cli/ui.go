package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"time"

	"github.com/berthkeep/berthkeep/internal/app"
	"github.com/berthkeep/berthkeep/internal/detach"
	"github.com/berthkeep/berthkeep/internal/server"
)

func init() {
	register("ui", "open the manager window (starts the service if needed)", runUI)
	register("gui", "run the service with window and tray in the foreground: gui [--tray]", runGUI)
	register("quit", "stop the background service (browsers keep running)", runQuit)
}

// postToService calls a running service; ok is false when none runs.
func postToService(home, path string) (bool, error) {
	in, alive := server.ReadInfo(home)
	if !alive {
		return false, nil
	}
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d%s", in.Port, path), nil)
	req.Header.Set(server.TokenHeader, in.Token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return true, err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return true, fmt.Errorf("service answered %s", resp.Status)
	}
	return true, nil
}

func runUI(e *Env, _ []string) error {
	if ok, err := postToService(e.Home, "/api/ui/open"); ok {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--home", e.Home, "gui")
	detach.Apply(cmd, false) // hiding would hide the first window too
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	fmt.Fprintln(e.Out, "Berthkeep is starting; look for its window and tray icon")
	return nil
}

func runGUI(e *Env, args []string) error {
	fs := newFlags("gui", e)
	tray := fs.Bool("tray", false, "start in the tray without a window")
	if _, err := parseInterleaved(fs, args); err != nil {
		return errUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return app.RunGUI(ctx, e.Home, Version, *tray)
}

func runQuit(e *Env, _ []string) error {
	ok, err := postToService(e.Home, "/api/quit")
	if !ok {
		fmt.Fprintln(e.Out, "no service is running")
		return nil
	}
	if err == nil {
		fmt.Fprintln(e.Out, "service stopped")
	}
	return err
}
