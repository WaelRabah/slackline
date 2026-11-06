package tui

import (
	"context"
	"fmt"
	"github.com/WaelRabah/slackline/internal/logs"
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
	url, label, link string
	emoji            bool
}
type imageEntry struct {
	image         image.Image
	raw           []byte
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
	raw   []byte
}

// All state changes run in Bubble Tea's update loop; downloads and decoding run
// in commands. Only visible previews are loaded, with one request in flight.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// ctrl+y toggles notification sounds from any screen.
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "ctrl+y" {
		m.toggleSound()
		return m, nil
	}
	var cmd tea.Cmd
	switch event := msg.(type) {
	case imageMsg, historyMsg, parentHistoryMsg, threadsMsg, emojisMsg, reactionsMsg, bgResult, preferencesSaved:
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
			entry.raw = result.raw
			if result.err != nil {
				logs.Error("image", result.err)
				entry.error = cleanLine(result.err.Error())
			}
		}
		if m.mode == imageMode {
			cmd = m.inlineImageCmd()
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
			cmd = func() tea.Msg { return tea.ClearScreen() }
		case "o":
			cmd = m.openImageExternally()
		case "right", "l", "j", "down":
			m.imageIndex = (m.imageIndex + 1) % len(m.viewer)
			cmd = m.inlineImageCmd()
		case "left", "h", "k", "up":
			m.imageIndex = (m.imageIndex + len(m.viewer) - 1) % len(m.viewer)
			cmd = m.inlineImageCmd()
		}
	} else if _, ok := msg.(inlineDrawMsg); ok {
		cmd = m.inlineImageRaw()
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
			cmd = m.inlineImageCmd()
		} else {
			m.status = "No previewable media in this message"
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
	summaries := m.summaryHook(msg)
	images := m.loadVisibleImages()
	return m, tea.Batch(cmd, summaries, images)
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
	add := func(raw, label, link string, emoji bool) {
		if raw == "" || seen[raw] || len(refs) >= 8 || !slack.Previewable(raw) {
			return
		}
		if label = cleanLine(label); label == "" {
			label = "Image"
		}
		seen[raw] = true
		refs = append(refs, imageRef{url: raw, label: label, link: link, emoji: emoji})
	}
	for _, file := range message.Files {
		link := file.Permalink
		if link == "" {
			link = file.URLPrivate
		}
		switch {
		case file.IsImage():
			add(file.PreviewURL(), fileTitle(file), link, false)
		case file.IsVideo():
			add(file.PreviewURL(), "▶ "+fileTitle(file)+" · "+fileMeta(file), link, false)
		}
	}
	for _, attachment := range message.Attachments {
		raw := attachment.ImageURL
		if raw == "" {
			raw = attachment.ThumbURL
		}
		label := attachmentTitle(attachment)
		if attachment.VideoURL != "" {
			label = "▶ " + label
		}
		add(raw, label, attachment.LinkURL(), false)
	}
	var blocks func([]slack.ImageBlock, int)
	blocks = func(items []slack.ImageBlock, depth int) {
		if depth > 6 {
			return
		}
		for _, block := range items {
			switch block.Type {
			case "image":
				add(block.ImageURL, block.AltText, "", false)
			case "video":
				label := block.AltText
				if label == "" {
					label = "Video"
				}
				add(block.ThumbnailURL, "▶ "+label+" · Video", block.VideoURL, false)
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
			add(m.customEmojiURL(name), ":"+name+":", "", true)
		}
	}
	for _, reaction := range message.Reactions {
		if reaction.Count > 0 {
			add(m.customEmojiURL(reaction.Name), ":"+reaction.Name+":", "", true)
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
			if err != nil {
				data = nil
			}
			return imageMsg{url: ref.url, image: decoded, err: err, raw: data}
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

const (
	imagePreviewRows = 10
	emojiCellColumns = 6
	emojiCellRows    = 3
)

func (m Model) mediaBlocks(message slack.Message, width int) string {
	var out strings.Builder
	var emoji []imageRef
	for _, ref := range m.messageImages(message) {
		if ref.emoji {
			emoji = append(emoji, ref)
			continue
		}
		out.WriteString("\n\n" + m.mediaLabel(ref, width) + "\n")
		preview := m.renderImage(ref, min(width, 64), imagePreviewRows)
		// Reserve rows while loading so downloads do not move the selected block.
		out.WriteString(lipgloss.NewStyle().Height(imagePreviewRows).MaxHeight(imagePreviewRows).Render(preview))
	}
	if len(emoji) > 0 {
		out.WriteString("\n\n" + m.emojiStrip(emoji, width))
	}
	if cards := m.mediaCards(message, width); cards != "" {
		out.WriteString("\n\n" + cards)
	}
	return out.String()
}

// mediaLabel makes the caption of a file preview clickable without moving the
// preview out of the message block.
func (m Model) mediaLabel(ref imageRef, width int) string {
	label := ansi.Hardwrap("▧ "+ref.label, width, true)
	safe, ok := safeLink(ref.link)
	if !ok {
		return label
	}
	lines := strings.Split(label, "\n")
	for i := range lines {
		lines[i] = m.linkText(safe, lines[i])
	}
	return strings.Join(lines, "\n")
}

func (m Model) emojiStrip(refs []imageRef, width int) string {
	columns := min(emojiCellColumns, max(1, width))
	var rows []string
	var current []string
	currentWidth := 0
	flush := func() {
		if len(current) == 0 {
			return
		}
		rows = append(rows, joinSideBySide(current, " "))
		current = nil
		currentWidth = 0
	}
	for _, ref := range refs {
		if currentWidth > 0 && currentWidth+1+columns > width {
			flush()
		}
		if currentWidth > 0 {
			currentWidth++
		}
		current = append(current, m.renderEmoji(ref, columns))
		currentWidth += columns
	}
	flush()
	return strings.Join(rows, "\n")
}

func joinSideBySide(blocks []string, gap string) string {
	split := make([][]string, len(blocks))
	height := 0
	for i, block := range blocks {
		split[i] = strings.Split(block, "\n")
		height = max(height, len(split[i]))
	}
	lines := make([]string, height)
	for row := range height {
		var line strings.Builder
		for column, block := range split {
			if column > 0 {
				line.WriteString(gap)
			}
			if row < len(block) {
				line.WriteString(block[row])
			}
		}
		lines[row] = line.String()
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderEmoji(ref imageRef, columns int) string {
	entry := m.images[ref.url]
	if entry == nil || entry.image == nil {
		return padCell(lipgloss.NewStyle().Foreground(m.muted()).Render("▧"), columns, emojiCellRows)
	}
	return padCell(m.renderImage(ref, columns, emojiCellRows), columns, emojiCellRows)
}

func padCell(content string, columns, rows int) string {
	lines := strings.Split(content, "\n")
	cells := make([]string, rows)
	for row := range rows {
		line := ""
		if row < len(lines) {
			line = ansi.Truncate(lines[row], columns, "")
		}
		cells[row] = line + strings.Repeat(" ", max(0, columns-ansi.StringWidth(line)))
	}
	return strings.Join(cells, "\n")
}

type mediaCard struct {
	icon, title, meta, url string
}

func (m Model) mediaCards(message slack.Message, width int) string {
	var cards []mediaCard
	seen := map[string]bool{}
	add := func(card mediaCard) {
		card.title = cleanLine(card.title)
		if card.title == "" {
			card.title = "Media"
		}
		card.meta = cleanLine(card.meta)
		key := card.url + "\x00" + card.title
		if seen[key] || len(cards) >= 8 {
			return
		}
		seen[key] = true
		cards = append(cards, card)
	}
	for _, file := range message.Files {
		if slack.Previewable(file.PreviewURL()) {
			continue
		}
		link := file.Permalink
		if link == "" {
			link = file.URLPrivate
		}
		icon := "▤"
		switch {
		case file.IsVideo():
			icon = "▶"
		case file.IsAudio():
			icon = "♪"
		}
		add(mediaCard{icon: icon, title: fileTitle(file), meta: fileMeta(file), url: link})
	}
	for _, attachment := range message.Attachments {
		raw := attachment.ImageURL
		if raw == "" {
			raw = attachment.ThumbURL
		}
		if slack.Previewable(raw) {
			continue
		}
		link := attachment.LinkURL()
		if link == "" {
			link = raw
		}
		card := mediaCard{icon: "↗", title: attachmentTitle(attachment), url: link}
		if attachment.VideoURL != "" {
			card.icon = "▶"
			card.meta = "Video"
		}
		if provider := attachment.ProviderName; provider != "" {
			card.meta = strings.TrimSpace(card.meta + " · " + provider)
		} else if service := attachment.ServiceName; service != "" {
			card.meta = strings.TrimSpace(card.meta + " · " + service)
		}
		if card.meta == "" {
			card.meta = "Link"
		}
		add(card)
	}
	var filtered []slack.ImageBlock
	var blocks func([]slack.ImageBlock, int)
	blocks = func(items []slack.ImageBlock, depth int) {
		if depth > 6 {
			return
		}
		for _, block := range items {
			if block.Type == "image" && block.ImageURL != "" && !slack.Previewable(block.ImageURL) {
				filtered = append(filtered, block)
			}
			if block.Type == "video" && !slack.Previewable(block.ThumbnailURL) {
				filtered = append(filtered, block)
			}
			if block.Accessory != nil {
				blocks([]slack.ImageBlock{*block.Accessory}, depth+1)
			}
			blocks(block.Elements, depth+1)
		}
	}
	blocks(message.Blocks, 0)
	for _, block := range filtered {
		label := block.AltText
		card := mediaCard{icon: "▧", title: label, meta: "Image", url: block.ImageURL}
		if block.Type == "video" {
			if label == "" {
				label = "Video"
			}
			card = mediaCard{icon: "▶", title: label, meta: "Video", url: block.VideoURL}
			if card.url == "" {
				card.url = block.ThumbnailURL
			}
		}
		if label == "" {
			label = "Image"
		}
		card.title = label
		add(card)
	}
	rendered := make([]string, 0, len(cards))
	for _, card := range cards {
		rendered = append(rendered, m.renderMediaCard(card, width))
	}
	return strings.Join(rendered, "\n")
}

func (m Model) renderMediaCard(card mediaCard, width int) string {
	lines := strings.Split(ansi.Hardwrap(card.icon+" "+card.title, width, true), "\n")
	if safe, ok := safeLink(card.url); ok {
		for i := range lines {
			lines[i] = m.linkText(safe, lines[i])
		}
	}
	body := strings.Join(lines, "\n")
	if card.meta != "" {
		body += "\n" + lipgloss.NewStyle().Foreground(m.muted()).Render(ansi.Hardwrap(card.meta, width, true))
	}
	return body
}

func fileTitle(file slack.File) string {
	for _, value := range []string{file.Title, file.Name, file.ID} {
		if strings.TrimSpace(value) != "" {
			return cleanLine(value)
		}
	}
	return "File"
}

func fileMeta(file slack.File) string {
	parts := []string{fileKind(file)}
	if size := humanSize(file.Size); size != "" {
		parts = append(parts, size)
	}
	return strings.Join(parts, " · ")
}

func fileKind(file slack.File) string {
	switch {
	case file.IsVideo():
		return "Video"
	case file.IsAudio():
		return "Audio"
	case file.IsImage():
		return "Image"
	}
	if file.Filetype != "" {
		return strings.ToUpper(file.Filetype)
	}
	if _, subtype, ok := strings.Cut(file.Mimetype, "/"); ok && subtype != "" {
		return strings.ToUpper(subtype)
	}
	return "File"
}

func attachmentTitle(attachment slack.Attachment) string {
	for _, value := range []string{attachment.Title, attachment.ProviderName, attachment.ServiceName, attachment.Fallback} {
		if strings.TrimSpace(value) != "" {
			return cleanLine(value)
		}
	}
	if link := attachment.LinkURL(); link != "" {
		return cleanLine(link)
	}
	return "Link"
}

func humanSize(size int64) string {
	if size <= 0 {
		return ""
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	value := float64(size)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d B", size)
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}
func (m Model) imageView() string {
	if len(m.viewer) == 0 {
		return "No image selected"
	}
	ref := m.viewer[m.imageIndex]
	title := fmt.Sprintf("IMAGE %d/%d · Esc closes · ←/→ next · o opens in Preview", m.imageIndex+1, len(m.viewer))
	picture := m.renderImage(ref, max(1, m.width-6), max(1, m.mainHeight()-7))
	if inlineImagesSupported() {
		picture = strings.Repeat("\n", max(0, m.mainHeight()-8))
	}
	body := ansi.Hardwrap(ref.label, max(1, m.width-6), true) + "\n\n" + picture
	return m.paneFrame(title, body, m.width, m.mainHeight(), true)
}
