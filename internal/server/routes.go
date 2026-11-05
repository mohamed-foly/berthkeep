package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mohamed-foly/berthkeep/internal/cdp"
	"github.com/mohamed-foly/berthkeep/internal/config"
	"github.com/mohamed-foly/berthkeep/internal/domain"
	"github.com/mohamed-foly/berthkeep/internal/launcher"
	"github.com/mohamed-foly/berthkeep/internal/ports"
	"github.com/mohamed-foly/berthkeep/internal/session"
	"github.com/mohamed-foly/berthkeep/web"
)

func (s *Server) routes(m *http.ServeMux) {
	m.HandleFunc("GET /{$}", s.serveIndex)
	m.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(web.FS)))
	m.HandleFunc("GET /api/ping", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"ok": "berthkeep"}) })
	m.HandleFunc("GET /api/state", s.state)
	m.HandleFunc("GET /api/events", s.events)
	m.HandleFunc("POST /api/sessions", s.create)
	m.HandleFunc("GET /api/sessions/{ref}", s.get)
	m.HandleFunc("PATCH /api/sessions/{ref}", s.patch)
	m.HandleFunc("DELETE /api/sessions/{ref}", s.remove)
	m.HandleFunc("POST /api/sessions/{ref}/start", s.start)
	m.HandleFunc("POST /api/sessions/{ref}/stop", s.stop)
	m.HandleFunc("POST /api/sessions/{ref}/restart", s.restart)
	m.HandleFunc("POST /api/sessions/{ref}/mode", s.mode)
	m.HandleFunc("POST /api/sessions/{ref}/persist", s.persist)
	m.HandleFunc("POST /api/sessions/{ref}/ephemeral", s.ephemeral)
	m.HandleFunc("POST /api/sessions/{ref}/clone", s.clone)
	m.HandleFunc("GET /api/sessions/{ref}/endpoint", s.endpoint)
	m.HandleFunc("GET /api/sessions/{ref}/targets", s.targets)
	m.HandleFunc("POST /api/sessions/{ref}/targets", s.openTab)
	m.HandleFunc("DELETE /api/sessions/{ref}/targets/{id}", s.closeTab)
	m.HandleFunc("POST /api/sessions/{ref}/targets/{id}/activate", s.activateTab)
	m.HandleFunc("GET /api/sessions/{ref}/targets/{id}/screenshot", s.screenshot)
	m.HandleFunc("GET /api/sessions/{ref}/log", s.log)
	m.HandleFunc("GET /api/detect", s.detect)
	m.HandleFunc("POST /api/adopt", s.adopt)
	m.HandleFunc("GET /api/browsers", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, s.Service().Browsers.Browsers()) })
	m.HandleFunc("GET /api/config", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, s.Service().Config) })
	m.HandleFunc("PUT /api/config", s.putConfig)
	m.HandleFunc("GET /api/ports/check", s.checkPort)
	m.HandleFunc("POST /api/gc", s.gc)
	m.HandleFunc("POST /api/trash/empty", s.emptyTrash)
	m.HandleFunc("POST /api/shell/pick-folder", s.pickFolder)
	m.HandleFunc("POST /api/shell/reveal", s.reveal)
	m.HandleFunc("POST /api/ui/open", s.openUI)
	m.HandleFunc("POST /api/quit", s.quit)
}

func ctxOf(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 90*time.Second)
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxOf(r)
	defer cancel()
	svc := s.Service()
	views, err := svc.List(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	if views == nil {
		views = []session.View{}
	}
	writeJSON(w, 200, map[string]any{"sessions": views, "home": s.Home, "version": s.Version, "ports": svc.Config.Ports, "shell": s.Shell != nil})
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	ch, cancel := s.Hub.Subscribe()
	defer cancel()
	fmt.Fprint(w, ": connected\n\n")
	fl.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case e, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: ", e.Type)
			b, _ := jsonMarshal(e.Data)
			_, _ = w.Write(b)
			fmt.Fprint(w, "\n\n")
			fl.Flush()
		}
	}
}

type createReq struct {
	Title       string   `json:"title"`
	Labels      []string `json:"labels"`
	Notes       string   `json:"notes"`
	Persistence string   `json:"persistence"`
	Port        int      `json:"port"`
	Browser     string   `json:"browser"`
	BrowserPath string   `json:"browserPath"`
	Mode        string   `json:"mode"`
	URLs        []string `json:"startUrls"`
	ExtraArgs   []string `json:"extraArgs"`
	CloneFrom   string   `json:"cloneFrom"`
	CloneLive   bool     `json:"cloneLive"`
	AdoptDir    string   `json:"adoptDir"`
	Start       bool     `json:"start"`
}

func (c createReq) input() session.CreateInput {
	return session.CreateInput{Title: c.Title, Labels: c.Labels, Notes: c.Notes, Persistence: c.Persistence, Port: c.Port,
		Browser: c.Browser, BrowserPath: c.BrowserPath, Headless: c.Mode == "headless", Headed: c.Mode == "headed",
		URLs: c.URLs, ExtraArgs: c.ExtraArgs, CloneFrom: c.CloneFrom, CloneLive: c.CloneLive, AdoptDir: c.AdoptDir, Start: c.Start}
}

func (s *Server) respondView(w http.ResponseWriter, r *http.Request, status int, ref string) {
	ctx, cancel := ctxOf(r)
	defer cancel()
	v, err := s.Service().Get(ctx, ref)
	if err != nil {
		fail(w, err)
		return
	}
	s.Hub.Publish(eventFor(v))
	writeJSON(w, status, v)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var req createReq
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := ctxOf(r)
	defer cancel()
	sess, err := s.Service().Create(ctx, req.input())
	if err != nil {
		fail(w, err)
		return
	}
	s.respondView(w, r, http.StatusCreated, sess.ID)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxOf(r)
	defer cancel()
	v, err := s.Service().Get(ctx, r.PathValue("ref"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}

type patchReq struct {
	Title       *string           `json:"title"`
	Slug        *string           `json:"slug"`
	Labels      *[]string         `json:"labels"`
	Notes       *string           `json:"notes"`
	Color       *string           `json:"color"`
	Port        *int              `json:"port"` // 0 = auto
	Browser     *string           `json:"browser"`
	BrowserPath *string           `json:"browserPath"`
	Mode        *string           `json:"mode"`
	StartURLs   *[]string         `json:"startUrls"`
	RestoreTabs *bool             `json:"restoreTabs"`
	ExtraArgs   *[]string         `json:"extraArgs"`
	Window      *domain.Window    `json:"window"`
	Env         map[string]string `json:"env"`
}

func (s *Server) patch(w http.ResponseWriter, r *http.Request) {
	var p patchReq
	if err := decode(r, &p); err != nil {
		fail(w, err)
		return
	}
	sess, err := s.Service().Store.Modify(r.PathValue("ref"), func(x *domain.Session) error {
		set := func(dst *string, v *string) {
			if v != nil {
				*dst = *v
			}
		}
		set(&x.Title, p.Title)
		set(&x.Slug, p.Slug)
		set(&x.Notes, p.Notes)
		set(&x.Color, p.Color)
		set(&x.Browser, p.Browser)
		set(&x.BrowserPath, p.BrowserPath)
		if p.Labels != nil {
			x.Labels = *p.Labels
		}
		if p.Port != nil {
			switch n := *p.Port; {
			case n == 0:
				x.Port = nil
			case n >= 1024 && n <= 65535:
				x.Port = &n
			default:
				return fmt.Errorf("port %d out of range: %w", n, domain.ErrInvalid)
			}
		}
		if p.Mode != nil {
			if *p.Mode != string(domain.Headed) && *p.Mode != string(domain.Headless) {
				return fmt.Errorf("mode must be headed or headless: %w", domain.ErrInvalid)
			}
			x.Mode = domain.Mode(*p.Mode)
		}
		if p.StartURLs != nil {
			x.StartURLs = *p.StartURLs
		}
		if p.RestoreTabs != nil {
			x.RestoreTabs = *p.RestoreTabs
		}
		if p.ExtraArgs != nil {
			x.ExtraArgs = *p.ExtraArgs
		}
		if p.Window != nil {
			x.Window = p.Window
		}
		if p.Env != nil {
			x.Env = p.Env
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	s.respondView(w, r, 200, sess.ID)
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxOf(r)
	defer cancel()
	ref := r.PathValue("ref")
	trashed, err := s.Service().Delete(ctx, ref, r.URL.Query().Get("keepProfile") == "1")
	if err != nil {
		fail(w, err)
		return
	}
	s.Hub.Publish(eventRemoved(ref))
	writeJSON(w, 200, map[string]string{"deleted": ref, "trashedTo": trashed})
}

type modeReq struct {
	Mode string `json:"mode"`
	Now  bool   `json:"now"`
	Live bool   `json:"live"`
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	var req modeReq
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := ctxOf(r)
	defer cancel()
	sess, _, err := s.Service().Start(ctx, r.PathValue("ref"), domain.Mode(req.Mode))
	if err != nil {
		fail(w, err)
		return
	}
	s.respondView(w, r, 200, sess.ID)
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxOf(r)
	defer cancel()
	ref := r.PathValue("ref")
	if err := s.Service().Stop(ctx, ref); err != nil {
		fail(w, err)
		return
	}
	if _, err := s.Service().Store.Get(ref); err != nil { // quick temp removed itself
		s.Hub.Publish(eventRemoved(ref))
		writeJSON(w, 200, map[string]string{"removed": ref})
		return
	}
	s.respondView(w, r, 200, ref)
}

func (s *Server) restart(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxOf(r)
	defer cancel()
	sess, _, err := s.Service().Restart(ctx, r.PathValue("ref"))
	if err != nil {
		fail(w, err)
		return
	}
	s.respondView(w, r, 200, sess.ID)
}

func (s *Server) mode(w http.ResponseWriter, r *http.Request) {
	var req modeReq
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := ctxOf(r)
	defer cancel()
	sess, _, err := s.Service().SwitchMode(ctx, r.PathValue("ref"), domain.Mode(req.Mode))
	if err != nil {
		fail(w, err)
		return
	}
	s.respondView(w, r, 200, sess.ID)
}

func (s *Server) persist(w http.ResponseWriter, r *http.Request) {
	var req modeReq
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := ctxOf(r)
	defer cancel()
	sess, err := s.Service().Persist(ctx, r.PathValue("ref"), req.Now)
	if err != nil {
		fail(w, err)
		return
	}
	s.respondView(w, r, 200, sess.ID)
}

func (s *Server) ephemeral(w http.ResponseWriter, r *http.Request) {
	sess, err := s.Service().MakeEphemeral(r.PathValue("ref"))
	if err != nil {
		fail(w, err)
		return
	}
	s.respondView(w, r, 200, sess.ID)
}

func (s *Server) clone(w http.ResponseWriter, r *http.Request) {
	var req createReq
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	in := req.input()
	in.CloneFrom = r.PathValue("ref")
	ctx, cancel := ctxOf(r)
	defer cancel()
	sess, err := s.Service().Create(ctx, in)
	if err != nil {
		fail(w, err)
		return
	}
	s.respondView(w, r, http.StatusCreated, sess.ID)
}

func (s *Server) endpoint(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxOf(r)
	defer cancel()
	url, st, err := s.Service().EndpointFor(ctx, r.PathValue("ref"), r.URL.Query().Get("ensure") == "1")
	if err != nil {
		fail(w, err)
		return
	}
	ws := ""
	if v, err := cdp.GetVersion(ctx, st.Port); err == nil {
		ws = v.WebSocketDebuggerURL
	}
	writeJSON(w, 200, map[string]any{"http": url, "ws": ws, "port": st.Port})
}

// runningPort returns the live port of a session or writes an error.
func (s *Server) runningPort(w http.ResponseWriter, r *http.Request) (context.Context, context.CancelFunc, int, bool) {
	ctx, cancel := ctxOf(r)
	v, err := s.Service().Get(ctx, r.PathValue("ref"))
	if err != nil {
		cancel()
		fail(w, err)
		return nil, nil, 0, false
	}
	if v.Status.State != launcher.Running && v.Status.State != launcher.RunningUnverified {
		cancel()
		writeErr(w, http.StatusConflict, v.Slug+" is not running")
		return nil, nil, 0, false
	}
	return ctx, cancel, v.Status.Port, true
}

func (s *Server) targets(w http.ResponseWriter, r *http.Request) {
	ctx, cancel, p, ok := s.runningPort(w, r)
	if !ok {
		return
	}
	defer cancel()
	pages, err := cdp.Pages(ctx, p)
	if err != nil {
		fail(w, err)
		return
	}
	if pages == nil {
		pages = []cdp.Target{}
	}
	writeJSON(w, 200, pages)
}

func (s *Server) openTab(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	if req.URL == "" {
		req.URL = "about:blank"
	}
	ctx, cancel, p, ok := s.runningPort(w, r)
	if !ok {
		return
	}
	defer cancel()
	id, err := cdp.OpenTab(ctx, p, req.URL)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) closeTab(w http.ResponseWriter, r *http.Request) {
	ctx, cancel, p, ok := s.runningPort(w, r)
	if !ok {
		return
	}
	defer cancel()
	if err := cdp.CloseTab(ctx, p, r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"closed": r.PathValue("id")})
}

func (s *Server) activateTab(w http.ResponseWriter, r *http.Request) {
	ctx, cancel, p, ok := s.runningPort(w, r)
	if !ok {
		return
	}
	defer cancel()
	if err := cdp.ActivateTab(ctx, p, r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"activated": r.PathValue("id")})
}

func (s *Server) screenshot(w http.ResponseWriter, r *http.Request) {
	ctx, cancel, p, ok := s.runningPort(w, r)
	if !ok {
		return
	}
	defer cancel()
	q, _ := strconv.Atoi(r.URL.Query().Get("quality"))
	if q <= 0 || q > 100 {
		q = 60
	}
	img, err := cdp.Screenshot(ctx, p, r.PathValue("id"), q)
	if err != nil {
		if err == cdp.ErrNoTarget {
			writeErr(w, 404, err.Error())
			return
		}
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(img) //nolint:gosec // JPEG bytes served as image/jpeg
}

func (s *Server) log(w http.ResponseWriter, r *http.Request) {
	sess, err := s.Service().Store.Get(r.PathValue("ref"))
	if err != nil {
		fail(w, err)
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if n <= 0 || n > 2000 {
		n = 200
	}
	tail := launcher.TailLog(filepath.Join(s.Home, "logs", sess.Slug+".log"), n)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(tail))
}

func (s *Server) detect(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxOf(r)
	defer cancel()
	list, err := s.Service().Detect(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) adopt(w http.ResponseWriter, r *http.Request) {
	var in session.AdoptInput
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := ctxOf(r)
	defer cancel()
	sess, err := s.Service().Adopt(ctx, in)
	if err != nil {
		fail(w, err)
		return
	}
	s.respondView(w, r, http.StatusCreated, sess.ID)
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	var c config.Config
	if err := decode(r, &c); err != nil {
		fail(w, err)
		return
	}
	if err := c.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := config.Save(s.Home, c); err != nil {
		fail(w, err)
		return
	}
	if s.Reload != nil {
		svc, err := s.Reload()
		if err != nil {
			fail(w, err)
			return
		}
		s.setService(svc)
	}
	writeJSON(w, 200, c)
}

func (s *Server) checkPort(w http.ResponseWriter, r *http.Request) {
	p, err := strconv.Atoi(r.URL.Query().Get("port"))
	if err != nil || p < 1 || p > 65535 {
		writeErr(w, 400, "port query parameter required")
		return
	}
	list, err := s.Service().Store.List()
	if err != nil {
		fail(w, err)
		return
	}
	reserved := []string{}
	for _, x := range list {
		if x.FixedPort() == p {
			reserved = append(reserved, x.Slug)
		}
	}
	writeJSON(w, 200, map[string]any{"port": p, "free": ports.IsFree(p), "listening": ports.IsListening(p), "reservedBy": reserved})
}

func (s *Server) gc(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxOf(r)
	defer cancel()
	if err := s.Service().GC(ctx); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"gc": "done"})
}

func (s *Server) emptyTrash(w http.ResponseWriter, _ *http.Request) {
	dir := filepath.Join(s.Home, "trash")
	if err := os.RemoveAll(dir); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"trash": "empty"})
}

func (s *Server) pickFolder(w http.ResponseWriter, r *http.Request) {
	if s.Shell == nil {
		writeErr(w, http.StatusNotImplemented, "no native shell")
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	path, err := s.Shell.PickFolder(r.Context(), req.Title)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"path": path})
}

func (s *Server) reveal(w http.ResponseWriter, r *http.Request) {
	if s.Shell == nil {
		writeErr(w, http.StatusNotImplemented, "no native shell")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	if err := s.Shell.Reveal(req.Path); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"revealed": req.Path})
}

func (s *Server) openUI(w http.ResponseWriter, _ *http.Request) {
	if s.OpenUI == nil {
		writeErr(w, http.StatusNotImplemented, "no UI in this mode; open the address in a browser")
		return
	}
	if err := s.OpenUI(); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"ui": "open"})
}

func (s *Server) quit(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]string{"quit": "ok"})
	if s.Quit != nil {
		go func() { time.Sleep(100 * time.Millisecond); s.Quit() }()
	}
}
