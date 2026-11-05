package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/berthkeep/berthkeep/internal/adapters/browsers"
	"github.com/berthkeep/berthkeep/internal/server"
	"github.com/berthkeep/berthkeep/internal/session"
	"github.com/berthkeep/berthkeep/internal/shell"
	"github.com/berthkeep/berthkeep/internal/shell/appwindow"
	"github.com/berthkeep/berthkeep/internal/shell/browser"
	"github.com/berthkeep/berthkeep/internal/shell/wails"
)

// EnvShell forces a shell: wails, appwindow or browser.
const EnvShell = "BERTHKEEP_SHELL"

// RunGUI runs the service plus a UI shell. Call it on the main goroutine.
func RunGUI(ctx context.Context, home, version string) error {
	debug.SetGCPercent(50) // long-lived tray process: keep the heap small
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu    sync.Mutex
		srv   *server.Server
		ready = make(chan struct{})
	)
	current := func() *server.Server { mu.Lock(); defer mu.Unlock(); return srv }
	url := func() string { s := current(); return fmt.Sprintf("http://127.0.0.1:%d/", s.Port) }
	svc := func() *session.Service { return current().Service() }

	choice := os.Getenv(EnvShell)
	useWails := wails.Available && (choice == "" || choice == "wails")
	var native *wails.Shell
	if useWails {
		native = wails.New(wails.Options{Home: home, URL: url, Svc: svc, Quit: cancel})
	}

	errc := make(chan error, 1)
	go func() {
		errc <- Serve(ctx, ServeOptions{Home: home, Version: version, Ready: func(s *server.Server) {
			mu.Lock()
			srv = s
			mu.Unlock()
			s.Quit = cancel
			if native != nil {
				s.Shell = native
				s.OpenUI = native.Open
			} else {
				s.OpenUI = fallbackUI(s, home, choice).Open
			}
			close(ready)
		}})
	}()
	select {
	case err := <-errc:
		return err
	case <-ready:
	}
	waitListening(current())

	if native != nil {
		native.SetHub(current().Hub)
		if err := native.Run(ctx); err != nil && err != shell.ErrUnavailable {
			return err
		}
		cancel()
		return <-errc
	}
	if err := current().OpenUI(); err != nil {
		slog.Warn("open ui", "err", err)
	}
	return <-errc
}

// fallbackUI picks the app window or the default browser.
func fallbackUI(s *server.Server, home, choice string) shell.UI {
	url := fmt.Sprintf("http://127.0.0.1:%d/", s.Port)
	if choice == "browser" {
		return browser.UI{URL: url}
	}
	return firstWorking{
		appwindow.UI{URL: url, Home: home, Browsers: browsers.New(nil)},
		browser.UI{URL: url},
	}
}

// firstWorking tries each UI in order.
type firstWorking []shell.UI

func (f firstWorking) Open() error {
	var err error
	for _, u := range f {
		if err = u.Open(); err == nil {
			return nil
		}
	}
	return err
}

func waitListening(s *server.Server) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := server.ReadInfo(s.Home); ok {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}
