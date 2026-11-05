// Package server is the local HTTP API and UI host.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/berthkeep/berthkeep/internal/domain"
	"github.com/berthkeep/berthkeep/internal/events"
	"github.com/berthkeep/berthkeep/internal/fsutil"
	"github.com/berthkeep/berthkeep/internal/port"
	"github.com/berthkeep/berthkeep/internal/session"
	"github.com/berthkeep/berthkeep/web"
)

// InfoFile is written to the home while the service runs.
const InfoFile = "server.json"

// TokenHeader authenticates mutating requests.
const TokenHeader = "X-Berthkeep-Token"

// Info is the content of server.json.
type Info struct {
	Port      int       `json:"port"`
	PID       int       `json:"pid"`
	Token     string    `json:"token"`
	StartedAt time.Time `json:"startedAt"`
	Version   string    `json:"version"`
}

// Server serves the API and the UI.
type Server struct {
	Home    string
	Port    int
	Token   string
	Version string
	Hub     *events.Hub
	Shell   port.Shell   // nil: native actions return 501
	OpenUI  func() error // shows the manager window
	Quit    func()       // stops the service

	mu  sync.RWMutex
	svc *session.Service
	// Reload rebuilds the service after a config change.
	Reload func() (*session.Service, error)
}

// New returns a Server with a fresh token.
func New(home string, portNum int, svc *session.Service, hub *events.Hub) *Server {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return &Server{Home: home, Port: portNum, Token: hex.EncodeToString(b), Hub: hub, svc: svc}
}

// Service returns the current session service.
func (s *Server) Service() *session.Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.svc
}

func (s *Server) setService(svc *session.Service) {
	s.mu.Lock()
	s.svc = svc
	s.mu.Unlock()
}

// ErrRunning means another service already serves this home.
var ErrRunning = errors.New("berthkeep service already running")

// ReadInfo loads server.json and checks that its service answers.
func ReadInfo(home string) (Info, bool) {
	b, err := os.ReadFile(filepath.Join(home, InfoFile))
	if err != nil {
		return Info{}, false
	}
	var in Info
	if json.Unmarshal(b, &in) != nil || in.Port == 0 {
		return Info{}, false
	}
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/ping", in.Port), nil)
	req.Header.Set(TokenHeader, in.Token)
	resp, err := (&http.Client{Timeout: time.Second}).Do(req)
	if err != nil {
		return in, false
	}
	resp.Body.Close()
	return in, resp.StatusCode == http.StatusOK
}

// ListenAndServe serves until ctx ends, maintaining server.json.
func (s *Server) ListenAndServe(ctx context.Context) error {
	if in, alive := ReadInfo(s.Home); alive {
		return fmt.Errorf("%w on port %d (pid %d)", ErrRunning, in.Port, in.PID)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.Port)))
	if err != nil {
		return fmt.Errorf("listen on 127.0.0.1:%d: %w", s.Port, err)
	}
	s.Port = ln.Addr().(*net.TCPAddr).Port
	info, _ := json.MarshalIndent(Info{Port: s.Port, PID: os.Getpid(), Token: s.Token, StartedAt: time.Now().UTC(), Version: s.Version}, "", "  ")
	if err := fsutil.WriteFileAtomic(filepath.Join(s.Home, InfoFile), info, 0o600); err != nil {
		ln.Close()
		return err
	}
	defer os.Remove(filepath.Join(s.Home, InfoFile))
	hs := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = hs.Shutdown(sh)
	}()
	if err := hs.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Handler returns the routed, secured handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.routes(mux)
	return s.secure(mux)
}

// secure enforces the Host allow-list, the token and JSON bodies.
func (s *Server) secure(next http.Handler) http.Handler {
	allowed := map[string]bool{
		fmt.Sprintf("127.0.0.1:%d", s.Port): true,
		fmt.Sprintf("localhost:%d", s.Port): true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[strings.ToLower(r.Host)] {
			writeErr(w, http.StatusMisdirectedRequest, "host not allowed")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			mutating := r.Method != http.MethodGet && r.Method != http.MethodHead
			needs := mutating || r.URL.Query().Get("ensure") != "" || r.URL.Path == "/api/events" || r.URL.Path == "/api/ping"
			if needs && !s.tokenOK(r) {
				writeErr(w, http.StatusForbidden, "missing or invalid token")
				return
			}
			if mutating && r.ContentLength != 0 && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				writeErr(w, http.StatusUnsupportedMediaType, "JSON body required")
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) tokenOK(r *http.Request) bool {
	t := r.Header.Get(TokenHeader)
	if t == "" && r.URL.Path == "/api/events" {
		t = r.URL.Query().Get("t")
	}
	return t != "" && t == s.Token
}

func (s *Server) serveIndex(w http.ResponseWriter, _ *http.Request) {
	b, err := fs.ReadFile(web.FS, "index.html")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
	w.Write([]byte(strings.ReplaceAll(string(b), "{{TOKEN}}", s.Token)))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// fail maps domain errors to HTTP status codes.
func fail(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrBusy):
		status = http.StatusConflict
	case errors.Is(err, domain.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, session.ErrStartFailed):
		status = http.StatusBadGateway
	}
	writeErr(w, status, err.Error())
}

func decode(r *http.Request, v any) error {
	if r.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("bad JSON: %v: %w", err, domain.ErrInvalid)
	}
	return nil
}
