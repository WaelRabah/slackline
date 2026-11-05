package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/WaelRabah/slackline/internal/slack"
)

// End to end through the model: open a demo conversation with an image, press v,
// and check the picture is scheduled after the frame and then drawn in the viewer.
func TestViewerDrawsDemoImageAfterFrame(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "WarpTerminal")
	m := ready(t)
	general := -1
	for i, c := range m.conversations {
		if c.Name == "general" {
			general = i
		}
	}
	if general < 0 {
		t.Skip("demo has no #general")
	}
	m.selected = general
	m.openSelected()
	m.loading = false
	m.focus = messages
	history, err := m.api.History(m.ctx, "CGENERAL", "")
	if err != nil {
		t.Fatal(err)
	}
	m.history = history.Messages
	var image slack.Message
	for _, msg := range m.history {
		if len(msg.Files) > 0 {
			image = msg
		}
	}
	if image.TS == "" {
		t.Skip("no image message in demo history")
	}
	m.selectedMessage = image.TS
	m.viewer = m.messageImages(image)
	m.mode = imageMode
	m.imageIndex = 0
	if !inlineImagesSupported() {
		t.Fatal("Warp not detected")
	}
	// The picture needs the bytes; the demo returns fixtures.
	data, err := m.api.(slack.ImageAPI).Image(m.ctx, m.viewer[0].url)
	if err != nil {
		t.Fatal(err)
	}
	m.images[m.viewer[0].url] = &imageEntry{raw: data}
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'x'})
	_ = next
	if cmd != nil {
		t.Log("unexpected command for an unrelated key")
	}
	cmd = m.inlineImageCmd()
	if cmd == nil {
		t.Fatal("no draw scheduled")
	}
	if _, ok := cmd().(inlineDrawMsg); !ok {
		t.Fatal("draw is not scheduled after a delay")
	}
	raw := m.inlineImageRaw()
	if raw == nil {
		t.Fatal("no picture to draw while the viewer is open")
	}
}
