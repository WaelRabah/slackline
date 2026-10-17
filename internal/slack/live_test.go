package slack

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestLiveTransportCarriesCookieReceivesEventsAndCancels(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rtm.connect" {
			_ = r.ParseForm()
			if r.Form.Get("token") != "xoxc-test-session" {
				t.Error("missing live session token")
			}
			fmt.Fprintf(w, `{"ok":true,"url":%q}`, "ws"+strings.TrimPrefix(server.URL, "http")+"/socket?ticket=private-fixture")
			return
		}
		cookie, err := r.Cookie("d")
		if err != nil || cookie.Value != "xoxd-test%2Fencoded%3D" {
			t.Error("live socket cookie missing")
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for _, event := range []string{`{"type":"hello"}`, `{"type":"user_typing","channel":"C1","user":"U1"}`, `{"type":"message","channel":"C1","user":"U1","ts":"200.000001","text":"hello"}`} {
			if conn.Write(r.Context(), websocket.MessageText, []byte(event)) != nil {
				return
			}
		}
		_, _, _ = conn.Read(r.Context())
	}))
	defer server.Close()
	client := NewClient(testCredentials(t))
	client.baseURL = server.URL + "/"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	events := []LiveEvent{}
	err := client.Listen(ctx, func(event LiveEvent) {
		events = append(events, event)
		if event.Type == "message" {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || len(events) != 3 || events[0].State != "live" || events[1].Type != "user_typing" || events[2].Message.Text != "hello" {
		t.Fatalf("live events/cleanup failed: events=%d, err=%v", len(events), err)
	}
}
func TestLiveDesktopFallbackAndEndpointValidation(t *testing.T) {
	calls := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/rtm.connect" {
			fmt.Fprint(w, `{"ok":false,"error":"enterprise_is_restricted"}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"primary_websocket_url":"wss://wss-primary.slack.com/websocket/private-ticket"}`)
	})
	raw, err := client.socketURL(context.Background())
	if err != nil || calls != 2 || !strings.HasPrefix(raw, "wss://wss-primary.slack.com/") {
		t.Fatal("desktop socket fallback failed")
	}
	for _, raw := range []string{"wss://evil.example/socket", "wss://slack.com.evil.example/socket", "wss://user:secret@wss-primary.slack.com/socket", "ws://wss-primary.slack.com/socket", "wss://wss-primary.slack.com:444/socket", "file:///tmp/socket"} {
		if client.validSocketURL(raw) {
			t.Fatal("accepted untrusted socket endpoint")
		}
	}
}
func TestLiveSocketFailureDoesNotExposeSignedURL(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true,"url":"wss://not-slack.example/socket?ticket=private-ticket"}`)
	})
	err := client.Listen(context.Background(), func(LiveEvent) {})
	if err == nil || strings.Contains(err.Error(), "private-ticket") || strings.Contains(err.Error(), "xox") {
		t.Fatal("live failure exposed sensitive details")
	}
}
func TestParseLiveEditAndTypingScope(t *testing.T) {
	event, err := ParseLiveEvent([]byte(`{"type":"message","subtype":"message_changed","channel":"C1","message":{"ts":"200.1","text":"edited","user":"U1"}}`))
	if err != nil || event.Message.Subtype != "message_changed" || event.Message.Channel != "C1" || event.Changed == nil || event.Changed.Text != "edited" {
		t.Fatal("edit envelope lost")
	}
	event, err = ParseLiveEvent([]byte(`{"type":"user_typing","channel":"C1","user":"U1","thread_ts":"100.1"}`))
	if err != nil || event.Message.ThreadTS != "100.1" || event.Message.User != "U1" {
		t.Fatal("typing scope lost")
	}
}
