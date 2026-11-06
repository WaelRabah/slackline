package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
)

func TestMarkdownStylesEmphasisCodeAndQuotes(t *testing.T) {
	m := ready(t)
	got := m.messageText("*bold* _italic_ ~strike~ `code *kept* :wave:`\n> quoted :rocket:\nplain :tada:")
	if plain := ansi.Strip(got); plain != "bold italic strike code *kept* :wave:\n▎ quoted 🚀\nplain 🎉" {
		t.Fatalf("unexpected rendering: %q", plain)
	}
	for name, sequence := range map[string]string{"bold": "\x1b[1m", "italic": "\x1b[3m", "strike": "\x1b[9m"} {
		if !strings.Contains(got, sequence) {
			t.Errorf("%s style missing from %q", name, got)
		}
	}
	if strings.Contains(ansi.Strip(got), "🚀") == false || strings.Contains(ansi.Strip(got), ":rocket:") {
		t.Error("emoji outside code were not substituted")
	}
	if strings.Contains(got, "\x1b[1mcode") || strings.Contains(got, "\x1b[1m*kept*") {
		t.Error("emphasis leaked into inline code")
	}
	if strings.Count(ansi.Strip(got), ":wave:") != 1 || strings.Count(ansi.Strip(got), "*kept*") != 1 {
		t.Error("inline code contents changed")
	}
}

func TestMarkdownCodeBlocksKeepLiteralText(t *testing.T) {
	m := ready(t)
	got := m.messageText("before\n```go\nfmt.Println(\":rocket: <@U1>\")\n```\nafter :rocket:")
	plain := ansi.Strip(got)
	if !strings.Contains(plain, "fmt.Println(\":rocket: <@U1>\")") {
		t.Fatalf("code block contents changed: %q", plain)
	}
	if strings.HasSuffix(plain, "after 🚀") == false {
		t.Fatalf("text after a closed fence was swallowed: %q", plain)
	}
	if strings.Count(plain, "\n") != 2 {
		t.Fatalf("fence containers added or removed lines: %q", plain)
	}
	unfinished := ansi.Strip(m.messageText("```\nstill :rocket:"))
	if strings.Contains(unfinished, "🚀") {
		t.Fatalf("unfinished fence stopped protecting its contents: %q", unfinished)
	}
	for _, input := range []string{"text ```go\n:rocket:", "text ```go```junk\n:rocket:"} {
		invalid := ansi.Strip(m.messageText(input))
		if !strings.HasSuffix(invalid, "\n🚀") || strings.Contains(invalid, "```\n") {
			t.Fatalf("a fence inside a line swallowed following text: %q", invalid)
		}
	}
}

func TestMarkdownRendersMentionsChannelsAndGroups(t *testing.T) {
	m := ready(t)
	m.users["U1"] = "alice"
	m.conversations = append(m.conversations, slack.Conversation{ID: "C1", Name: "general"})
	plain := ansi.Strip(m.messageText("<@U1> <@U1|alice> <#C1> <!here> <!subteam^S1|@devs> <@U9>"))
	if plain != "@alice @alice #general @here @devs @U9" {
		t.Fatalf("unexpected mention rendering: %q", plain)
	}
	if !strings.Contains(plain, "@U9") {
		t.Error("unknown mention lost its identifier")
	}
}

func TestMarkdownLinksUseOSC8AndSurviveWrapping(t *testing.T) {
	m := ready(t)
	target := "https://example.test/a/very/long/path/that/wraps/:smile:"
	rendered := m.messageText("<" + target + "|Docs>")
	if !strings.Contains(rendered, "\x1b]8;;"+target+"\x1b\\") {
		t.Fatalf("link target missing OSC 8 sequence: %q", rendered)
	}
	if !strings.Contains(ansi.Strip(rendered), "Docs") {
		t.Fatal("link label missing")
	}
	bare := m.messageText(target + ".")
	if !strings.Contains(bare, "\x1b]8;;"+target+"\x1b\\") {
		t.Fatalf("bare URL did not become clickable: %q", bare)
	}
	if plain := ansi.Strip(bare); plain != target+"." {
		t.Fatalf("bare URL text changed: %q", plain)
	}
	wrapped := ansi.Hardwrap(rendered, 16, true)
	if strings.Count(wrapped, "\x1b]8;;"+target+"\x1b\\") != 1 {
		t.Fatalf("wrapping split or dropped the hyperlink: %q", wrapped)
	}
	for _, line := range strings.Split(wrapped, "\n") {
		if ansi.StringWidth(line) > 16 {
			t.Fatalf("hyperlinked line escaped its width: %q", line)
		}
	}
	for _, input := range []string{
		"<javascript:alert(1)|click>",
		"<data:text/html,hello|data>",
		"<//example.test|relative>",
		"<mailto:hi@example.test|Mail>",
	} {
		output := m.messageText(input)
		switch input[1] {
		case 'm':
			if !strings.Contains(output, "\x1b]8;;mailto:hi@example.test\x1b\\") {
				t.Errorf("mailto link was not clickable: %q", output)
			}
		default:
			if strings.Contains(output, "\x1b]8;;") {
				t.Errorf("unsafe target produced a hyperlink: %q", output)
			}
		}
	}
}

func TestSafeLinkRejectsTerminalEscapesAndSchemes(t *testing.T) {
	for _, raw := range []string{
		"",
		"javascript:alert(1)",
		"data:text/html,hello",
		"file:///tmp/secret",
		"https://",
		"https://example.test/\x1b]8;;https://evil.test",
		"https://example.test/\nnext",
		"https://example.test/\u009cevil",
		"https://example.test/a b",
		strings.Repeat("https://example.test/", 300),
	} {
		if safe, ok := safeLink(raw); ok {
			t.Errorf("safeLink accepted %q as %q", raw, safe)
		}
	}
	for _, raw := range []string{"https://example.test/a?b=1", "http://localhost:8080", "mailto:hi@example.test"} {
		if safe, ok := safeLink(raw); !ok || safe != raw {
			t.Errorf("safeLink rejected %q", raw)
		}
	}
}

func TestHyperlinksStayInsideMessageFrames(t *testing.T) {
	m := ready(t)
	target := "https://example.test/" + strings.Repeat("segment/", 12)
	for _, width := range []int{12, 28, 60, 90} {
		block := m.messageBlock(slack.Message{TS: "1792188000.000001", User: "U1", Text: "<" + target + "|a long link label>"}, width)
		for _, line := range strings.Split(block, "\n") {
			if lipgloss.Width(line) != width {
				t.Fatalf("width %d escaped by line %q", width, line)
			}
		}
		if strings.Count(block, "\x1b]8;;"+target+"\x1b\\") != 1 {
			t.Fatalf("message frame lost the OSC 8 target at width %d", width)
		}
	}
}
