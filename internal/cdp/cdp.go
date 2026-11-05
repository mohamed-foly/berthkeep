// Package cdp is a minimal DevTools Protocol client.
package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Version is the /json/version response.
type Version struct {
	Browser              string `json:"Browser"`
	ProtocolVersion      string `json:"Protocol-Version"`
	UserAgent            string `json:"User-Agent"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// GUID returns the browser id at the end of the WebSocket URL.
func (v Version) GUID() string {
	i := strings.LastIndex(v.WebSocketDebuggerURL, "/")
	if i < 0 {
		return ""
	}
	return v.WebSocketDebuggerURL[i+1:]
}

// Target is one entry from /json/list.
type Target struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	Title                string `json:"title"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	DevtoolsFrontendURL  string `json:"devtoolsFrontendUrl"`
}

// httpClient avoids keep-alive: DevTools drops idle connections.
var httpClient = &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}

// Endpoint returns http://127.0.0.1:<port>.
func Endpoint(port int) string { return fmt.Sprintf("http://127.0.0.1:%d", port) }

func getJSON(ctx context.Context, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// GetVersion queries /json/version with a short timeout.
func GetVersion(ctx context.Context, port int) (Version, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var v Version
	err := getJSON(ctx, Endpoint(port)+"/json/version", &v)
	return v, err
}

// Pages returns the page targets, hiding browser UI and extension targets.
func Pages(ctx context.Context, port int) ([]Target, error) {
	var all []Target
	if err := getJSON(ctx, Endpoint(port)+"/json/list", &all); err != nil {
		return nil, err
	}
	out := all[:0]
	for _, t := range all {
		if t.Type == "page" && !strings.HasPrefix(t.URL, "devtools://") && !strings.HasPrefix(t.URL, "chrome-extension://") {
			out = append(out, t)
		}
	}
	return out, nil
}

// Conn is a CDP WebSocket connection (browser or page target).
type Conn struct {
	ws      *websocket.Conn
	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan response
	done    chan struct{}
	err     error
}

type response struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Dial connects to a CDP WebSocket URL.
func Dial(ctx context.Context, wsURL string) (*Conn, error) {
	ws, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{}})
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(64 << 20) // screenshots can be large
	c := &Conn{ws: ws, pending: map[int64]chan response{}, done: make(chan struct{})}
	go c.readLoop()
	return c, nil
}

func (c *Conn) readLoop() {
	defer close(c.done)
	for {
		_, data, err := c.ws.Read(context.Background())
		if err != nil {
			c.mu.Lock()
			c.err = err
			for id, ch := range c.pending {
				close(ch)
				delete(c.pending, id)
			}
			c.mu.Unlock()
			return
		}
		var r response
		if json.Unmarshal(data, &r) != nil || r.ID == 0 {
			continue // events are ignored by this minimal client
		}
		c.mu.Lock()
		ch := c.pending[r.ID]
		delete(c.pending, r.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- r
		}
	}
}

// ErrClosed is returned when the connection ends before a reply arrives.
var ErrClosed = errors.New("cdp connection closed")

// Call sends a command and waits for its result.
func (c *Conn) Call(ctx context.Context, method string, params any, result any) error {
	id := c.nextID.Add(1)
	ch := make(chan response, 1)
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return fmt.Errorf("%w: %v", ErrClosed, c.err)
	}
	c.pending[id] = ch
	c.mu.Unlock()
	if params == nil {
		params = struct{}{}
	}
	msg, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	if err := c.ws.Write(ctx, websocket.MessageText, msg); err != nil {
		return err
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return ErrClosed
		}
		if r.Error != nil {
			return fmt.Errorf("cdp %s: %s (%d)", method, r.Error.Message, r.Error.Code)
		}
		if result != nil && len(r.Result) > 0 {
			return json.Unmarshal(r.Result, result)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	}
}

// Close closes the connection.
func (c *Conn) Close() error { return c.ws.Close(websocket.StatusNormalClosure, "") }

// Done is closed when the connection ends.
func (c *Conn) Done() <-chan struct{} { return c.done }

// CloseBrowser asks the browser on port to shut down gracefully.
func CloseBrowser(ctx context.Context, port int) error {
	v, err := GetVersion(ctx, port)
	if err != nil {
		return err
	}
	c, err := Dial(ctx, v.WebSocketDebuggerURL)
	if err != nil {
		return err
	}
	defer c.Close()
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err = c.Call(callCtx, "Browser.close", nil, nil)
	if errors.Is(err, ErrClosed) {
		return nil // the browser closed the socket while shutting down
	}
	return err
}

// OpenTab creates a new page via Target.createTarget.
func OpenTab(ctx context.Context, port int, url string) (string, error) {
	v, err := GetVersion(ctx, port)
	if err != nil {
		return "", err
	}
	c, err := Dial(ctx, v.WebSocketDebuggerURL)
	if err != nil {
		return "", err
	}
	defer c.Close()
	var res struct {
		TargetID string `json:"targetId"`
	}
	err = c.Call(ctx, "Target.createTarget", map[string]any{"url": url}, &res)
	return res.TargetID, err
}

// browserCall runs one command on the browser target.
func browserCall(ctx context.Context, port int, method string, params, result any) error {
	v, err := GetVersion(ctx, port)
	if err != nil {
		return err
	}
	c, err := Dial(ctx, v.WebSocketDebuggerURL)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Call(ctx, method, params, result)
}

// CloseTab closes a page target.
func CloseTab(ctx context.Context, port int, targetID string) error {
	return browserCall(ctx, port, "Target.closeTarget", map[string]any{"targetId": targetID}, nil)
}

// ActivateTab brings a page target to the front.
func ActivateTab(ctx context.Context, port int, targetID string) error {
	return browserCall(ctx, port, "Target.activateTarget", map[string]any{"targetId": targetID}, nil)
}

// ErrNoTarget means the page target does not exist.
var ErrNoTarget = errors.New("target not found")

// Screenshot returns a JPEG of a page target.
func Screenshot(ctx context.Context, port int, targetID string, quality int) ([]byte, error) {
	pages, err := Pages(ctx, port)
	if err != nil {
		return nil, err
	}
	var ws string
	for _, p := range pages {
		if p.ID == targetID {
			ws = p.WebSocketDebuggerURL
		}
	}
	if ws == "" {
		return nil, ErrNoTarget
	}
	c, err := Dial(ctx, ws)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	var res struct {
		Data []byte `json:"data"` // base64 in JSON, decoded by encoding/json
	}
	err = c.Call(ctx, "Page.captureScreenshot", map[string]any{"format": "jpeg", "quality": quality}, &res)
	return res.Data, err
}
