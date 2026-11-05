package server

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/mohamed-foly/berthkeep/internal/adapters/browsers"
	"github.com/mohamed-foly/berthkeep/internal/adapters/lock"
	"github.com/mohamed-foly/berthkeep/internal/adapters/procs"
	"github.com/mohamed-foly/berthkeep/internal/config"
	"github.com/mohamed-foly/berthkeep/internal/events"
	"github.com/mohamed-foly/berthkeep/internal/launcher"
	"github.com/mohamed-foly/berthkeep/internal/registry"
	"github.com/mohamed-foly/berthkeep/internal/session"
)

// testServer runs the handler on a real loopback listener so Host checks apply.
func testServer(t *testing.T, rangeStart int) (*Server, string) {
	t.Helper()
	home := t.TempDir()
	cfg := config.Default()
	cfg.Server.Port = rangeStart - 1
	cfg.Ports.RangeStart, cfg.Ports.RangeEnd = rangeStart, rangeStart+49
	cfg.Defaults.Mode = "headless"
	svc := &session.Service{Home: home, Config: cfg, Store: registry.New(home, nil),
		Launcher: &launcher.Launcher{Procs: procs.New(), Locks: lock.New()}, Browsers: browsers.New(nil)}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := New(home, ln.Addr().(*net.TCPAddr).Port, svc, events.NewHub())
	hs := httptest.NewUnstartedServer(s.Handler())
	hs.Listener.Close()
	hs.Listener = ln
	hs.Start()
	t.Cleanup(hs.Close)
	return s, "http://127.0.0.1:" + strconv.Itoa(s.Port)
}

func call(t *testing.T, s *Server, method, url, body string, token bool) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token {
		req.Header.Set(TokenHeader, s.Token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

func TestSecurityRules(t *testing.T) {
	s, base := testServer(t, 19400)
	if r, _ := call(t, s, "POST", base+"/api/sessions", `{"title":"x"}`, false); r.StatusCode != 403 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	req, _ := http.NewRequest("GET", base+"/api/state", nil)
	req.Host = "evil.test"
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("bad host: %v", r.StatusCode)
	}
	req, _ = http.NewRequest("POST", base+"/api/sessions", strings.NewReader("title=x"))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set(TokenHeader, s.Token)
	if r, err = http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain: %d", r.StatusCode)
	}
	if r, _ := call(t, s, "GET", base+"/api/sessions/x/endpoint?ensure=1", "", false); r.StatusCode != 403 {
		t.Fatalf("ensure without token: %d", r.StatusCode)
	}
	if r, _ := call(t, s, "GET", base+"/api/events", "", false); r.StatusCode != 403 {
		t.Fatalf("events without token: %d", r.StatusCode)
	}
	r, body := call(t, s, "GET", base+"/", "", false)
	if r.StatusCode != 200 || !strings.Contains(body, s.Token) || r.Header.Get("Content-Security-Policy") == "" {
		t.Fatalf("index: %d token=%v", r.StatusCode, strings.Contains(body, s.Token))
	}
	if r.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("CORS must never be enabled")
	}
}

func TestSessionCRUD(t *testing.T) {
	s, base := testServer(t, 19450)
	r, body := call(t, s, "POST", base+"/api/sessions", `{"title":"Shop admin","labels":["a"],"persistence":"ephemeral"}`, true)
	if r.StatusCode != 201 {
		t.Fatalf("create: %d %s", r.StatusCode, body)
	}
	_, body = call(t, s, "GET", base+"/api/state", "", false)
	var st struct {
		Sessions []session.View `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(body), &st); err != nil || len(st.Sessions) != 1 || st.Sessions[0].Slug != "shop-admin" {
		t.Fatalf("state: %s", body)
	}
	if string(st.Sessions[0].Status.State) != "stopped" {
		t.Fatalf("new session should be stopped: %+v", st.Sessions[0].Status)
	}
	if r, body = call(t, s, "PATCH", base+"/api/sessions/shop-admin", `{"labels":["a","b"],"port":19470}`, true); r.StatusCode != 200 || !strings.Contains(body, `"b"`) {
		t.Fatalf("patch: %d %s", r.StatusCode, body)
	}
	if r, _ = call(t, s, "PATCH", base+"/api/sessions/shop-admin", `{"port":80}`, true); r.StatusCode != 400 {
		t.Fatalf("bad port should be 400: %d", r.StatusCode)
	}
	if _, body = call(t, s, "GET", base+"/api/ports/check?port=19470", "", false); !strings.Contains(body, `"reservedBy":["shop-admin"]`) {
		t.Fatalf("ports check: %s", body)
	}
	if r, _ = call(t, s, "GET", base+"/api/sessions/nope", "", false); r.StatusCode != 404 {
		t.Fatalf("missing: %d", r.StatusCode)
	}
	if r, _ = call(t, s, "POST", base+"/api/sessions/shop-admin/stop", "", true); r.StatusCode != 200 {
		t.Fatalf("stop stopped session: %d", r.StatusCode)
	}
	if r, _ = call(t, s, "DELETE", base+"/api/sessions/shop-admin", "", true); r.StatusCode != 200 {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if r, _ = call(t, s, "POST", base+"/api/shell/pick-folder", "", true); r.StatusCode != http.StatusNotImplemented {
		t.Fatalf("no shell should be 501: %d", r.StatusCode)
	}
}

func TestEventsStream(t *testing.T) {
	s, base := testServer(t, 19500)
	resp, err := http.Get(base + "/api/events?t=" + s.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rd := bufio.NewReader(resp.Body)
	if line, _ := rd.ReadString('\n'); !strings.HasPrefix(line, ": connected") {
		t.Fatalf("first line %q", line)
	}
	s.Hub.Publish(events.Event{Type: "session.status", Data: map[string]string{"slug": "x"}})
	rd.ReadString('\n') // blank line
	if line, _ := rd.ReadString('\n'); strings.TrimSpace(line) != "event: session.status" {
		t.Fatalf("event line %q", line)
	}
}
