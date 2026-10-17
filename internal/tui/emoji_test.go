package tui

import (
	"context"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
)

func TestEmojiTextPreservesCodeLinksAndUnknownNames(t *testing.T) {
	m := ready(t)
	m.emojis = map[string]string{"shipit": "alias:rocket", "again": "alias:shipit", "parrot": "https://emoji.slack-edge.com/custom.gif", "a": "alias:b", "b": "alias:a"}
	for _, test := range [][2]string{
		{"Hi :wave: :+1::skin-tone-3: :flag-us: :again:", "Hi 👋 👍🏼 🇺🇸 🚀"},
		{":female-technologist::skin-tone-4:", "👩🏽‍💻"},
		{"`:smile:` ```\n:rocket:\n``` :tada:", "`:smile:` ```\n:rocket:\n``` 🎉"},
		{"`:smile: :wave:", "`:smile: :wave:"},
		{"<https://example.test/:smile:|:wave:> https://example.test/:rocket:", "<https://example.test/:smile:|:wave:> https://example.test/:rocket:"},
		{":unknown: :parrot: :a: already 🙂", ":unknown: :parrot: :a: already 🙂"},
	} {
		if got := m.messageText(test[0]); got != test[1] {
			t.Errorf("%q: got %q, want %q", test[0], got, test[1])
		}
	}
}

func TestReactionBadgesFitPanesAndUseServerCount(t *testing.T) {
	m := ready(t)
	message := slack.Message{TS: "200.000001", Text: ":wave:", Reactions: []slack.Reaction{{Name: "+1::skin-tone-3", Count: 14, Users: []string{m.session.UserID}}, {Name: "unknown_custom", Count: 2}, {Name: "zero", Count: 0}}}
	for _, width := range []int{10, 30, 80} {
		block := m.messageBlock(message, width)
		for _, line := range strings.Split(block, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("emoji escaped %d-column pane", width)
			}
		}
		if width == 80 {
			plain := ansi.Strip(block)
			for _, want := range []string{"👋", "✓", "👍🏼 14", ":unknown_custom: 2"} {
				if !strings.Contains(plain, want) {
					t.Errorf("missing badge %q", want)
				}
			}
		}
	}
}

type reactionFixture struct {
	*slack.Demo
	count int
}

func (f *reactionFixture) Reactions(context.Context, string, string) ([]slack.Reaction, error) {
	return []slack.Reaction{{Name: "wave", Count: f.count, Users: []string{"KNOWN"}}}, nil
}
func TestAuthoritativeReactionsNeverGuessFromPartialUsers(t *testing.T) {
	m := ready(t)
	api := &reactionFixture{slack.NewDemo(), 6}
	m.api = api
	message := m.history[0]
	message.Reactions = []slack.Reaction{{Name: "wave", Count: 6, Users: []string{"KNOWN"}}}
	m.history[0] = message
	before := m.eventVersion
	change := slack.ReactionChange{Name: "wave", User: "OMITTED", Channel: m.active, TS: message.TS, EventTS: "201.1"}
	next, cmd := m.Update(slack.LiveEvent{Type: "reaction_added", Reaction: &change})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("missing authoritative refresh")
	}
	next, duplicate := m.Update(slack.LiveEvent{Type: "reaction_added", Reaction: &change})
	m = next.(Model)
	if duplicate != nil {
		t.Fatal("duplicate reaction triggered another request")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.history[0].Reactions[0].Count != 6 {
		t.Fatal("already reflected addition counted twice")
	}
	next, _ = m.Update(historyMsg{channel: m.active, generation: m.generation, version: before, history: slack.History{Messages: []slack.Message{{TS: message.TS, Text: "history text", Reactions: []slack.Reaction{{Name: "wave", Count: 5}}}}}})
	m = next.(Model)
	if m.history[0].Reactions[0].Count != 6 || m.history[0].Text != "history text" {
		t.Fatal("reaction protection lost counts or message fields")
	}
	// A refresh before the first history response must not discard that message.
	m.history = nil
	change.EventTS = "202.1"
	change.TS = "222.1"
	api.count = 5
	next, cmd = m.Update(slack.LiveEvent{Type: "reaction_removed", Reaction: &change})
	m = next.(Model)
	next, _ = m.Update(cmd())
	m = next.(Model)
	next, _ = m.Update(historyMsg{channel: m.active, generation: m.generation, version: before, history: slack.History{Messages: []slack.Message{{TS: "222.1", Text: "first load"}}}})
	m = next.(Model)
	if len(m.history) != 1 || m.history[0].Reactions[0].Count != 5 {
		t.Fatal("reaction arriving before history hid the message")
	}
}
