package tui

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/slack"
)

type imageFixture struct {
	*slack.Demo
	calls int
}

func (f *imageFixture) Image(context.Context, string) ([]byte, error) {
	f.calls++
	source := image.NewNRGBA(image.Rect(0, 0, 32, 16))
	for y := range 16 {
		for x := range 32 {
			source.SetNRGBA(x, y, color.NRGBA{uint8(x * 8), uint8(y * 16), 128, 255})
		}
	}
	var buffer bytes.Buffer
	err := png.Encode(&buffer, source)
	return buffer.Bytes(), err
}
func imageModel(t *testing.T) Model {
	t.Helper()
	m := ready(t)
	m.api = &imageFixture{Demo: slack.NewDemo()}
	m.history = []slack.Message{{TS: "200.1", Text: "A picture", Files: []slack.File{{ID: "F1", Name: "sample.png", Mimetype: "image/png", URLPrivate: "https://files.slack.com/sample.png"}}}}
	m.selectedMessage = "200.1"
	m.focus = messages
	m.renderHistory()
	return m
}
func TestVisibleImagesDownloadOnceAndViewerRestoresConversation(t *testing.T) {
	m := imageModel(t)
	cmd := m.loadVisibleImages()
	if cmd == nil {
		t.Fatal("visible image did not load")
	}
	next, more := m.Update(cmd())
	m = next.(Model)
	if more != nil || m.api.(*imageFixture).calls != 1 || !strings.Contains(m.viewport.View(), "▀") {
		t.Fatal("image was not rendered or was fetched twice")
	}
	if m.loadVisibleImages() != nil {
		t.Fatal("cache missed")
	}
	for _, size := range [][2]int{{60, 12}, {100, 28}, {160, 45}} {
		next, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		next, _ = m.Update(key('v'))
		m = next.(Model)
		if m.mode != imageMode {
			t.Fatal("v did not open image viewer")
		}
		view := m.View().Content
		if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
			t.Fatal("viewer escaped terminal bounds")
		}
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = next.(Model)
		if m.mode != normal || m.active == "" || m.selectedMessage != "200.1" {
			t.Fatal("closing image viewer lost conversation selection")
		}
	}
}
func TestImageSourcesIncludeBlocksAliasesAndReactionsWithoutCodeOrLinkFetches(t *testing.T) {
	m := imageModel(t)
	m.emojis = map[string]string{"parrot": "https://emoji.slack-edge.com/parrot.gif", "alias": "alias:parrot", "hidden": "https://emoji.slack-edge.com/hidden.png"}
	message := m.history[0]
	message.Text = ":alias: `:hidden:` <https://example.test/:hidden:>"
	message.Blocks = []slack.ImageBlock{{Type: "section", Accessory: &slack.ImageBlock{Type: "image", ImageURL: "https://files.slack.com/block.png", AltText: "A block image"}}}
	message.Reactions = []slack.Reaction{{Name: "parrot", Count: 2}}
	refs := m.messageImages(message)
	if len(refs) != 3 {
		t.Fatalf("expected file, block, deduplicated custom emoji; got %d", len(refs))
	}
	for _, ref := range refs {
		if strings.Contains(ref.url, "hidden") {
			t.Fatal("code or URL content triggered an emoji fetch")
		}
	}
}
func TestOffscreenImagesAndCacheAreBounded(t *testing.T) {
	m := imageModel(t)
	m.viewport.SetHeight(5)
	m.blockStarts["200.1"] = 100
	if m.loadVisibleImages() != nil {
		t.Fatal("offscreen image fetched")
	}
	for i := range imageCacheLimit + 10 {
		source := m.history[0]
		source.Files[0].URLPrivate = string(rune('a' + i))
		m.history = []slack.Message{source}
		m.viewport.SetHeight(20)
		m.renderHistory()
		m.viewport.SetYOffset(0)
		cmd := m.loadVisibleImages()
		if cmd == nil {
			t.Fatal("new visible image was starved")
		}
		next, _ := m.Update(cmd())
		m = next.(Model)
	}
	if len(m.images) > imageCacheLimit {
		t.Fatal("preview cache exceeded its memory bound")
	}
}

func BenchmarkVisibleImages(b *testing.B) {
	m := New(context.Background(), slack.NewDemo(), slack.Session{}, true)
	for i := range 1000 {
		m.history = append(m.history, slack.Message{TS: strconv.Itoa(i+1) + ".1", Text: "A long conversation with :wave: and no media to download"})
	}
	m.renderHistory()
	m.viewport.GotoBottom()
	b.ResetTimer()
	for range b.N {
		m.visibleImages()
	}
}
func TestParentImageVisibilityCacheAndEscFromReplies(t *testing.T) {
	m := imageModel(t)
	m.thread = &threadState{ts: "200.1", history: m.history, selection: "200.1"}
	m.history = []slack.Message{{TS: "201.1", Text: "A reply", ThreadTS: "200.1"}}
	m.selectedMessage = "201.1"
	m.resize()
	_ = m.visibleImages()
	cached := m.imageParent
	_ = m.visibleImages()
	if cached == nil || m.imageParent != cached {
		t.Fatal("unchanged parent layout was rendered again")
	}
	m.focus = parentPane
	next, _ := m.Update(key('v'))
	m = next.(Model)
	if m.mode != imageMode {
		t.Fatal("parent image did not open from split replies")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.thread == nil || m.mode != normal {
		t.Fatal("Esc closed replies instead of just the image")
	}
}

func TestImageFailureKeepsReservedBlockHeight(t *testing.T) {
	m := imageModel(t)
	m.emojis = map[string]string{"custom": "https://emoji.slack-edge.com/custom.png"}
	message := m.history[0]
	message.Text = ":custom:"
	for _, width := range []int{10, 30, 80} {
		m.images = map[string]*imageEntry{}
		before := lipgloss.Height(m.messageBlock(message, width))
		for _, ref := range m.messageImages(message) {
			m.images[ref.url] = &imageEntry{error: strings.Repeat("Unavailable preview diagnostic ", 10)}
		}
		after := lipgloss.Height(m.messageBlock(message, width))
		if before != after {
			t.Fatal("failed image moved message blocks")
		}
	}
}
