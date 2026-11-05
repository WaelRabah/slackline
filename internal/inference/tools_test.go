package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssistRunsToolThenAnswers(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer private-key" {
			t.Error("wrong chat request")
		}
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Tools    []map[string]any `json:"tools"`
			Messages []map[string]any `json:"messages"`
		}
		if json.Unmarshal(body, &payload) != nil || len(payload.Tools) != 1 {
			t.Error("tools were not sent")
		}
		requests++
		if requests == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_conversations","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		// The second request must carry the tool result back with its call id.
		last := payload.Messages[len(payload.Messages)-1]
		if last["role"] != "tool" || last["tool_call_id"] != "call_1" || !strings.Contains(fmt.Sprint(last["content"]), "general") {
			t.Error("tool result not returned to the model")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Nothing urgent in general."},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	settings := Settings{Enabled: true, BaseURL: server.URL + "/v1", APIKey: "private-key", ChatModel: "chat-test"}
	tools := []Tool{{Name: "list_conversations", Description: "list", Parameters: map[string]any{"type": "object"}}}
	var called []string
	answer, err := Assist(context.Background(), settings, AssistantSystem, "what is new?", tools, func(_ context.Context, name, args string) (string, error) {
		called = append(called, name)
		return "general (C1, channel)", nil
	})
	if err != nil || answer != "Nothing urgent in general." {
		t.Fatalf("unexpected answer %q, %v", answer, err)
	}
	if len(called) != 1 || called[0] != "list_conversations" {
		t.Fatalf("handler calls: %v", called)
	}
}

func TestAssistStopsAfterRoundLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"","tool_calls":[{"id":"c","type":"function","function":{"name":"get_messages","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer server.Close()
	settings := Settings{Enabled: true, BaseURL: server.URL + "/v1", ChatModel: "m"}
	_, err := Assist(context.Background(), settings, AssistantSystem, "q", nil, func(context.Context, string, string) (string, error) { return "ok", nil })
	if err == nil || !strings.Contains(err.Error(), "too many lookups") {
		t.Fatalf("expected round limit error, got %v", err)
	}
}

func TestAssistRejectsDisabledAndEmptyQuestions(t *testing.T) {
	if _, err := Assist(context.Background(), Settings{}, AssistantSystem, "q", nil, nil); err == nil {
		t.Fatal("ran while disabled")
	}
	settings := Settings{Enabled: true, BaseURL: "https://api.example.com/v1", ChatModel: "m"}
	if _, err := Assist(context.Background(), settings, AssistantSystem, "  ", nil, nil); err == nil {
		t.Fatal("accepted empty question")
	}
}
