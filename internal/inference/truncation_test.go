package inference

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCutOffAnswerKeepsTextAndAddsNote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"## What you missed\n- partial bullet"},"finish_reason":"length"}]}`)
	}))
	defer server.Close()
	settings := Settings{Enabled: true, BaseURL: server.URL + "/v1", ChatModel: "m"}
	got, err := CatchUp(context.Background(), settings, "transcript", false)
	if err != nil {
		t.Fatalf("cut-off answer failed instead of returning partial text: %v", err)
	}
	if !strings.Contains(got, "partial bullet") || !strings.Contains(got, "cut off") {
		t.Fatalf("partial answer or note missing: %q", got)
	}
}
