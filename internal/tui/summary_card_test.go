package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestSummaryCardScrollsTogglesAndRendersBold(t *testing.T) {
	m := ready(t)
	m.width, m.height = 120, 40
	m.resize()
	long := strings.Repeat("Decision line with enough words to wrap across the summary card width. ", 30) +
		"\n\n**Blockers:** no committed ETA for acceptance criteria."
	m.summaries[m.active] = conversationSummary{Text: long, Messages: 3, Generated: time.Now()}
	m.resize()

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "J/K scroll 1-") {
		t.Fatal("long summary card is not marked scrollable")
	}
	if strings.Contains(view, "**") {
		t.Fatal("model bold markers rendered literally")
	}

	m.scrollSummary(1000)
	if m.summaryScroll == 0 {
		t.Fatal("J/K scroll did not move the card")
	}
	bottom := ansi.Strip(m.View().Content)
	if !strings.Contains(bottom, "no committed ETA") {
		t.Fatal("scrolled card does not reach the end of the summary")
	}

	m.scrollSummary(-1000)
	if m.summaryScroll != 0 {
		t.Fatalf("scroll not clamped at top: %d", m.summaryScroll)
	}

	m.toggleSummary()
	if !m.summaryHidden || m.summaryHeight() != 0 {
		t.Fatal("H did not hide the private summary")
	}
	if strings.Contains(ansi.Strip(m.View().Content), "PRIVATE SUMMARY") {
		t.Fatal("hidden summary still rendered")
	}
	m.toggleSummary()
	if m.summaryHidden || !strings.Contains(ansi.Strip(m.View().Content), "PRIVATE SUMMARY") {
		t.Fatal("H did not show the private summary again")
	}
}
