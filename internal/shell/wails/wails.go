//go:build !server && (windows || cgo)

// Package wails is the native shell: window, tray, dialogs, single instance.
package wails

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	wevents "github.com/wailsapp/wails/v3/pkg/events"

	"github.com/berthkeep/berthkeep/internal/domain"
	"github.com/berthkeep/berthkeep/internal/events"
	"github.com/berthkeep/berthkeep/internal/launcher"
	"github.com/berthkeep/berthkeep/internal/session"
	"github.com/berthkeep/berthkeep/internal/shell"
	"github.com/berthkeep/berthkeep/internal/shell/browser"
)

// Available reports whether this build has the native shell.
const Available = true

// Options configure the shell.
type Options struct {
	Home string
	URL  func() string // UI address, known once the server listens
	Svc  func() *session.Service
	Hub  *events.Hub
	Quit func() // stops the service
	Tray bool   // start without a window
}

// Shell owns the Wails app.
type Shell struct {
	o    Options
	app  *application.App
	tray *application.SystemTray
	mu   sync.Mutex
	win  *application.WebviewWindow
}

// New creates the Wails app; call Run on the main goroutine.
func New(o Options) *Shell {
	s := &Shell{o: o}
	sum := sha256.Sum256([]byte(filepath.Clean(o.Home)))
	s.app = application.New(application.Options{
		Name:        "Berthkeep",
		Description: "Chromium CDP session manager",
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
			WebviewUserDataPath:           filepath.Join(o.Home, "ui-webview"),
		},
		Linux: application.LinuxOptions{DisableQuitOnLastWindowClosed: true},
		Mac:   application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               "io.berthkeep." + hex.EncodeToString(sum[:6]),
			OnSecondInstanceLaunch: func(application.SecondInstanceData) { _ = s.Open() },
		},
	})
	return s
}

// SetHub sets the event source for tray updates.
func (s *Shell) SetHub(h *events.Hub) { s.o.Hub = h }

// Run starts the tray and window and blocks until quit.
func (s *Shell) Run(ctx context.Context) error {
	s.tray = s.app.SystemTray.New()
	s.tray.SetIcon(shell.TrayIcon(false))
	s.tray.SetTooltip("Berthkeep")
	s.tray.OnClick(func() { _ = s.Open() })
	s.rebuildMenu(ctx)
	go s.followEvents(ctx)
	go func() {
		<-ctx.Done()
		s.app.Quit()
	}()
	if !s.o.Tray {
		s.mu.Lock()
		s.showWindow() // before Run: no main-thread dispatch yet
		s.mu.Unlock()
	}
	err := s.app.Run()
	if s.o.Quit != nil {
		s.o.Quit()
	}
	return err
}

// Open shows the manager window, creating it if needed.
func (s *Shell) Open() error {
	application.InvokeSync(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.showWindow()
	})
	return nil
}

// showWindow focuses or creates the window; caller holds mu.
func (s *Shell) showWindow() {
	if s.win != nil {
		s.win.Show()
		s.win.Restore()
		s.win.Focus()
		return
	}
	w := s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "main", Title: "Berthkeep", Width: 1100, Height: 760, MinWidth: 360, MinHeight: 480,
		URL: s.o.URL(),
	})
	w.OnWindowEvent(wevents.Common.WindowClosing, func(*application.WindowEvent) {
		s.mu.Lock()
		s.win = nil
		s.mu.Unlock()
		time.AfterFunc(3*time.Second, debug.FreeOSMemory) // return window memory to the OS
	})
	s.win = w
}

// PickFolder implements port.Shell.
func (s *Shell) PickFolder(_ context.Context, title string) (string, error) {
	if title == "" {
		title = "Choose a folder"
	}
	return s.app.Dialog.OpenFile().SetTitle(title).CanChooseDirectories(true).CanChooseFiles(false).PromptForSingleSelection()
}

// Reveal implements port.Shell.
func (s *Shell) Reveal(path string) error { return browser.Reveal(path) }

func (s *Shell) followEvents(ctx context.Context) {
	ch, cancel := s.o.Hub.Subscribe()
	defer cancel()
	var timer *time.Timer
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ch:
			if !ok {
				return
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(300*time.Millisecond, func() { s.rebuildMenu(ctx) })
		}
	}
}

func (s *Shell) rebuildMenu(ctx context.Context) {
	svc := s.o.Svc()
	views, _ := svc.List(ctx)
	sort.Slice(views, func(i, j int) bool { return views[i].Title < views[j].Title })
	menu := s.app.NewMenu()
	anyRunning := false
	for _, v := range views {
		v := v
		running := v.Status.State == launcher.Running || v.Status.State == launcher.RunningUnverified
		anyRunning = anyRunning || running
		mark, action := "○", "Start"
		label := v.Title
		if running {
			mark, action = "●", "Stop"
			label = fmt.Sprintf("%s  :%d", v.Title, v.Status.Port)
		}
		sub := menu.AddSubmenu(mark + "  " + label)
		sub.Add(action).OnClick(func(*application.Context) {
			go func() {
				c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				if running {
					_ = svc.Stop(c, v.ID)
				} else {
					_, _, _ = svc.Start(c, v.ID, domain.Mode(""))
				}
				s.o.Hub.Publish(events.Event{Type: "sessions.changed"})
			}()
		})
		if running {
			sub.Add("Copy endpoint").OnClick(func(*application.Context) {
				s.app.Clipboard.SetText(session.Endpoint(v.Status.Port))
			})
		}
	}
	if len(views) > 0 {
		menu.AddSeparator()
	}
	menu.Add("Open Berthkeep").OnClick(func(*application.Context) { _ = s.Open() })
	menu.Add("New quick temp session").OnClick(func(*application.Context) {
		go func() {
			c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			_, _ = svc.Create(c, session.CreateInput{Title: "Quick temp " + time.Now().Format("15:04"), Persistence: "temp", Start: true})
			s.o.Hub.Publish(events.Event{Type: "sessions.changed"})
		}()
	})
	menu.AddSeparator()
	menu.Add("Quit (browsers keep running)").OnClick(func(*application.Context) { s.app.Quit() })
	s.tray.SetMenu(menu)
	s.tray.SetIcon(shell.TrayIcon(anyRunning))
}
