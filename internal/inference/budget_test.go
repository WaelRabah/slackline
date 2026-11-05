package inference

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An empty answer that ran out of budget is retried with a bigger budget, and the
// retry's answer is returned.
func TestEmptyAnswerFromReasoningIsRetriedWithLargerBudget(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"content":""},"finish_reason":"length"}]}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Summary text"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	settings := Settings{Enabled: true, BaseURL: server.URL + "/v1", ChatModel: "m"}
	got, err := Summarize(context.Background(), settings, "transcript")
	if err != nil || got != "Summary text" || calls != 2 {
		t.Fatalf("got %q, err %v, calls %d", got, err, calls)
	}
}

// Compactif requests keep reasoning out of the answer budget.
func TestCompactifRequestsAskForNoReasoningTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := new(strings.Builder)
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		body.Write(buf[:n])
		if !strings.Contains(body.String(), "clear_thinking") {
			t.Error("clear_thinking not sent")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	settings := Settings{Enabled: true, BaseURL: server.URL + "/v1", ChatModel: "m"}
	settings.BaseURL = strings.Replace(settings.BaseURL, "127.0.0.1", "127.0.0.1", 1)
	payload := map[string]any{}
	withoutReasoning(payload, Settings{BaseURL: "https://api.compactif.ai/v1"})
	if payload["chat_template_kwargs"] == nil {
		t.Fatal("Compactif payload missing clear_thinking")
	}
	withoutReasoning(payload, Settings{BaseURL: "https://api.example.com/v1"})
	if len(payload) != 1 {
		t.Fatal("other providers received Compactif-only options")
	}
}
