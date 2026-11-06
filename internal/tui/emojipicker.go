package tui

import (
	"cmp"
	"slices"
	"strings"
	"sync"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
	"github.com/kyokomi/emoji/v2"
)

// Reactions people reach for most often, shown before anything is typed.
var quickEmojis = []string{"+1", "heart", "joy", "tada", "eyes", "pray", "white_check_mark", "fire", "100", "raised_hands", "thinking_face", "rocket", "clap", "ok_hand", "smile"}

const (
	pickerRows      = 9
	composerRows    = 6
	minEmojiQuery   = 2
	recentEmojiKeep = 12
)

var (
	standardOnce  sync.Once
	standardNames []string
)

// standardEmojiNames lists Slack-style shortcodes the terminal can render.
func standardEmojiNames() []string {
	standardOnce.Do(func() {
		for code := range emoji.CodeMap() {
			name := strings.Trim(code, ":")
			if name != "" && !strings.ContainsAny(name, " :") {
				standardNames = append(standardNames, name)
			}
		}
		slices.Sort(standardNames)
	})
	return standardNames
}

// emojiNames merges the standard set with the workspace's custom emoji.
func (m Model) emojiNames() []string {
	standard := standardEmojiNames()
	if len(m.emojis) == 0 {
		return standard
	}
	seen := make(map[string]bool, len(standard)+len(m.emojis))
	names := make([]string, 0, len(standard)+len(m.emojis))
	for _, name := range standard {
		seen[name] = true
		names = append(names, name)
	}
	for name := range m.emojis {
		if !seen[name] && name != "" && !strings.ContainsAny(name, " :") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// emojiScore ranks how well query matches name; lower is better, -1 no match.
// Exact beats prefix beats word-prefix beats substring beats subsequence.
func emojiScore(name, query string) int {
	switch {
	case name == query:
		return 0
	case strings.HasPrefix(name, query):
		return 10 + len(name) - len(query)
	}
	for i := 1; i < len(name); i++ {
		if (name[i-1] == '_' || name[i-1] == '-' || name[i-1] == '+') && strings.HasPrefix(name[i:], query) {
			return 100 + i
		}
	}
	if i := strings.Index(name, query); i >= 0 {
		return 200 + i
	}
	// Fuzzy: query letters appear in order. Tighter spans score better.
	qi, first, last := 0, -1, 0
	q := []rune(query)
	for i, r := range []rune(name) {
		if qi < len(q) && r == q[qi] {
			if first < 0 {
				first = i
			}
			last = i
			qi++
		}
	}
	if qi < len(q) {
		return -1
	}
	return 300 + (last - first - len(q) + 1) + first
}

// searchEmoji returns up to limit names ordered by match quality.
func searchEmoji(names []string, query string, limit int) []string {
	query = strings.ToLower(strings.Trim(strings.TrimSpace(query), ":"))
	type hit struct {
		name  string
		score int
	}
	var hits []hit
	for _, name := range names {
		if score := emojiScore(name, query); score >= 0 {
			hits = append(hits, hit{name, score})
		}
	}
	slices.SortFunc(hits, func(a, b hit) int {
		return cmp.Or(cmp.Compare(a.score, b.score), cmp.Compare(len(a.name), len(b.name)), cmp.Compare(a.name, b.name))
	})
	out := make([]string, 0, min(limit, len(hits)))
	for _, h := range hits[:min(limit, len(hits))] {
		out = append(out, h.name)
	}
	return out
}

// suggestedEmojis is the empty-query list: recent picks, then the common set.
func (m Model) suggestedEmojis() []string {
	var out []string
	add := func(name string) {
		if !slices.Contains(out, name) && (m.emojiGlyph(name) != ":"+name+":" || m.emojis[name] != "") {
			out = append(out, name)
		}
	}
	for _, name := range m.recentEmoji {
		add(name)
	}
	for _, name := range quickEmojis {
		add(name)
	}
	return out
}

func (m *Model) rememberEmoji(name string) {
	m.recentEmoji = slices.DeleteFunc(m.recentEmoji, func(n string) bool { return n == name })
	m.recentEmoji = append([]string{name}, m.recentEmoji...)
	if len(m.recentEmoji) > recentEmojiKeep {
		m.recentEmoji = m.recentEmoji[:recentEmojiKeep]
	}
}

// emojiLabel shows the glyph next to its shortcode.
func (m Model) emojiLabel(name string) string {
	glyph := m.emojiGlyph(name)
	if glyph == ":"+name+":" {
		return "  :" + name + ":"
	}
	return glyph + " :" + name + ":"
}

// ---- reaction picker -------------------------------------------------

type emojiPicker struct {
	channel, ts string
	query       string
	results     []string
	index       int
}

type reactedMsg struct {
	channel, ts, name string
	added             bool
	err               error
}

// reactionTarget is the message the `e` key applies to: the selected block in
// a conversation or thread, the thread's parent pane, or the selected thread
// in the Threads feed.
func (m Model) reactionTarget() (channel, ts string, ok bool) {
	if m.destination == threadsDestination {
		for _, thread := range m.feed {
			if thread.Key() == m.feedSelection {
				return thread.Root.Channel, thread.Root.TS, true
			}
		}
		return "", "", false
	}
	if m.destination != "" || m.active == "" {
		return "", "", false
	}
	if m.focus == parentPane && m.thread != nil {
		return m.active, m.thread.ts, true
	}
	for _, message := range m.history {
		if message.TS == m.selectedMessage {
			return m.active, message.TS, true
		}
	}
	return "", "", false
}

// findMessage returns the current copy of a message, including its reactions.
func (m Model) findMessage(channel, ts string) (slack.Message, bool) {
	if m.active == channel {
		for _, message := range m.history {
			if message.TS == ts {
				return message, true
			}
		}
		if m.thread != nil {
			for _, message := range m.thread.history {
				if message.TS == ts {
					return message, true
				}
			}
		}
	}
	for _, thread := range m.feed {
		if thread.Root.Channel == channel && thread.Root.TS == ts {
			return thread.Root, true
		}
	}
	return slack.Message{}, false
}

func (m Model) reactedByMe(channel, ts, name string) bool {
	message, ok := m.findMessage(channel, ts)
	if !ok {
		return false
	}
	for _, reaction := range message.Reactions {
		if reaction.Name == name {
			return slices.Contains(reaction.Users, m.session.UserID)
		}
	}
	return false
}

func (m *Model) openReactPicker() tea.Cmd {
	if _, ok := m.api.(slack.ReactAPI); !ok {
		m.status = "Reactions are not available for this connection"
		return nil
	}
	channel, ts, ok := m.reactionTarget()
	if !ok {
		m.status = "Select a message to react to"
		return nil
	}
	m.picker = &emojiPicker{channel: channel, ts: ts}
	m.picker.refresh(*m)
	m.mode = reactMode
	return nil
}

func (p *emojiPicker) refresh(m Model) {
	if strings.TrimSpace(p.query) == "" {
		p.results = m.suggestedEmojis()
	} else {
		p.results = searchEmoji(m.emojiNames(), p.query, 50)
	}
	p.index = max(0, min(p.index, len(p.results)-1))
}

func (m *Model) closePicker() {
	m.picker = nil
	m.mode = normal
}

func (m *Model) updateReact(msg tea.Msg) (tea.Model, tea.Cmd) {
	p := m.picker
	if p == nil {
		m.mode = normal
		return *m, nil
	}
	switch msg := msg.(type) {
	case tea.PasteMsg:
		p.query += strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || unicode.IsControl(r) {
				return -1
			}
			return r
		}, msg.Content)
		p.index = 0
		p.refresh(*m)
	case tea.KeyPressMsg:
		switch key := msg.String(); key {
		case "ctrl+c":
			return *m, tea.Quit
		case "esc":
			m.closePicker()
		case "up", "ctrl+p", "ctrl+k", "shift+tab":
			if len(p.results) > 0 {
				p.index = (p.index - 1 + len(p.results)) % len(p.results)
			}
		case "down", "ctrl+n", "ctrl+j", "tab":
			if len(p.results) > 0 {
				p.index = (p.index + 1) % len(p.results)
			}
		case "backspace":
			if r := []rune(p.query); len(r) > 0 {
				p.query = string(r[:len(r)-1])
				p.index = 0
				p.refresh(*m)
			}
		case "ctrl+u":
			p.query, p.index = "", 0
			p.refresh(*m)
		case "enter":
			if len(p.results) == 0 {
				return *m, nil
			}
			name := p.results[p.index]
			channel, ts := p.channel, p.ts
			remove := m.reactedByMe(channel, ts, name)
			m.closePicker()
			return *m, m.react(channel, ts, name, !remove)
		default:
			if msg.Text != "" && !unicode.IsSpace([]rune(msg.Text)[0]) {
				p.query += msg.Text
				p.index = 0
				p.refresh(*m)
			}
		}
	}
	return *m, nil
}

// react adds or removes the user's reaction, then lets the authoritative
// refresh settle the counts.
func (m *Model) react(channel, ts, name string, add bool) tea.Cmd {
	api, ok := m.api.(slack.ReactAPI)
	if !ok || !m.canRequest() {
		return nil
	}
	m.status = "Reacting…"
	ctx := m.ctx
	return func() tea.Msg {
		var err error
		if add {
			err = api.AddReaction(ctx, channel, ts, name)
		} else {
			err = api.RemoveReaction(ctx, channel, ts, name)
		}
		return reactedMsg{channel, ts, name, add, err}
	}
}

func (m *Model) applyReacted(msg reactedMsg) tea.Cmd {
	if msg.err != nil {
		m.setError(msg.err)
		return nil
	}
	if msg.added {
		m.rememberEmoji(msg.name)
		m.status = "Reacted " + m.emojiGlyph(msg.name)
	} else {
		m.status = "Removed reaction " + m.emojiGlyph(msg.name)
	}
	m.editReaction(msg)
	m.renderHistory()
	// Authoritative counts replace the local edit when the API supports it.
	key := msg.channel + "/" + msg.ts
	m.eventVersion++
	if _, queued := m.reactionQueue[key]; queued || len(m.reactionQueue) < 128 {
		m.reactionQueue[key] = reactionRequest{msg.channel, msg.ts, m.eventVersion}
	}
	return m.nextReaction()
}

// editReaction updates every visible copy so the badge changes immediately.
func (m *Model) editReaction(msg reactedMsg) {
	change := func(message *slack.Message) {
		if message.TS != msg.ts {
			return
		}
		for i := range message.Reactions {
			r := &message.Reactions[i]
			if r.Name != msg.name {
				continue
			}
			mine := slices.Contains(r.Users, m.session.UserID)
			switch {
			case msg.added && !mine:
				r.Users = append(slices.Clone(r.Users), m.session.UserID)
				r.Count++
			case !msg.added && mine:
				r.Users = slices.DeleteFunc(slices.Clone(r.Users), func(u string) bool { return u == m.session.UserID })
				r.Count--
			}
			if r.Count <= 0 {
				message.Reactions = slices.Delete(slices.Clone(message.Reactions), i, i+1)
			}
			return
		}
		if msg.added {
			message.Reactions = append(slices.Clone(message.Reactions), slack.Reaction{Name: msg.name, Count: 1, Users: []string{m.session.UserID}})
		}
	}
	if m.active == msg.channel {
		for i := range m.history {
			change(&m.history[i])
		}
		if m.thread != nil {
			for i := range m.thread.history {
				change(&m.thread.history[i])
			}
		}
	}
	for i := range m.feed {
		if m.feed[i].Root.Channel != msg.channel {
			continue
		}
		change(&m.feed[i].Root)
		for j := range m.feed[i].LatestReplies {
			change(&m.feed[i].LatestReplies[j])
		}
		for j := range m.feed[i].UnreadReplies {
			change(&m.feed[i].UnreadReplies[j])
		}
	}
}

func (m Model) pickerView() string {
	p := m.picker
	title := lipgloss.NewStyle().Bold(true).Foreground(m.accent())
	muted := lipgloss.NewStyle().Foreground(m.muted())
	prompt := "react › " + p.query + "█"
	hint := "Type to search · recent and common first"
	if strings.TrimSpace(p.query) != "" {
		hint = "Fuzzy match on name"
	}
	lines := []string{prompt, muted.Render(hint)}
	count := max(1, m.mainHeight()-6)
	start := max(0, p.index-count+1)
	for i := start; i < min(len(p.results), start+count); i++ {
		name := p.results[i]
		label := m.emojiLabel(name)
		if m.reactedByMe(p.channel, p.ts, name) {
			label += "  ✓ yours · Enter removes"
		}
		if i == p.index {
			label = title.Render("› " + label)
		} else {
			label = "  " + label
		}
		lines = append(lines, label)
	}
	if len(p.results) == 0 {
		lines = append(lines, "No matching emoji")
	}
	return m.paneFrame("ADD REACTION", strings.Join(lines, "\n"), m.width, m.mainHeight(), true)
}

// ---- composer :emoji: dropdown ---------------------------------------

func isEmojiNameRune(r rune) bool {
	return r == '_' || r == '+' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// emojiQueryAt finds an unfinished ":name" immediately before the cursor.
// The colon must start the text or follow a non-word character, so times
// like 10:30 and URLs do not open the dropdown.
func emojiQueryAt(value []rune, pos int) (start int, query string, ok bool) {
	pos = max(0, min(pos, len(value)))
	i := pos
	for i > 0 && isEmojiNameRune(value[i-1]) {
		i--
	}
	if i == 0 || value[i-1] != ':' || pos-i < minEmojiQuery {
		return 0, "", false
	}
	if i-1 > 0 {
		before := value[i-2]
		if isEmojiNameRune(before) || before == ':' || before == '/' {
			return 0, "", false
		}
	}
	return i - 1, string(value[i:pos]), true
}

// refreshEmojiSuggestions recomputes the dropdown after every composer edit.
func (m *Model) refreshEmojiSuggestions() {
	previous := m.emojiQuery
	m.emojiSugg, m.emojiQuery = nil, ""
	if m.destination != "" || m.active == "" || m.sending {
		return
	}
	value := m.input.Value()
	start, query, ok := emojiQueryAt([]rune(value), m.input.Position())
	if !ok {
		m.emojiDismissed = ""
		return
	}
	if m.emojiDismissed == value {
		return
	}
	m.emojiStart = start
	m.emojiQuery = query
	m.emojiSugg = searchEmoji(m.emojiNames(), query, composerRows)
	if query != previous {
		m.emojiIndex = 0
	}
	m.emojiIndex = max(0, min(m.emojiIndex, len(m.emojiSugg)-1))
}

// emojiDropdownKey handles keys while the dropdown is open.
func (m *Model) emojiDropdownKey(key string) bool {
	if len(m.emojiSugg) == 0 {
		return false
	}
	switch key {
	case "up", "ctrl+p":
		m.emojiIndex = (m.emojiIndex - 1 + len(m.emojiSugg)) % len(m.emojiSugg)
	case "down", "ctrl+n":
		m.emojiIndex = (m.emojiIndex + 1) % len(m.emojiSugg)
	case "tab", "enter":
		m.acceptEmoji()
	case "esc":
		m.emojiDismissed = m.input.Value()
		m.emojiSugg, m.emojiQuery = nil, ""
	default:
		return false
	}
	return true
}

func (m *Model) acceptEmoji() {
	name := m.emojiSugg[m.emojiIndex]
	value := []rune(m.input.Value())
	pos := min(m.input.Position(), len(value))
	insert := []rune(":" + name + ": ")
	next := append(slices.Clone(value[:m.emojiStart]), insert...)
	next = append(next, value[pos:]...)
	m.input.SetValue(string(next))
	m.input.SetCursor(m.emojiStart + len(insert))
	m.drafts[m.draftKey()] = m.input.Value()
	m.rememberEmoji(name)
	m.emojiSugg, m.emojiQuery = nil, ""
}

// dropdownView is a small boxed list anchored above the composer.
func (m Model) dropdownView() string {
	if len(m.emojiSugg) == 0 {
		return ""
	}
	var lines []string
	width := 0
	for i, name := range m.emojiSugg {
		label := m.emojiLabel(name)
		if i == m.emojiIndex {
			label = "› " + label
		} else {
			label = "  " + label
		}
		lines = append(lines, label)
		width = max(width, lipgloss.Width(label))
	}
	for i, line := range lines {
		lines[i] = line + strings.Repeat(" ", width-lipgloss.Width(line))
		if i == m.emojiIndex {
			lines[i] = lipgloss.NewStyle().Bold(true).Foreground(m.accent()).Render(lines[i])
		}
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(m.accent()).Padding(0, 1)
	return box.Render(strings.Join(lines, "\n"))
}

// overlayBottom draws box over the last lines of content, near the left edge.
func overlayBottom(content, box string, x int) string {
	if box == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	boxLines := strings.Split(box, "\n")
	offset := len(lines) - len(boxLines)
	if offset < 0 {
		return content
	}
	for i, line := range boxLines {
		base := lines[offset+i]
		width := ansi.StringWidth(base)
		tail := ""
		if end := x + ansi.StringWidth(line); end < width {
			tail = ansi.Cut(base, end, width)
		}
		pad := ""
		if width < x {
			pad = strings.Repeat(" ", x-width)
		}
		lines[offset+i] = ansi.Truncate(base, x, "") + pad + line + tail
	}
	return strings.Join(lines, "\n")
}
