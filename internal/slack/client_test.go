package slack

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testCredentials(t *testing.T) Credentials {
	t.Helper()
	c, err := NewCredentials("xoxc-test-session", "xoxd-test%2Fencoded%3D")
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := NewClient(testCredentials(t))
	c.baseURL = server.URL + "/"
	return c
}

func TestCredentials(t *testing.T) {
	for _, cookie := range []string{"xoxd-encoded%2Fcookie%3D", "d=xoxd-encoded%2Fcookie%3D"} {
		c, err := NewCredentials("  xoxc-test  ", cookie)
		if err != nil {
			t.Fatal(err)
		}
		if c.Cookie() != "xoxd-encoded%2Fcookie%3D" {
			t.Fatal("cookie was altered")
		}
		if strings.Contains(fmt.Sprintf("%v %+v %#v", c, c, c), "xox") {
			t.Fatal("credentials leaked through formatting")
		}
	}
	for _, pair := range [][2]string{{"xoxp-test", "cookie"}, {"xoxc-test", ""}, {"xoxc-test", "d=test; other=secret"}, {"xoxc-test", "bad\r\ncookie"}, {"xoxc-bad\ntoken", "cookie"}, {"xoxc-test", "bad\"cookie"}} {
		if _, err := NewCredentials(pair[0], pair[1]); err == nil {
			t.Fatal("accepted invalid credentials")
		}
	}
}

func TestSessionAuthentication(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.RawQuery != "" {
			t.Error("expected POST without query credentials")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.PostForm.Get("token") != "xoxc-test-session" {
			t.Error("missing session token in form")
		}
		cookie, err := r.Cookie("d")
		if err != nil || cookie.Value != "xoxd-test%2Fencoded%3D" {
			t.Error("cookie missing or re-encoded")
		}
		fmt.Fprint(w, `{"ok":true,"team":"Example","user_id":"U1"}`)
	})
	session, err := c.AuthTest(context.Background())
	if err != nil || session.UserID != "U1" {
		t.Fatalf("unexpected auth response: %v", err)
	}
}

func TestRedirectDoesNotLeakCredentials(t *testing.T) {
	var received atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1) }))
	defer destination.Close()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	})
	if _, err := c.AuthTest(context.Background()); err == nil {
		t.Fatal("expected redirect rejection")
	}
	if received.Load() != 0 {
		t.Fatal("followed credential-bearing redirect")
	}
}

func TestPagination(t *testing.T) {
	for _, method := range []string{"users.conversations", "users.list"} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				r.ParseForm()
				if calls > 2 {
					t.Error("too many requests")
					fmt.Fprint(w, `{"ok":false,"error":"bad_cursor"}`)
					return
				}
				if calls == 2 && r.Form.Get("cursor") != "next page +/=" {
					t.Error("cursor not preserved")
				}
				cursor := "next page +/="
				if calls == 2 {
					cursor = ""
				}
				field := "channels"
				if method == "users.list" {
					field = "members"
				}
				fmt.Fprintf(w, `{"ok":true,"%s":[{"id":"%d"}],"response_metadata":{"next_cursor":%q}}`, field, calls, cursor)
			})
			if method == "users.list" {
				result, err := c.Users(context.Background())
				if err != nil || len(result) != 2 {
					t.Fatalf("pagination: %v", err)
				}
			} else {
				result, err := c.Conversations(context.Background())
				if err != nil || len(result) != 2 {
					t.Fatalf("pagination: %v", err)
				}
			}
			if calls != 2 {
				t.Fatal("did not traverse pages")
			}
		})
	}
}

func TestRepeatedCursorStops(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"ok":true,"response_metadata":{"next_cursor":"same"}}`)
	})
	_, err := c.Conversations(context.Background())
	if err == nil || calls != 2 {
		t.Fatal("repeated cursor was not stopped")
	}
}

func TestRateLimitAndNoAutomaticWriteRetry(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := c.Send(context.Background(), "C1", "hello")
	var rate *RateLimitError
	if !errors.As(err, &rate) || rate.RetryAfter != 42*time.Second {
		t.Fatalf("wrong rate error: %v", err)
	}
	if calls != 1 {
		t.Fatal("retried write automatically")
	}
}

func TestErrorsDoNotExposeResponseBody(t *testing.T) {
	for _, response := range []string{`{"ok":false,"error":"xoxc-test-session"}`, `{"ok":false,"error":"<html>secret</html>"}`, "not JSON xoxd-test%2Fencoded%3D"} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, response) })
		_, err := c.AuthTest(context.Background())
		if err == nil || strings.Contains(err.Error(), "xox") || strings.Contains(err.Error(), "secret") {
			t.Fatal("unsafe API error")
		}
	}
}

func TestSendAndHistory(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.URL.Path {
		case "/chat.postMessage":
			if r.Form.Get("channel") != "C1" || r.Form.Get("text") != "hello + & café" {
				t.Error("message form corrupted")
			}
			fmt.Fprint(w, `{"ok":true,"ts":"123.000001","message":{"user":"U1","text":"hello + & café"}}`)
		case "/conversations.history":
			if r.Form.Get("cursor") != "older +/=" {
				t.Error("history cursor missing")
			}
			fmt.Fprint(w, `{"ok":true,"messages":[{"ts":"123.000001","text":"hi"}],"response_metadata":{"next_cursor":"more"}}`)
		}
	})
	message, err := c.Send(context.Background(), "C1", "hello + & café")
	if err != nil || message.TS != "123.000001" {
		t.Fatalf("send failed: %v", err)
	}
	history, err := c.History(context.Background(), "C1", "older +/=")
	if err != nil || len(history.Messages) != 1 || history.ResponseMetadata.NextCursor != "more" {
		t.Fatalf("history failed: %v", err)
	}
}

func TestRepliesAndThreadPosting(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.URL.Path == "/conversations.replies" {
			if r.Form.Get("ts") != "100.000001" || r.Form.Get("cursor") != "next" {
				t.Error("thread pagination fields missing")
			}
			fmt.Fprint(w, `{"ok":true,"messages":[{"ts":"100.000001","text":"parent"},{"ts":"101.000001","thread_ts":"100.000001","text":"reply"}]}`)
		} else {
			if r.Form.Get("thread_ts") != "100.000001" {
				t.Error("reply would be posted outside the thread")
			}
			fmt.Fprint(w, `{"ok":true,"ts":"102.000001","message":{"text":"new reply"}}`)
		}
	})
	h, err := c.Replies(context.Background(), "C1", "100.000001", "next")
	if err != nil || len(h.Messages) != 2 {
		t.Fatal("thread history failed")
	}
	msg, err := c.SendReply(context.Background(), "C1", "100.000001", "new reply")
	if err != nil || msg.ThreadTS != "100.000001" {
		t.Fatal("thread posting failed")
	}
}
