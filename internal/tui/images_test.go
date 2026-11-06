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
	"github.com/charmbracelet/x/ansi"
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
		source.Files[0].URLPrivate = "https://files.slack.com/" + strconv.Itoa(i) + ".png"
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
			t.Fatalf("failed image moved message blocks: width=%d before=%d after=%d", width, before, after)
		}
	}
}

func TestCustomEmojiStripIsCompactAndSideBySide(t *testing.T) {
	m := imageModel(t)
	m.emojis = map[string]string{
		"parrot": "https://emoji.slack-edge.com/parrot.png",
		"shipit": "https://emoji.slack-edge.com/shipit.png",
	}
	message := m.history[0]
	message.Text = ":parrot: :shipit: :wave:"
	message.Files = nil
	message.Blocks = nil
	m.history = []slack.Message{message}
	m.selectedMessage = message.TS
	m.images = map[string]*imageEntry{}
	for _, ref := range m.messageImages(message) {
		m.images[ref.url] = &imageEntry{image: image.NewNRGBA(image.Rect(0, 0, 8, 8)), used: 1}
	}
	block := m.messageBlock(message, 80)
	plain := ansi.Strip(block)
	var previewLines []string
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "▀") {
			previewLines = append(previewLines, line)
		}
	}
	if len(previewLines) != emojiCellRows {
		t.Fatalf("custom emoji strip used %d rows, want %d", len(previewLines), emojiCellRows)
	}
	if strings.Count(previewLines[0], "▀") != 2*emojiCellColumns {
		t.Fatalf("custom emoji were not laid out side by side: %q", previewLines[0])
	}
	if !strings.Contains(plain, ":parrot: :shipit: 👋") {
		t.Fatalf("custom emoji shortcodes should stay readable in text: %q", plain)
	}
}

func TestMediaCardsLinkFilesAndUnfurlsWithoutFakePreviews(t *testing.T) {
	m := ready(t)
	message := slack.Message{
		TS: "200.000002", User: "U1", Text: "Files and links",
		Files: []slack.File{
			{ID: "F1", Title: "Demo clip", Mimetype: "video/mp4", Size: 5 << 20, Permalink: "https://workspace.slack.com/files/F1", URLPrivate: "https://files.slack.com/clip.mp4"},
			{ID: "F2", Title: "Spec", Mimetype: "application/pdf", Filetype: "pdf", Size: 2048, Permalink: "https://workspace.slack.com/files/F2"},
			{ID: "F3", Title: "Giphy", Mimetype: "image/gif", URLPrivate: "https://media.giphy.com/x.gif", Permalink: "https://workspace.slack.com/files/F3"},
		},
		Attachments: []slack.Attachment{{
			Title: "A video", ProviderName: "YouTube",
			FromURL: "https://youtube.com/watch?v=1", VideoURL: "https://youtube.com/watch?v=1",
			ImageURL: "https://i.ytimg.com/vi/1/hq.jpg",
		}},
		Blocks: []slack.ImageBlock{{
			Type: "video", AltText: "Product demo",
			ThumbnailURL: "https://cdn.example.test/thumb.png", VideoURL: "https://example.test/video",
		}},
	}
	if refs := m.messageImages(message); len(refs) != 0 {
		t.Fatalf("external or non-image media became preview downloads: %+v", refs)
	}
	block := m.messageBlock(message, 80)
	plain := ansi.Strip(block)
	for _, want := range []string{"▶ Demo clip", "Video · 5.0 MB", "▤ Spec", "PDF · 2.0 KB", "▤ Giphy", "Image", "▶ A video", "Video · YouTube", "▶ Product demo"} {
		if !strings.Contains(plain, want) {
			t.Errorf("media card missing %q in:\n%s", want, plain)
		}
	}
	for _, link := range []string{"https://workspace.slack.com/files/F1", "https://workspace.slack.com/files/F2", "https://youtube.com/watch?v=1", "https://example.test/video"} {
		if !strings.Contains(block, "\x1b]8;;"+link+"\x1b\\") {
			t.Errorf("media card is missing clickable link %q", link)
		}
	}
	if strings.Contains(plain, "Preview unavailable") {
		t.Fatal("unsupported media rendered an error block instead of a card")
	}
	for _, line := range strings.Split(block, "\n") {
		if lipgloss.Width(line) != 80 {
			t.Fatalf("media card escaped the message frame: %q", line)
		}
	}
}

func TestVideoThumbnailPreviewIsLabeledAndLinked(t *testing.T) {
	m := ready(t)
	message := slack.Message{
		TS: "200.000003", User: "U1", Text: "Launch clip",
		Files: []slack.File{{
			ID: "F4", Title: "Launch", Mimetype: "video/mp4", Size: 2 << 20,
			Thumb360: "https://files.slack.com/thumb.png", URLPrivate: "https://files.slack.com/clip.mp4",
			Permalink: "https://workspace.slack.com/files/F4",
		}},
	}
	refs := m.messageImages(message)
	if len(refs) != 1 || refs[0].url != "https://files.slack.com/thumb.png" || !strings.HasPrefix(refs[0].label, "▶ Launch · Video · 2.0 MB") {
		t.Fatalf("video thumbnail was not selected with media metadata: %+v", refs)
	}
	block := m.messageBlock(message, 80)
	if !strings.Contains(block, "\x1b]8;;https://workspace.slack.com/files/F4\x1b\\") {
		t.Fatal("video preview caption is not clickable")
	}
	if strings.Contains(block, "clip.mp4") {
		t.Fatal("video URL was treated as a still image")
	}
}
