package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/coder/websocket"
)

func TestArcDebugEndpointStrictlyLoopbackAndBounded(t *testing.T) {
	for _, data := range []string{"12345\n/devtools/browser/abc-123\n", "9222\n/devtools/browser", "1234\r\n/devtools/browser/TEST_uuid\r\n"} {
		endpoint, err := parseDebugEndpoint([]byte(data))
		if err != nil || !strings.HasPrefix(endpoint, "ws://127.0.0.1:") {
			t.Fatal("valid endpoint rejected")
		}
	}
	for _, data := range []string{"", "0\n/devtools/browser/a", "65536\n/devtools/browser/a", "-1\n/devtools/browser/a", "+9222\n/devtools/browser/a", "09222\n/devtools/browser/a", "9222\n//evil.test/socket", "9222\n/devtools/browser/a?secret=xoxc-fixture", "9222\n/devtools/browser/../a", "9222\n/devtools/browser/a\nextra", strings.Repeat("x", 4097)} {
		_, err := parseDebugEndpoint([]byte(data))
		if err == nil || strings.Contains(err.Error(), "xoxc-fixture") {
			t.Fatal("unsafe metadata accepted or echoed")
		}
	}
}
func TestArcEndpointDiscoveryAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "DevToolsActivePort")
	if _, err := readDebugEndpoint([]string{path}); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing metadata not detected")
	}
	os.WriteFile(path, []byte("9222\n/devtools/browser/test"), 0600)
	endpoint, err := waitDebugEndpoint(context.Background(), []string{path})
	if err != nil || endpoint != "ws://127.0.0.1:9222/devtools/browser/test" {
		t.Fatal("endpoint discovery failed")
	}
	link := path + "-link"
	if os.Symlink(path, link) == nil {
		if _, err := readDebugEndpoint([]string{link}); err == nil {
			t.Fatal("accepted a symlink")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := waitDebugEndpoint(ctx, []string{path + "missing"}); !errors.Is(err, context.Canceled) {
		t.Fatal("discovery ignored cancellation")
	}
}
func TestArcAttachDisconnectsWithoutClosingBrowserOrTabs(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	disconnected := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "slackline" {
			t.Error("approval request missing client identity")
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		defer close(disconnected)
		for {
			_, data, err := ws.Read(r.Context())
			if err != nil {
				return
			}
			var req struct {
				ID     int    `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(data, &req) != nil {
				return
			}
			mu.Lock()
			methods = append(methods, req.Method)
			mu.Unlock()
			response := fmt.Sprintf(`{"id":%d,"result":{"targetInfos":[]}}`, req.ID)
			if ws.Write(r.Context(), websocket.MessageText, []byte(response)) != nil {
				return
			}
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	browser, detach := attachBrowser(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/devtools/browser/test")
	if _, err := chromedp.Targets(browser); err != nil {
		detach()
		t.Fatal("fixture attach failed")
	}
	detach()
	if ctx.Err() != nil {
		t.Fatal("detaching cancelled the parent login context")
	}
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("connection stayed open")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 1 || methods[0] != "Target.getTargets" {
		t.Fatal("attach sent commands that create or close tabs/browser")
	}
}
func TestArcPendingApprovalCancels(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-release }))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	browser, detach := attachBrowser(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/devtools/browser/test")
	done := make(chan struct{})
	go func() { chromedp.Targets(browser); detach(); close(done) }()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("approval handshake did not cancel")
	}
}

func TestArcErrorsDoNotConfuseRejectionWithNetworkFailureOrLeakDetails(t *testing.T) {
	ctx := context.Background()
	rejected := arcConnectionError(ctx, errors.New(`could not dial "ws://127.0.0.1:1234/devtools/browser/private-endpoint": websocket handshake: unexpected status "403 Forbidden", body "Connection rejected xoxc-secret"`))
	if !strings.Contains(rejected.Error(), "HTTP 403") || strings.Contains(rejected.Error(), "private-endpoint") || strings.Contains(rejected.Error(), "xoxc-secret") {
		t.Fatal("rejection diagnostics unsafe or missing status")
	}
	if !arcChromeFallback(rejected, "") || arcChromeFallback(rejected, "/Applications/Arc.app/Contents/MacOS/Arc") {
		t.Fatal("explicit browser choice not honored")
	}
	network := arcConnectionError(ctx, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("unreachable ws://secret-endpoint")})
	if !strings.Contains(network.Error(), "no longer reachable") || strings.Contains(network.Error(), "secret-endpoint") || arcChromeFallback(network, "") {
		t.Fatal("network error confused with browser rejection")
	}
	if arcChromeFallback(nil, "") {
		t.Fatal("success triggered fallback")
	}
	cancelled, stop := context.WithCancel(ctx)
	stop()
	err := arcConnectionError(cancelled, errors.New(`unexpected status "403 Forbidden"`))
	if !strings.Contains(err.Error(), "cancelled") || arcChromeFallback(err, "") {
		t.Fatal("cancellation triggered fallback")
	}
}
func TestIsolatedFallbackSelectionNeverReturnsArc(t *testing.T) {
	chrome := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	selected, err := findIsolatedFallback("/fixture", func(path string) (string, error) {
		if path == chrome {
			return path, nil
		}
		return "", os.ErrNotExist
	})
	if err != nil || selected != chrome || isolatedBrowserName(selected) != "Chrome" {
		t.Fatal("failed to select installed isolated browser")
	}
	_, err = findIsolatedFallback("/fixture", func(path string) (string, error) {
		if strings.Contains(path, "Arc.app") {
			t.Fatal("attempted to launch another Arc instance")
		}
		return "", os.ErrNotExist
	})
	if err == nil {
		t.Fatal("missing browser did not fail")
	}
}
func TestArcReal403UsesSafeDiagnostic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, "Connection rejected xoxc-fixture-secret")
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	browser, detach := attachBrowser(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/devtools/browser/private")
	defer detach()
	_, err := chromedp.Targets(browser)
	if err == nil {
		t.Fatal("fixture unexpectedly connected")
	}
	safe := arcConnectionError(ctx, err)
	if !arcChromeFallback(safe, "") || strings.Contains(safe.Error(), "xoxc-fixture-secret") || strings.Contains(safe.Error(), server.URL) {
		t.Fatal("real rejected handshake not safely classified")
	}
}
