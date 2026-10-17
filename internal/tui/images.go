package tui

import (
	"context"
	"fmt"
	"image"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/media"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
)

const imageCacheLimit = 24

type imageRef struct {
	url, label string
	emoji      bool
}
type imageEntry struct {
	image         image.Image
	error         string
	used          uint64
	columns, rows int
	rendered      string
}
type parentImageKey struct {
	thread                            *threadState
	width, height, offset, generation int
	selection                         string
}
type parentImageCache struct {
	key  parentImageKey
	refs []imageRef
}

type imageMsg struct {
	url   string
	image image.Image
	err   error
}

// All state changes run in Bubble Tea's update loop; downloads and decoding run
// in commands. Only visible previews are loaded, with one request in flight.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch event := msg.(type) {
	case imageMsg, historyMsg, parentHistoryMsg, threadsMsg, emojisMsg, reactionsMsg, summaryMsg, preferencesSaved:
		m.imageParent = nil
	case slack.LiveEvent:
		if event.Type == "message" {
			m.imageParent = nil
		}
	}
	if result, ok := msg.(imageMsg); ok {
		m.imageLoading = ""
		entry := m.images[result.url]
		if entry != nil {
			entry.image = result.image
			if result.err != nil {
				entry.error = cleanLine(result.err.Error())
			}
		}
		bottom := m.viewport.AtBottom()
		m.renderHistory()
		if bottom {
			m.viewport.GotoBottom()
		}
	} else if key, ok := msg.(tea.KeyPressMsg); ok && m.mode == imageMode {
		switch key.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc", "q", "v":
			m.mode = normal
			m.viewer = nil
		case "right", "l", "j", "down":
			m.imageIndex = (m.imageIndex + 1) % len(m.viewer)
		case "left", "h", "k", "up":
			m.imageIndex = (m.imageIndex + len(m.viewer) - 1) % len(m.viewer)
		}
	} else if key, ok := msg.(tea.KeyPressMsg); ok && m.mode == normal && key.String() == "v" && (m.focus == messages || m.focus == parentPane) {
		m.viewer = nil
		source := m.history
		selection := m.selectedMessage
		if m.focus == parentPane && m.thread != nil {
			source = m.thread.history
			selection = m.thread.selection
		}
		if (m.focus == messages && m.destination == threadsDestination) || (m.focus == parentPane && m.thread != nil && m.thread.fromFeed) {
			source = nil
			for _, thread := range m.feed {
				if thread.Key() == m.feedSelection {
					source = []slack.Message{thread.Root}
					selection = thread.Root.TS
					break
				}
			}
		}
		for _, message := range source {
			if message.TS == selection {
				m.viewer = m.messageImages(message)
				break
			}
		}
		if len(m.viewer) > 0 {
			m.mode = imageMode
			m.imageIndex = 0
		} else {
			m.status = "No images in this message"
		}
	} else {
		if key, ok := msg.(tea.KeyPressMsg); ok && m.mode == normal && key.String() == "R" {
			m.imageParent = nil
			for _, ref := range m.visibleImages() {
				if entry := m.images[ref.url]; entry != nil && entry.error != "" {
					delete(m.images, ref.url)
				}
			}
		}
		updated, next := m.update(msg)
		m = updated.(Model)
		cmd = next
	}
	images := m.loadVisibleImages()
	return m, tea.Batch(cmd, images)
}

func (m Model) customEmojiURL(name string) string {
	for range 16 {
		value := m.emojis[name]
		if strings.HasPrefix(value, "alias:") {
			name = strings.TrimPrefix(value, "alias:")
			continue
		}
		if strings.HasPrefix(value, "https://") {
			return value
		}
		return ""
	}
	return ""
}
func (m Model) messageImages(message slack.Message) []imageRef {
	var refs []imageRef
	seen := map[string]bool{}
	add := func(raw, label string, emoji bool) {
		if raw != "" && !seen[raw] && len(refs) < 8 {
			seen[raw] = true
			refs = append(refs, imageRef{raw, cleanLine(label), emoji})
		}
	}
	for _, file := range message.Files {
		if strings.HasPrefix(file.Mimetype, "image/") {
			label := file.Title
			if label == "" {
				label = file.Name
			}
			if label == "" {
				label = "Image"
			}
			add(file.PreviewURL(), label, false)
		}
	}
	for _, attachment := range message.Attachments {
		raw := attachment.ImageURL
		if raw == "" {
			raw = attachment.ThumbURL
		}
		label := attachment.Title
		if label == "" {
			label = attachment.Fallback
		}
		if label == "" {
			label = "Image attachment"
		}
		add(raw, label, false)
	}
	var blocks func([]slack.ImageBlock, int)
	blocks = func(items []slack.ImageBlock, depth int) {
		if depth > 6 {
			return
		}
		for _, block := range items {
			if block.Type == "image" {
				add(block.ImageURL, block.AltText, false)
			}
			if block.Accessory != nil {
				blocks([]slack.ImageBlock{*block.Accessory}, depth+1)
			}
			blocks(block.Elements, depth+1)
		}
	}
	blocks(message.Blocks, 0)
	for _, token := range emojiToken.FindAllString(message.Text, 32) {
		if token[0] == ':' {
			name := token[1 : len(token)-1]
			add(m.customEmojiURL(name), ":"+name+":", true)
		}
	}
	for _, reaction := range message.Reactions {
		if reaction.Count > 0 {
			add(m.customEmojiURL(reaction.Name), ":"+reaction.Name+":", true)
		}
	}
	return refs
}
func (m *Model) visibleImages() []imageRef {
	if m.mode == imageMode && len(m.viewer) > 0 {
		return []imageRef{m.viewer[m.imageIndex]}
	}
	if m.mode != normal && m.mode != insert {
		return nil
	}
	var refs []imageRef
	if m.destination == threadsDestination {
		for _, thread := range m.feed {
			start, exists := m.blockStarts[thread.Key()]
			end := m.blockEnds[thread.Key()]
			if exists && start < m.viewport.YOffset()+m.viewport.Height() && end > m.viewport.YOffset() {
				refs = append(refs, m.messageImages(thread.Root)...)
			}
		}
	} else if m.destination == "" {
		for _, message := range m.history {
			start, exists := m.blockStarts[message.TS]
			end := m.blockEnds[message.TS]
			if exists && start < m.viewport.YOffset()+m.viewport.Height() && end > m.viewport.YOffset() {
				refs = append(refs, m.messageImages(message)...)
			}
		}
	}
	if m.thread != nil {
		key := parentImageKey{m.thread, m.width, m.height, m.thread.offset, m.generation, m.thread.selection}
		if m.imageParent == nil || m.imageParent.key != key {
			parent := m.parentModel((m.width - m.sidebarWidth() - 2) / 2)
			m.imageParent = &parentImageCache{key, parent.visibleImages()}
		}
		refs = append(refs, m.imageParent.refs...)
	}
	return refs
}
func (m *Model) loadVisibleImages() tea.Cmd {
	if m.thread == nil {
		m.imageParent = nil
	}
	if m.imageLoading != "" {
		return nil
	}
	api, ok := m.api.(slack.ImageAPI)
	if !ok {
		return nil
	}
	refs := m.visibleImages()
	visible := map[string]bool{}
	for _, ref := range refs {
		visible[ref.url] = true
	}
	for _, ref := range refs {
		m.imageClock++
		if entry := m.images[ref.url]; entry != nil {
			entry.used = m.imageClock
			continue
		}
		if len(m.images) >= imageCacheLimit {
			oldest := ""
			for key, entry := range m.images {
				if !visible[key] && (oldest == "" || entry.used < m.images[oldest].used) {
					oldest = key
				}
			}
			if oldest == "" {
				continue
			}
			delete(m.images, oldest)
		}
		m.images[ref.url] = &imageEntry{used: m.imageClock}
		m.imageLoading = ref.url
		ctx := m.ctx
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			data, err := api.Image(ctx, ref.url)
			var decoded image.Image
			if err == nil {
				decoded, err = media.Decode(data)
			}
			return imageMsg{ref.url, decoded, err}
		}
	}
	return nil
}
func (m Model) renderImage(ref imageRef, columns, rows int) string {
	entry := m.images[ref.url]
	if entry == nil || (entry.image == nil && entry.error == "") {
		return ansi.Truncate("Loading image…", columns, "…")
	}
	if entry.error != "" {
		return ansi.Hardwrap("Preview unavailable: "+entry.error, columns, true)
	}
	if entry.columns != columns || entry.rows != rows || entry.rendered == "" {
		entry.columns, entry.rows = columns, rows
		entry.rendered = media.Render(entry.image, columns, rows)
	}
	return entry.rendered
}
func (m Model) imageBlocks(message slack.Message, width int) string {
	var out strings.Builder
	for _, ref := range m.messageImages(message) {
		rows := 10
		if ref.emoji {
			rows = 2
		}
		out.WriteString("\n\n" + ansi.Hardwrap("▧ "+ref.label, width, true) + "\n")
		preview := m.renderImage(ref, min(width, 64), rows)
		// Reserve rows while loading so downloads do not move the selected block.
		out.WriteString(lipgloss.NewStyle().Height(rows).MaxHeight(rows).Render(preview))
	}
	return out.String()
}
func (m Model) imageView() string {
	if len(m.viewer) == 0 {
		return "No image selected"
	}
	ref := m.viewer[m.imageIndex]
	title := fmt.Sprintf("IMAGE %d/%d · Esc closes · ←/→ next", m.imageIndex+1, len(m.viewer))
	body := ansi.Hardwrap(ref.label, max(1, m.width-6), true) + "\n\n" + m.renderImage(ref, max(1, m.width-6), max(1, m.mainHeight()-7))
	return m.paneFrame(title, body, m.width, m.mainHeight(), true)
}
