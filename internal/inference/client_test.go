package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSummaryRequestAndCredentialSafeFailures(t *testing.T) {
	status := 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer private-key" {
			t.Error("wrong provider request")
		}
		var payload struct {
			Model    string
			Messages []struct{ Role, Content string }
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Model != "chat-test" || len(payload.Messages) != 2 || !strings.Contains(payload.Messages[1].Content, "sample transcript") {
			t.Error("wrong model or context")
		}
		w.WriteHeader(status)
		if status == 200 {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"What's happening: work. What's next: review."},"finish_reason":"stop"}]}`)
		} else {
			fmt.Fprint(w, "private-key secret transcript")
		}
	}))
	defer server.Close()
	settings := Settings{Enabled: true, BaseURL: server.URL + "/v1", APIKey: "private-key", ChatModel: "chat-test"}
	out, err := Summarize(context.Background(), settings, "sample transcript")
	if err != nil || !strings.Contains(out, "review") {
		t.Fatal("valid completion failed", err)
	}
	status = 401
	_, err = Summarize(context.Background(), settings, "sample transcript")
	if err == nil || strings.Contains(err.Error(), "private-key") || strings.Contains(err.Error(), "transcript") {
		t.Fatal("unsafe provider error")
	}
	if strings.Contains(fmt.Sprintf("%#v", settings), "private-key") {
		t.Fatal("settings formatting leaked key")
	}
}
func TestDisabledValidationAndNoCredentialRedirect(t *testing.T) {
	calls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer redirect.Close()
	settings := Settings{BaseURL: redirect.URL, ChatModel: "model", APIKey: "secret"}
	if _, err := Summarize(context.Background(), settings, "data"); err == nil {
		t.Fatal("disabled inference ran")
	}
	settings.Enabled = true
	if _, err := Summarize(context.Background(), settings, "data"); err == nil || calls != 0 {
		t.Fatal("followed credential-bearing redirect")
	}
	for _, base := range []string{"http://example.com/v1", "https://user:pass@example.com", "https://example.com?key=secret", "file:///tmp/model"} {
		settings.BaseURL = base
		if settings.Validate() == nil {
			t.Fatal("accepted unsafe URL")
		}
	}
}
