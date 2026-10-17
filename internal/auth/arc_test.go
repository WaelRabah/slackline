package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const testExtensionOrigin = "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestLocalCallbackRejectsUntrustedAndReplayedCredentials(t *testing.T) {
	b, _ := newLoginBridge("https://slack.com/signin")
	b.host = "127.0.0.1:12345"
	body := `{"state":"` + b.state + `","token":"xoxc-fixture","cookie":"xoxd-fixture%2F"}`
	for _, test := range []struct {
		origin, host, body string
		code               int
	}{
		{"https://attacker.test", b.host, body, 403},
		{testExtensionOrigin, "attacker.test", body, 403},
		{testExtensionOrigin, b.host, `{"state":"wrong","token":"xoxc-fixture","cookie":"xoxd-fixture"}`, 403},
		{testExtensionOrigin, b.host, body, 200},
		{testExtensionOrigin, b.host, body, 409},
	} {
		req := httptest.NewRequest("POST", "http://"+test.host+"/callback", strings.NewReader(test.body))
		req.Host = test.host
		req.Header.Set("Origin", test.origin)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		b.ServeHTTP(w, req)
		if w.Code != test.code {
			t.Fatalf("callback status %d, want %d", w.Code, test.code)
		}
		if strings.Contains(w.Body.String(), "xox") {
			t.Fatal("callback echoed credentials")
		}
	}
	page := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://"+b.host+"/", nil)
	req.Host = b.host
	b.ServeHTTP(page, req)
	if strings.Contains(page.Body.String(), b.state) {
		t.Fatal("one-time nonce exposed in public page HTML")
	}
}
func TestExtensionLoginReceivesAndClosesCallback(t *testing.T) {
	var endpoint string
	creds, err := extensionLogin(context.Background(), "https://slack.com/signin", func(ctx context.Context, raw string) error {
		u, _ := url.Parse(raw)
		endpoint = u.Scheme + "://" + u.Host + "/callback"
		body, _ := json.Marshal(map[string]string{"state": u.Fragment, "token": "xoxc-fixture", "cookie": "xoxd-fixture%2F"})
		req, _ := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(string(body)))
		req.Header.Set("Origin", testExtensionOrigin)
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, response.Body)
		if response.StatusCode != 200 {
			t.Fatal("callback failed")
		}
		return nil
	})
	if err != nil || creds.Cookie() != "xoxd-fixture%2F" {
		t.Fatalf("extension login failed: %v", err)
	}
	response, err := http.Get(endpoint)
	if err == nil {
		response.Body.Close()
		t.Fatal("callback stayed open after login")
	}
}
