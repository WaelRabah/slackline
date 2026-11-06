package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
)

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "backspace":
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
		default:
			msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		}
		next, cmd := m.Update(msg)
		m = next.(Model)
		for cmd != nil {
			out := cmd()
			if _, ok := out.(reactedMsg); !ok {
				break
			}
			next, cmd = m.Update(out)
			m = next.(Model)
		}
	}
	return m
}

func typeText(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m = press(t, m, string(r))
	}
	return m
}

func TestSearchEmojiRanksPrefixBeforeFuzzy(t *testing.T) {
	names := []string{"rocket", "tada", "thumbsup", "smile", "smiley", "sweat_smile", "slightly_smiling_face", "heart"}
	got := searchEmoji(names, "smi", 10)
	if len(got) < 3 || got[0] != "smile" || got[1] != "smiley" {
		t.Fatalf("prefix matches must come first, got %v", got)
	}
	if got := searchEmoji(names, "tda", 5); len(got) != 1 || got[0] != "tada" {
		t.Fatalf("fuzzy subsequence should find tada, got %v", got)
	}
	if got := searchEmoji(names, ":rock:", 5); len(got) != 1 || got[0] != "rocket" {
		t.Fatalf("surrounding colons are ignored, got %v", got)
	}
	if got := searchEmoji(names, "zzz", 5); len(got) != 0 {
		t.Fatalf("no match expected, got %v", got)
	}
	if got := searchEmoji(names, "face", 5); got[0] != "slightly_smiling_face" {
		t.Fatalf("word-suffix match expected, got %v", got)
	}
}

func TestQuickEmojisAllExist(t *testing.T) {
	names := standardEmojiNames()
	for _, name := range quickEmojis {
		if !slices.Contains(names, name) {
			t.Errorf("quick emoji %q missing from the catalogue", name)
		}
	}
}

func TestEmojiQueryAtOnlyFiresOnColonWords(t *testing.T) {
	for _, test := range []struct {
		text  string
		pos   int
		want  string
		start int
		ok    bool
	}{
		{"hi :smi", 7, "smi", 3, true},
		{":ro", 3, "ro", 0, true},
		{"(:fi", 4, "fi", 1, true},
		{"at 10:30", 8, "", 0, false},
		{"http://x", 8, "", 0, false},
		{":a", 2, "", 0, false}, // too short
		{"done :smile: ", 13, "", 0, false},
		{"a:smi", 5, "", 0, false},
		{"hi :smile and", 9, "smile", 3, true}, // cursor inside the word
	} {
		start, query, ok := emojiQueryAt([]rune(test.text), test.pos)
		if ok != test.ok || query != test.want || (ok && start != test.start) {
			t.Errorf("%q@%d: got (%d,%q,%v) want (%d,%q,%v)", test.text, test.pos, start, query, ok, test.start, test.want, test.ok)
		}
	}
}

func TestComposerDropdownInsertsEmoji(t *testing.T) {
	m := ready(t)
	m = press(t, m, "i")
	m = typeText(t, m, "ship it :rocke")
	if len(m.emojiSugg) == 0 || m.emojiSugg[0] != "rocket" {
		t.Fatalf("expected rocket suggestion, got %v", m.emojiSugg)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, ":rocket:") || !strings.Contains(view, "Tab/Enter insert") {
		t.Fatalf("dropdown or help not rendered:\n%s", view)
	}
	m = press(t, m, "enter")
	if got := m.input.Value(); got != "ship it :rocket: " {
		t.Fatalf("accept should replace the partial token, got %q", got)
	}
	if m.sending {
		t.Fatal("accepting a suggestion must not send")
	}
	if len(m.emojiSugg) != 0 {
		t.Fatal("dropdown should close after accept")
	}
	m = typeText(t, m, "ok")
	if m.input.Value() != "ship it :rocket: ok" {
		t.Fatalf("cursor should sit after the inserted emoji, got %q", m.input.Value())
	}
}

func TestComposerDropdownEscDismissesWithoutLeavingInsert(t *testing.T) {
	m := ready(t)
	m = press(t, m, "i")
	m = typeText(t, m, ":smi")
	if len(m.emojiSugg) == 0 {
		t.Fatal("expected suggestions")
	}
	m = press(t, m, "esc")
	if m.mode != insert || len(m.emojiSugg) != 0 {
		t.Fatalf("esc closes only the dropdown: mode=%v sugg=%v", m.mode, m.emojiSugg)
	}
	m = typeText(t, m, "l")
	if len(m.emojiSugg) == 0 {
		t.Fatal("typing more should reopen suggestions")
	}
	m = press(t, m, "down", "tab")
	if !strings.HasPrefix(m.input.Value(), ":smil") && !strings.HasSuffix(m.input.Value(), ": ") {
		t.Fatalf("down+tab should accept second suggestion, got %q", m.input.Value())
	}
}

func TestEnterStillSendsWhenNoDropdown(t *testing.T) {
	m := ready(t)
	before := len(m.history)
	m = press(t, m, "i")
	m = typeText(t, m, "plain")
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil || !m.sending {
		t.Fatal("Enter should send when no dropdown is open")
	}
	_ = before
}

func selectFirstMessage(t *testing.T, m Model) Model {
	t.Helper()
	m.focus = messages
	m.mode = normal
	m.selectedMessage = m.history[0].TS
	return m
}

func TestReactionPickerAddsAndTogglesReaction(t *testing.T) {
	m := ready(t)
	m = selectFirstMessage(t, m)
	ts := m.history[0].TS
	count := func(name string) int {
		for _, r := range m.history[0].Reactions {
			if r.Name == name {
				return r.Count
			}
		}
		return 0
	}
	m = press(t, m, "e")
	if m.mode != reactMode || m.picker == nil {
		t.Fatalf("e should open the picker, mode=%v", m.mode)
	}
	if len(m.picker.results) == 0 || m.picker.results[0] != "+1" {
		t.Fatalf("empty query should list common emoji first, got %v", m.picker.results)
	}
	m = typeText(t, m, "rocke")
	if m.picker.results[0] != "rocket" {
		t.Fatalf("typing should fuzzy filter, got %v", m.picker.results)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "ADD REACTION") || !strings.Contains(view, ":rocket:") {
		t.Fatalf("picker view missing:\n%s", view)
	}
	m = press(t, m, "enter")
	if m.mode != normal || count("rocket") != 1 {
		t.Fatalf("reaction should be added immediately, mode=%v reactions=%v", m.mode, m.history[0].Reactions)
	}
	if !m.reactedByMe(m.active, ts, "rocket") {
		t.Fatal("own reaction not recorded")
	}
	if m.recentEmoji[0] != "rocket" {
		t.Fatalf("recent list not updated: %v", m.recentEmoji)
	}
	// Same emoji again removes it.
	m = press(t, m, "e")
	if m.picker.results[0] != "rocket" {
		t.Fatalf("recent emoji should lead, got %v", m.picker.results)
	}
	m = press(t, m, "enter")
	if count("rocket") != 0 {
		t.Fatalf("second pick should remove the reaction, got %v", m.history[0].Reactions)
	}
}

func TestReactionPickerEscCancelsAndNeedsSelection(t *testing.T) {
	m := ready(t)
	m = selectFirstMessage(t, m)
	m = press(t, m, "e", "esc")
	if m.mode != normal || m.picker != nil {
		t.Fatal("esc should close the picker")
	}
	m.selectedMessage = ""
	m = press(t, m, "e")
	if m.mode != normal || !strings.Contains(m.status, "Select a message") {
		t.Fatalf("no selection should report, status=%q mode=%v", m.status, m.mode)
	}
}

func TestDemoReactionsRoundTrip(t *testing.T) {
	ctx := context.Background()
	api := slack.NewDemo()
	history, _ := api.History(ctx, "CGENERAL", "")
	ts := history.Messages[0].TS
	reactions := func() []slack.Reaction {
		h, _ := api.History(ctx, "CGENERAL", "")
		for _, message := range h.Messages {
			if message.TS == ts {
				return message.Reactions
			}
		}
		return nil
	}
	before := len(reactions())
	_ = api.AddReaction(ctx, "CGENERAL", ts, "rocket")
	_ = api.AddReaction(ctx, "CGENERAL", ts, "rocket") // idempotent
	if got := reactions(); len(got) != before+1 || got[len(got)-1].Count != 1 {
		t.Fatalf("add should create one reaction, got %v", got)
	}
	_ = api.RemoveReaction(ctx, "CGENERAL", ts, "rocket")
	if len(reactions()) != before {
		t.Fatalf("remove should drop an empty reaction, got %v", reactions())
	}
}
