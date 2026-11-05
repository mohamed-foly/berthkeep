//go:build integration

package server

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mohamed-foly/berthkeep/internal/session"
	"github.com/mohamed-foly/berthkeep/internal/watcher"
)

func TestStartTabsScreenshotAndWatcher(t *testing.T) {
	s, base := testServer(t, 19550)
	if len(s.Service().Browsers.Browsers()) == 0 {
		t.Skip("no browser installed")
	}
	r, body := call(t, s, "POST", base+"/api/sessions", `{"title":"peek","persistence":"ephemeral","mode":"headless","start":true}`, true)
	if r.StatusCode != 201 {
		t.Fatalf("create+start: %d %s", r.StatusCode, body)
	}
	var v session.View
	json.Unmarshal([]byte(body), &v)
	if v.Status.State != "running" || v.Runtime == nil {
		t.Fatalf("not running: %+v", v.Status)
	}
	if r, body = call(t, s, "POST", base+"/api/sessions/peek/targets", `{"url":"data:text/html,<h1 style='font-size:80px'>berthkeep</h1>"}`, true); r.StatusCode != 201 {
		t.Fatalf("open tab: %d %s", r.StatusCode, body)
	}
	var tab struct{ ID string }
	json.Unmarshal([]byte(body), &tab)
	time.Sleep(500 * time.Millisecond)
	r, img := call(t, s, "GET", base+"/api/sessions/peek/targets/"+tab.ID+"/screenshot", "", false)
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "image/jpeg" || !strings.HasPrefix(img, "\xff\xd8") {
		t.Fatalf("screenshot: %d %s len=%d", r.StatusCode, r.Header.Get("Content-Type"), len(img))
	}
	if _, body = call(t, s, "GET", base+"/api/sessions/peek/endpoint", "", false); !strings.Contains(body, `"ws":"ws://127.0.0.1:`) {
		t.Fatalf("endpoint: %s", body)
	}

	// Kill the browser behind our back; the watcher must notice and wipe the ephemeral profile.
	profile := s.Service().ProfileAbs(v.Session)
	w := &watcher.Watcher{Svc: s.Service, Hub: s.Hub, Fast: 500 * time.Millisecond, Slow: 500 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	time.Sleep(700 * time.Millisecond) // first tick records "running"
	p, _ := os.FindProcess(v.Runtime.PID)
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	for time.Since(t0) < 3*time.Second {
		_, body = call(t, s, "GET", base+"/api/sessions/peek", "", false)
		_, statErr := os.Stat(profile)
		if strings.Contains(body, `"state":"stopped"`) && os.IsNotExist(statErr) {
			t.Logf("stopped and wiped after %s", time.Since(t0).Round(100*time.Millisecond))
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("watcher did not stop+wipe within 3s: %s", body)
}
