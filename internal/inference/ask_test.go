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

func TestAskSendsGroundedPromptAndNotesPartialCoverage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer private-key" {
			t.Error("wrong chat request")
		}
		var payload struct {
			Model    string
			Messages []struct{ Role, Content string }
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Model != "chat-test" || len(payload.Messages) != 2 {
			t.Error("wrong payload")
			return
		}
		user := payload.Messages[1].Content
		if !strings.Contains(user, "Coverage: partial") || !strings.Contains(user, "Question: what changed?") || !strings.Contains(user, "sample transcript") {
			t.Error("question, coverage, or transcript missing")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Deploy moved to Friday."},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	settings := Settings{Enabled: true, BaseURL: server.URL + "/v1", APIKey: "private-key", ChatModel: "chat-test"}
	answer, err := Ask(context.Background(), settings, " what changed? ", "sample transcript", true)
	if err != nil || answer != "Deploy moved to Friday." {
		t.Fatalf("unexpected answer %q, %v", answer, err)
	}
}

func TestAskRejectsEmptyAndDisabled(t *testing.T) {
	if _, err := Ask(context.Background(), Settings{}, "q", "t", false); err == nil {
		t.Fatal("ran while disabled")
	}
	settings := Settings{Enabled: true, BaseURL: "https://api.example.com/v1", ChatModel: "m"}
	if _, err := Ask(context.Background(), settings, "   ", "t", false); err == nil {
		t.Fatal("accepted empty question")
	}
}
