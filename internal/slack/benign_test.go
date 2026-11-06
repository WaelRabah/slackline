package slack

import (
	"errors"
	"testing"
)

func TestMarkingDeletedThreadIsBenign(t *testing.T) {
	missing := errors.New("subscriptions.thread.mark: Slack subscriptions.thread.mark: message_not_found")
	if !benignError("subscriptions.thread.mark", missing) {
		t.Fatal("message_not_found on thread mark should be benign")
	}
	if benignError("conversations.history", missing) {
		t.Fatal("other methods must keep reporting message_not_found")
	}
	if benignError("subscriptions.thread.mark", errors.New("rate limited")) {
		t.Fatal("rate limits are not benign")
	}
}
