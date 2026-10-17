package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/WaelRabah/slackline/internal/slack"
)

type loginBridge struct {
	state, host, startURL string
	result                chan slack.Credentials
	used                  atomic.Bool
}

func newLoginBridge(startURL string) (*loginBridge, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, errors.New("cannot initialize local login")
	}
	return &loginBridge{state: hex.EncodeToString(secret), startURL: startURL, result: make(chan slack.Credentials, 1)}, nil
}

func extensionOrigin(origin string) bool {
	const prefix = "chrome-extension://"
	if !strings.HasPrefix(origin, prefix) {
		return false
	}
	id := strings.TrimPrefix(origin, prefix)
	if len(id) != 32 {
		return false
	}
	for _, r := range id {
		if r < 'a' || r > 'p' {
			return false
		}
	}
	return true
}

var arcLoginPage = template.Must(template.New("login").Parse(`<!doctype html><html><head><meta charset="utf-8"><meta name="slackline-login" content="1"><title>Connect Slack to your terminal</title><style>body{background:#12171b;color:#e7ecef;font:17px system-ui;max-width:620px;margin:64px auto;padding:24px}button{background:#65d6ad;border:0;border-radius:8px;padding:14px 24px;font-size:17px}button:disabled{opacity:.4}code{color:#65d6ad}li{margin:12px 0}</style></head><body><h1>Connect Slack to your terminal</h1><p id="status">Install the local helper extension once, then reload this page.</p><ol><li>Open <code>arc://extensions</code> and enable Developer mode.</li><li>Choose Load unpacked and select the project's <code>browser-extension</code> folder.</li><li>Reload this page and select Connect Slack.</li></ol><p>The helper sends the Slack session you choose directly to this computer. Credentials never appear in a URL.</p><button id="connect" data-start-url="{{.}}" disabled>Connect Slack</button></body></html>`))

func (b *loginBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Host != b.host {
		http.Error(w, "invalid local host", http.StatusForbidden)
		return
	}
	if r.URL.Path == "/" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
		_ = arcLoginPage.Execute(w, b.startURL)
		return
	}
	if r.URL.Path != "/callback" {
		http.NotFound(w, r)
		return
	}
	origin := r.Header.Get("Origin")
	if !extensionOrigin(origin) {
		http.Error(w, "helper extension required", http.StatusForbidden)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "POST required", 405)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "JSON required", 415)
		return
	}
	var payload struct {
		State  string `json:"state"`
		Token  string `json:"token"`
		Cookie string `json:"cookie"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	if decoder.Decode(&payload) != nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid login payload", 400)
		return
	}
	if subtle.ConstantTimeCompare([]byte(payload.State), []byte(b.state)) != 1 {
		http.Error(w, "login expired or mismatched", 403)
		return
	}
	creds, err := slack.NewCredentials(payload.Token, payload.Cookie)
	if err != nil {
		http.Error(w, "invalid Slack session", 400)
		return
	}
	if !b.used.CompareAndSwap(false, true) {
		http.Error(w, "login already received", 409)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"ok":true}`)
	b.result <- creds
}

func arcExtensionLogin(ctx context.Context, startURL string) (slack.Credentials, error) {
	return extensionLogin(ctx, startURL, openLittleArc)
}

func openLittleArc(ctx context.Context, rawURL string) error {
	// Arc's application-level tab collection creates a Little Arc popup.
	const script = `on run argv
  tell application "Arc"
   make new tab with properties {URL:item 1 of argv}
   activate
  end tell
 end run`
	commandCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := exec.CommandContext(commandCtx, "/usr/bin/osascript", "-e", script, rawURL).Run(); err != nil {
		return errors.New("cannot open Little Arc; allow terminal automation of Arc, or select another browser with --browser")
	}
	return nil
}

func extensionLogin(ctx context.Context, startURL string, open func(context.Context, string) error) (slack.Credentials, error) {
	bridge, err := newLoginBridge(startURL)
	if err != nil {
		return slack.Credentials{}, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return slack.Credentials{}, errors.New("cannot start local login callback")
	}
	bridge.host = listener.Addr().String()
	server := &http.Server{Handler: bridge, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 15 * time.Second}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
		_ = listener.Close()
	}()
	failed := make(chan error, 1)
	go func() { failed <- server.Serve(listener) }()
	localURL := "http://" + bridge.host + "/#" + bridge.state
	fmt.Println("Opening Little Arc. Connect the helper extension on the local page to continue.")
	if err := open(ctx, localURL); err != nil {
		return slack.Credentials{}, err
	}
	select {
	case creds := <-bridge.result:
		return creds, nil
	case <-ctx.Done():
		return slack.Credentials{}, browserError(ctx, "local login cancelled")
	case <-failed:
		return slack.Credentials{}, errors.New("local login callback stopped")
	}
}
