package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestMarkdownRendersStructureWithoutRawMarkers(t *testing.T) {
	input := "# Title\n\n- **bold** and `code` and *soft*\n- [Docs](https://example.test/a)\n> quoted\n\n---\n1. step one"
	got := renderMarkdown(input, 80)
	plain := ansi.Strip(got)
	for _, raw := range []string{"#", "**", "`", "](", "> "} {
		if strings.Contains(plain, raw) {
			t.Fatalf("raw marker %q left in output: %q", raw, plain)
		}
	}
	for _, want := range []string{"Title", "• bold and code and soft", "• Docs", "│ quoted", "1. step one"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in %q", want, plain)
		}
	}
	if !strings.Contains(got, "\x1b]8;;https://example.test/a") {
		t.Fatal("link target not wrapped as a hyperlink")
	}
}

func TestMarkdownKeepsCodeBlocksLiteralAndRejectsUnsafeLinks(t *testing.T) {
	input := "```\n**raw** [x](https://example.test)\n```\n[bad](javascript:alert(1))"
	got := renderMarkdown(input, 80)
	plain := ansi.Strip(got)
	if !strings.Contains(plain, "**raw** [x](https://example.test)") {
		t.Fatalf("code block changed: %q", plain)
	}
	if strings.Contains(got, "javascript:") {
		t.Fatal("unsafe link target was emitted")
	}
	if !strings.Contains(plain, "bad") {
		t.Fatal("unsafe link label dropped")
	}
}
