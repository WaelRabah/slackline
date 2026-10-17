package tui

import (
	"context"
	"errors"
	"fmt"
	"html"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/inference"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
)

type pane int

const (
	sidebar pane = iota
	messages
	composer
	parentPane
)

type mode int

const (
	normal mode = iota
	insert
	finder
	settingsMode
	summaryMode
)

type bootstrapMsg struct {
	conversations []slack.Conversation
	sections      []slack.Section
	users         []slack.User
	err           error
}
type historyMsg struct {
	version         int
	channel, cursor string
	generation      int
	history         slack.History
	err             error
}
type threadState struct {
	fromFeed          bool
	ts                string
	history           []slack.Message
	cursor, selection string
	offset            int
}

type sentMsg struct {
	thread, draftKey string
	channel, text    string
	message          slack.Message
	err              error
}

type Model struct {
	reactionLoading   string
	reactionQueue     map[string]reactionRequest
	reactionSnapshots map[string]reactionSnapshot
	emojis            map[string]string
	revealMessage     string
	searchHits        []slack.SearchHit
	searchLoading     bool
	searchQuery       string
	configPath, theme string
	provider          inference.Settings
	settings          *settingsState
	summaries         map[string]conversationSummary
	summaryBusy       map[string]bool
	summaryViewport   viewport.Model
	directory         []slack.User
	finderGeneration  int
	finderUsers       []string

	liveState                                          string
	lastLiveFeedRefresh                                time.Time
	eventVersion, lastThreadMutation                   int
	liveSeen                                           map[string]bool
	liveOrder                                          []string
	messageVersions, channelVersions, threadVersions   map[string]int
	typing                                             map[string]map[string]time.Time
	typingGeneration                                   int
	typingScheduled                                    bool
	destination                                        string
	previousVisit                                      time.Time
	unread                                             map[string]unreadState
	remoteRead, readPositions                          map[string]string
	newMessages                                        map[string]bool
	pendingMarks                                       map[string]readRequest
	countBusy, markBusy, markScheduled, readEnabled    bool
	feed                                               []slack.Thread
	feedSelection, feedNext, feedError                 string
	feedPaged                                          bool
	feedPollGeneration                                 int
	feedLoading, feedReady                             bool
	feedUnread                                         int
	sidebarKey                                         string
	statePath, stateWarning                            string
	recentDays                                         int
	recentOnly, soundEnabled, activityReady, sentReady bool
	interactions                                       map[string]time.Time
	latest, ownLatest                                  map[string]string
	startedAt                                          time.Time
	now                                                func() time.Time
	notify                                             func() tea.Cmd
	sections                                           []slack.Section
	collapsed                                          map[string]bool
	thread                                             *threadState
	selectedMessage                                    string
	blockStarts                                        map[string]int
	ctx                                                context.Context
	api                                                slack.API
	session                                            slack.Session
	demo                                               bool
	width, height                                      int
	focus                                              pane
	mode                                               mode
	conversations                                      []slack.Conversation
	users                                              map[string]string
	selected                                           int
	active                                             string
	history                                            []slack.Message
	cursor                                             string
	generation                                         int
	loading, sending, booting                          bool
	ready                                              bool
	pendingG                                           bool
	status                                             string
	retryUntil                                         time.Time
	drafts                                             map[string]string
	input                                              textinput.Model
	query                                              textinput.Model
	matches                                            []int
	matchSelected                                      int
	viewport                                           viewport.Model
}

func New(ctx context.Context, api slack.API, session slack.Session, demo bool, options ...Options) Model {
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "i to compose a message"
	input.CharLimit = 4000
	query := textinput.New()
	query.Prompt = "/ "
	query.Placeholder = "Find conversations or workspace users"
	vp := viewport.New(viewport.WithWidth(70), viewport.WithHeight(20))
	vp.SoftWrap = true
	m := Model{ctx: ctx, api: api, session: session, demo: demo, width: 100, height: 28, users: map[string]string{}, drafts: map[string]string{}, input: input, query: query, viewport: vp, booting: true, status: "Loading conversations…"}
	m.theme = "mint"
	m.reactionQueue = map[string]reactionRequest{}
	m.reactionSnapshots = map[string]reactionSnapshot{}
	m.summaries = map[string]conversationSummary{}
	m.summaryBusy = map[string]bool{}
	m.summaryViewport = viewport.New(viewport.WithWidth(70), viewport.WithHeight(20))
	m.now = time.Now
	m.startedAt = m.now()
	m.recentDays, m.recentOnly, m.soundEnabled = 14, true, true
	m.interactions = map[string]time.Time{}
	m.latest, m.ownLatest = map[string]string{}, map[string]string{}
	m.collapsed = map[string]bool{}
	m.notify = func() tea.Cmd { return notification(ctx) }
	m.unread = map[string]unreadState{}
	m.remoteRead = map[string]string{}
	m.readPositions = map[string]string{}
	m.newMessages = map[string]bool{}
	m.pendingMarks = map[string]readRequest{}
	m.liveSeen = map[string]bool{}
	m.messageVersions = map[string]int{}
	m.channelVersions = map[string]int{}
	m.threadVersions = map[string]int{}
	m.typing = map[string]map[string]time.Time{}
	m.liveState = "offline"
	if _, ok := api.(slack.LiveAPI); ok {
		m.liveState = "connecting"
	}
	if len(options) > 0 {
		opts := options[0]
		m.statePath = opts.StatePath
		m.configPath = opts.ConfigPath
		m.provider = opts.Provider
		if opts.Theme == "ocean" || opts.Theme == "amber" {
			m.theme = opts.Theme
		}
		if opts.RecentDays != nil {
			m.recentDays = *opts.RecentDays
			m.recentOnly = m.recentDays > 0
		}
		m.loadState()
		if opts.SoundEnabled != nil {
			m.soundEnabled = *opts.SoundEnabled
		}
	}
	if m.provider.BaseURL == "" {
		m.provider.BaseURL = "https://api.compactif.ai/v1"
	}
	m.resize()
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.bootstrap(), m.activityCmd(), m.recentCmd(), m.fetchEmojis(), func() tea.Msg { return threadsTick{} })
}
func (m Model) bootstrap() tea.Cmd {
	return func() tea.Msg {
		conversations, err := m.api.Conversations(m.ctx)
		if err != nil {
			return bootstrapMsg{err: err}
		}
		users, err := m.api.Users(m.ctx)
		// Conversations still work if a workspace restricts the user directory.
		var sections []slack.Section
		if reader, ok := m.api.(interface {
			Sections(context.Context) ([]slack.Section, error)
		}); ok {
			sections, _ = reader.Sections(m.ctx)
		}
		return bootstrapMsg{conversations: conversations, users: users, sections: sections, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.mode == settingsMode {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.PasteMsg:
			if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m.updateSettings(msg)
		}
	}
	if m.mode == summaryMode {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc", "S":
				m.mode = normal
			case "j", "down":
				m.summaryViewport.ScrollDown(1)
			case "k", "up":
				m.summaryViewport.ScrollUp(1)
			case "ctrl+d":
				m.summaryViewport.HalfPageDown()
			case "ctrl+u":
				m.summaryViewport.HalfPageUp()
			}
			return m, nil
		}
	}
	switch msg := msg.(type) {
	case reactionsMsg:
		cmd := m.applyReactions(msg)
		return m, cmd
	case emojisMsg:
		m.emojis = msg.values
		m.renderHistory()
		return m, nil
	case searchMsg:
		if msg.generation != m.finderGeneration || m.mode != finder {
			return m, nil
		}
		m.searchLoading = false
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.searchHits = msg.page.Hits
		m.matches = nil
		m.matchSelected = 0
		for i := range m.searchHits {
			m.matches = append(m.matches, messageMatchBase-i)
		}
		m.searchQuery = m.query.Value()
		m.status = fmt.Sprintf("%d message matches · last 14 days", len(m.searchHits))
		if msg.page.Partial {
			m.status += " · first 100 results; refine your query"
		}
		return m, nil
	case parentHistoryMsg:
		if m.thread == nil || msg.channel != m.active || msg.generation != m.generation {
			return m, nil
		}
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		filtered := []slack.Message{}
		for _, message := range msg.history.Messages {
			if m.messageVersions[msg.channel+"/"+message.TS] <= msg.version {
				filtered = append(filtered, m.withReactionSnapshot(message, msg.channel, msg.version))
			}
		}
		m.thread.history = mergeMessages(m.thread.history, filtered)
		m.thread.cursor = msg.history.ResponseMetadata.NextCursor
		return m, nil
	case summaryMsg:
		delete(m.summaryBusy, msg.channel)
		if msg.err != nil {
			m.setError(msg.err)
		} else {
			m.summaries[msg.channel] = msg.summary
			if len(m.summaries) > 128 {
				oldest := ""
				for id, summary := range m.summaries {
					if oldest == "" || summary.Generated.Before(m.summaries[oldest].Generated) {
						oldest = id
					}
				}
				delete(m.summaries, oldest)
			}
			m.status = "Private summary updated · S expands"
			if err := m.SaveState(); err != nil {
				m.setError(err)
			}
		}
		m.resize()
		return m, nil
	case preferencesSaved:
		if m.settings != nil {
			m.settings.busy = false
		}
		if msg.err != nil {
			if m.settings != nil {
				m.settings.error = cleanLine(msg.err.Error())
			} else {
				m.setError(msg.err)
			}
			return m, nil
		}
		m.provider, m.theme, m.soundEnabled, m.recentDays = msg.values.AI, msg.values.Theme, msg.values.Sound, msg.values.Days
		m.recentOnly = m.recentDays > 0
		m.settings = nil
		m.mode = normal
		m.status = "Settings saved"
		m.resize()
		return m, nil
	case dmOpened:
		if msg.generation != m.finderGeneration {
			return m, nil
		}
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		// A result requested before Esc or another open cannot steal focus.
		if msg.generation != m.finderGeneration {
			return m, nil
		}
		m.conversations = append(m.conversations, msg.conversation)
		m.selected = len(m.conversations) - 1
		cmd := m.openSelected()
		m.revealSidebarSelection()
		return m, cmd

	case slack.LiveEvent:
		cmd := m.updateLive(msg)
		return m, cmd
	case typingTick:
		cmd := m.expireTyping(msg)
		return m, cmd
	case tea.WindowSizeMsg:
		bottom := m.viewport.AtBottom()
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		if bottom {
			m.viewport.GotoBottom()
		}
		return m, nil
	case bootstrapMsg:
		firstLoad := !m.ready
		m.booting = false
		if msg.conversations == nil && msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.ready = true
		m.sections = msg.sections
		m.directory = msg.users
		for _, user := range msg.users {
			m.users[user.ID] = cleanLine(user.DisplayName())
		}
		previous := m.active
		m.conversations = nil
		for _, c := range msg.conversations {
			if !c.IsArchived {
				m.conversations = append(m.conversations, c)
			}
		}
		slices.SortFunc(m.conversations, func(a, b slack.Conversation) int {
			if a.IsIM != b.IsIM {
				if a.IsIM {
					return 1
				}
				return -1
			}
			return strings.Compare(strings.ToLower(m.label(a)), strings.ToLower(m.label(b)))
		})
		m.selected = 0
		found := false
		for i, c := range m.conversations {
			if c.ID == previous {
				m.selected = i
				found = true
			}
		}
		if previous != "" && !found {
			m.drafts[previous] = m.input.Value()
			m.active = ""
			m.thread = nil
			m.selectedMessage = ""
			m.history = nil
			m.cursor = ""
			m.loading = false
			m.generation++
			m.input.Reset()
			m.viewport.SetContent("No conversations available.")
		}
		m.status = "Ready"
		if msg.err != nil {
			m.setError(msg.err)
		}
		if (firstLoad || previous != "") && len(m.conversations) > 0 && m.active == "" && m.destination == "" {
			cmd := m.openSelected()
			m.readEnabled = false // Startup/reload must not clear unreads without an explicit open.
			return m, tea.Batch(cmd, m.nextUnreadCount())
		}
		m.updateMatches()
		return m, m.nextUnreadCount()
	case threadsTick:
		if msg.generation != m.feedPollGeneration {
			return m, nil
		}
		if _, ok := m.api.(slack.ThreadsAPI); !ok {
			return m, nil
		}
		if m.feedLoading || time.Now().Before(m.retryUntil) {
			return m, m.threadTimer()
		}
		cmd := m.requestThreads("")
		return m, cmd
	case threadsMsg:
		cmd := m.updateThreads(msg)
		return m, cmd
	case unreadCountMsg:
		cmd := m.applyUnreadCount(msg)
		return m, cmd
	case countTick:
		cmd := m.nextUnreadCount()
		return m, cmd
	case readTick:
		cmd := m.flushRead()
		return m, cmd
	case markedMsg:
		cmd := m.applyMarked(msg)
		return m, cmd
	case activityTick:
		if time.Now().Before(m.retryUntil) {
			return m, m.activityTimer()
		}
		return m, tea.Batch(m.activityCmd(), m.nextReaction())
	case recentTick:
		if time.Now().Before(m.retryUntil) {
			return m, m.recentTimer()
		}
		return m, m.recentCmd()
	case activityMsg:
		cmd := m.updateActivity(msg)
		return m, cmd
	case recentMsg:
		if msg.err != nil {
			m.setError(msg.err)
			m.sentReady = false
			m.status = "Recent filter unavailable; showing all until retry: " + m.status
		} else {
			for id, at := range msg.sent {
				if at.After(m.interactions[id]) {
					m.interactions[id] = at
				}
			}
			m.sentReady = true
			m.pruneInteractions()
		}
		return m, m.recentTimer()
	case soundDoneMsg:
		if msg.fallback {
			return m, tea.Raw("\a")
		}
		return m, nil
	case historyMsg:
		// A slow request from a previously selected channel must not replace this one.
		if msg.channel != m.active || msg.generation != m.generation {
			return m, nil
		}
		m.loading = false
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		current := msg.history.Messages[:0]
		for _, message := range msg.history.Messages {
			if m.messageVersions[msg.channel+"/"+message.TS] <= msg.version {
				current = append(current, m.withReactionSnapshot(message, msg.channel, msg.version))
			}
		}
		msg.history.Messages = current
		bottom, oldOffset, oldLines := m.viewport.AtBottom(), m.viewport.YOffset(), m.viewport.TotalLineCount()
		if msg.cursor == "" {
			m.history = mergeMessages(m.history, msg.history.Messages)
		} else {
			m.history = mergeMessages(msg.history.Messages, m.history)
		}
		m.cursor = msg.history.ResponseMetadata.NextCursor
		readCmd := m.readLoadedHistory()
		m.renderHistory()
		if msg.cursor != "" {
			m.viewport.SetYOffset(oldOffset + m.viewport.TotalLineCount() - oldLines)
		} else if bottom && m.thread == nil {
			m.viewport.GotoBottom()
		}
		if m.revealMessage != "" {
			for i, message := range m.history {
				if message.TS == m.revealMessage {
					m.selectBlock(i)
					break
				}
			}
			m.revealMessage = ""
		}
		m.recordInteraction(msg.channel)
		m.status = fmt.Sprintf("%d messages loaded", len(m.history))
		return m, readCmd
	case sentMsg:
		m.sending = false
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.recordInteraction(msg.channel)
		if slack.TimestampTime(msg.message.TS).After(slack.TimestampTime(m.ownLatest[msg.channel])) {
			m.ownLatest[msg.channel] = msg.message.TS
		}
		key := msg.draftKey
		if key == "" {
			key = msg.channel
		}
		if m.drafts[key] == msg.text {
			m.drafts[key] = ""
		}
		if msg.channel == m.active && msg.thread == m.threadTS() {
			if m.input.Value() == msg.text {
				m.input.Reset()
			}
			if msg.message.User == "" {
				msg.message.User = m.session.UserID
			}
			m.history = mergeMessages(m.history, []slack.Message{msg.message})
			m.selectedMessage = msg.message.TS
			if m.thread != nil {
				updateReplyParent(m.thread.history, m.thread.ts, msg.message.TS)
			}
			m.renderHistory()
			m.viewport.GotoBottom()
		}
		if msg.message.User == "" {
			msg.message.User = m.session.UserID
		}
		msg.message.Channel = msg.channel
		if msg.thread != "" {
			msg.message.ThreadTS = msg.thread
		}
		cmd := m.updateLive(slack.LiveEvent{Type: "message", Message: msg.message})
		m.status = "Message sent"
		return m, cmd
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.mode == finder {
			return m.updateFinder(msg)
		}
		if m.mode == insert {
			switch msg.String() {
			case "esc":
				m.closeRightmost()
				return m, nil
			case "enter":
				return m, m.send()
			}
			if !m.sending {
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m.drafts[m.draftKey()] = m.input.Value()
				return m, cmd
			}
			return m, nil
		}
		return m.updateNormal(msg)
	case tea.PasteMsg:
		// Pasting never executes navigation commands or sends a message.
		if m.mode == insert && !m.sending {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.drafts[m.draftKey()] = m.input.Value()
			return m, cmd
		}
		if m.mode == finder {
			var cmd tea.Cmd
			m.query, cmd = m.query.Update(msg)
			m.updateMatches()
			return m, cmd
		}
	}
	var cmd tea.Cmd
	if m.mode == insert {
		m.input, cmd = m.input.Update(msg)
	}
	if m.mode == finder {
		m.query, cmd = m.query.Update(msg)
	}
	return m, cmd
}

func (m *Model) setError(err error) {
	m.status = cleanLine(err.Error())
	var limit *slack.RateLimitError
	if errors.As(err, &limit) {
		m.retryUntil = time.Now().Add(limit.RetryAfter)
	}
}
func (m *Model) canRequest() bool {
	if remaining := time.Until(m.retryUntil); remaining > 0 {
		m.status = fmt.Sprintf("Rate limited; retry in %s", remaining.Round(time.Second))
		return false
	}
	return true
}

func (m *Model) updateNormal(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := key.String()
	if m.focus == parentPane && m.thread != nil {
		switch k {
		case "j", "down", "k", "up", "G", "g", "ctrl+d", "ctrl+u", "enter", "r":
			return m.navigateParent(key)
		}
	}
	doubleG := m.pendingG && k == "g"
	m.pendingG = false
	switch k {
	case "q":
		return *m, tea.Quit
	case "esc":
		m.closeRightmost()
	case "f":
		if m.recentDays <= 0 {
			m.recentDays = 14
		}
		m.recentOnly = !m.recentOnly
		m.sidebarKey = ""
		m.status = "Sidebar: " + m.filterLabel()
	case "m":
		m.soundEnabled = !m.soundEnabled
		if m.soundEnabled {
			m.status = "Notification sounds on"
		} else {
			m.status = "Notification sounds muted"
		}
	case "backspace":
		if m.thread != nil {
			m.closeThread()
		}
	case " ", "space":
		if m.focus == sidebar {
			m.toggleSection()
		}
	case "tab":
		m.cyclePane(1)
	case "shift+tab":
		m.cyclePane(-1)
	case "/", "ctrl+p", "super+p", "cmd+p":
		m.finderGeneration++
		m.mode = finder
		m.query.Reset()
		m.matchSelected = 0
		m.updateMatches()
		return *m, m.query.Focus()
	case "i", "a", "A", "I":
		if m.active == "" || m.sending {
			return *m, nil
		}
		m.focus = composer
		m.mode = insert
		if k == "A" {
			m.input.CursorEnd()
		}
		if k == "I" {
			m.input.CursorStart()
		}
		if k == "a" {
			m.input.SetCursor(m.input.Position() + 1)
		}
		m.renderHistory()
		return *m, m.input.Focus()
	case ",", "super+,", "cmd+,":
		return *m, m.openSettings()
	case "s":
		cmd := m.summarizeConversation()
		return *m, cmd
	case "S":
		m.expandSummary()
		return *m, nil
	case "r":
		if m.focus == messages || m.focus == parentPane {
			var cmd tea.Cmd
			if m.focus == parentPane && m.thread != nil {
				m.closeThread()
			}
			if m.destination == threadsDestination {
				cmd = m.openFeedThread()
			} else if m.thread == nil {
				cmd = m.openThread()
			}
			if m.thread != nil {
				m.focus = composer
				m.mode = insert
				return *m, tea.Batch(cmd, m.input.Focus())
			}
		}
	case "R":
		if m.destination == threadsDestination {
			return *m, m.requestThreads("")
		}
		if m.active != "" {
			return *m, tea.Batch(m.fetchHistory(""), m.fetchParentHistory())
		}
		if !m.booting && m.canRequest() {
			m.booting = true
			return *m, m.bootstrap()
		}
	case "L":
		if m.destination == threadsDestination {
			if m.feedNext != "" {
				cmd := m.requestThreads(m.feedNext)
				return *m, cmd
			}
			return *m, nil
		}
		if m.cursor != "" {
			return *m, m.fetchHistory(m.cursor)
		}
		m.status = "No older page available"
	case "h", "left":
		if m.thread != nil && m.focus == messages {
			m.focus = parentPane
		} else if m.focus == composer {
			m.input.SetCursor(m.input.Position() - 1)
		} else {
			m.focus = sidebar
		}
	case "l", "right":
		if m.thread != nil && m.focus == sidebar {
			m.focus = parentPane
		} else if m.focus == composer {
			m.input.SetCursor(m.input.Position() + 1)
		} else {
			m.focus = messages
		}
	case "j", "down":
		if m.focus == sidebar {
			m.moveConversation(1)
		} else {
			m.moveBlock(1)
		}
	case "k", "up":
		if m.focus == sidebar {
			m.moveConversation(-1)
		} else {
			m.moveBlock(-1)
		}
	case "g":
		if doubleG {
			if m.focus == sidebar {
				m.selectSidebar(0)
			} else if m.destination == threadsDestination {
				m.selectFeed(0)
			} else {
				m.selectBlock(0)
			}
		} else {
			m.pendingG = true
		}
	case "G":
		if m.focus == sidebar {
			m.selectSidebar(len(m.sidebarRows()) - 1)
		} else if m.destination == threadsDestination {
			m.selectFeed(len(m.feed) - 1)
		} else {
			m.selectBlock(len(m.history) - 1)
		}
	case "ctrl+d":
		m.viewport.HalfPageDown()
	case "ctrl+u":
		m.viewport.HalfPageUp()
	case "0":
		if m.focus == composer {
			m.input.CursorStart()
		}
	case "$":
		if m.focus == composer {
			m.input.CursorEnd()
		}
	case "w", "b":
		if m.focus == composer {
			m.moveWord(k == "w")
		}
	case "x":
		if m.focus == composer && !m.sending {
			value := []rune(m.input.Value())
			pos := m.input.Position()
			if pos < len(value) {
				m.input.SetValue(string(append(value[:pos], value[pos+1:]...)))
				m.input.SetCursor(pos)
				m.drafts[m.draftKey()] = m.input.Value()
			}
		}
	case "enter":
		if m.focus == sidebar {
			if row, ok := m.sidebarSelection(); ok && row.destination != "" {
				cmd := m.openDestination(row.destination)
				return *m, cmd
			}
			if row, ok := m.sidebarSelection(); ok && row.index < 0 {
				m.toggleSection()
				return *m, nil
			}
			row, ok := m.sidebarSelection()
			if !ok {
				return *m, nil
			}
			m.selected = row.index
			cmd := m.openSelected()
			return *m, cmd
		}
		if m.focus == messages && m.destination == threadsDestination {
			cmd := m.openFeedThread()
			return *m, cmd
		}
		if m.focus == messages && m.thread == nil {
			cmd := m.openThread()
			return *m, cmd
		}
	}
	m.renderHistory()
	return *m, nil
}

func (m Model) messagePaneLabel() string {
	if m.destination != "" {
		return m.destinationTitle()
	}
	if m.active == "" {
		return "MESSAGES"
	}
	if m.thread != nil {
		return "REPLIES · " + m.activeLabel()
	}
	return "MESSAGES · " + m.activeLabel()
}

func (m *Model) moveWord(forward bool) {
	value := []rune(m.input.Value())
	pos := m.input.Position()
	if forward {
		for pos < len(value) && !unicode.IsSpace(value[pos]) {
			pos++
		}
		for pos < len(value) && unicode.IsSpace(value[pos]) {
			pos++
		}
	} else {
		pos = max(0, pos-1)
		for pos > 0 && unicode.IsSpace(value[pos]) {
			pos--
		}
		for pos > 0 && !unicode.IsSpace(value[pos-1]) {
			pos--
		}
	}
	m.input.SetCursor(pos)
}

func (m *Model) openSelected() tea.Cmd {
	if len(m.conversations) == 0 || !m.canRequest() {
		return nil
	}
	c := m.conversations[m.selected]
	m.finderGeneration++
	m.revealMessage = ""
	m.destination = ""
	m.readEnabled = true
	m.newMessages = map[string]bool{}
	m.recordInteraction(c.ID)
	m.sidebarKey = "conversation:" + c.ID
	if c.ID == m.active && !(m.thread != nil && m.thread.fromFeed) {
		if m.thread != nil {
			m.closeThread()
		}
		m.focus = messages
		cmd := m.readLoadedHistory()
		m.renderHistory()
		return cmd
	}
	if m.active != "" {
		m.drafts[m.draftKey()] = m.input.Value()
	}
	m.thread = nil
	m.selectedMessage = ""
	m.active = c.ID
	m.focus = messages
	m.input.SetValue(m.drafts[c.ID])
	m.input.CursorEnd()
	m.history = nil
	m.cursor = ""
	m.loading = false
	m.generation++
	m.resize()
	m.viewport.SetContent("Loading messages…")
	return m.fetchHistory("")
}

func (m *Model) fetchHistory(cursor string) tea.Cmd {
	if m.active == "" || m.loading || !m.canRequest() {
		return nil
	}
	m.loading = true
	m.status = "Loading messages…"
	channel, generation, api, ctx := m.active, m.generation, m.api, m.ctx
	thread := m.threadTS()
	version := m.eventVersion
	return func() tea.Msg {
		var history slack.History
		var err error
		if thread != "" {
			history, err = api.Replies(ctx, channel, thread, cursor)
		} else {
			history, err = api.History(ctx, channel, cursor)
		}
		return historyMsg{channel: channel, cursor: cursor, generation: generation, history: history, err: err, version: version}
	}
}

func (m *Model) send() tea.Cmd {
	text := m.input.Value()
	if m.destination != "" || m.active == "" || m.sending || strings.TrimSpace(text) == "" || !m.canRequest() {
		return nil
	}
	m.sending = true
	m.status = "Sending…"
	m.drafts[m.draftKey()] = text
	channel, api, ctx := m.active, m.api, m.ctx
	thread, key := m.threadTS(), m.draftKey()
	return func() tea.Msg {
		var message slack.Message
		var err error
		if thread != "" {
			message, err = api.SendReply(ctx, channel, thread, text)
		} else {
			message, err = api.Send(ctx, channel, text)
		}
		return sentMsg{channel: channel, thread: thread, draftKey: key, text: text, message: message, err: err}
	}
}

func (m *Model) updateMatches() {
	m.finderGeneration++
	m.searchQuery = ""
	if strings.HasPrefix(m.query.Value(), "?") {
		if m.query.Value() != m.searchQuery {
			m.finderGeneration++
			m.searchLoading = false
			m.searchHits = nil
			m.matches = nil
		}
		return
	}
	m.searchLoading = false
	m.searchHits = nil

	labels := make([]string, len(m.conversations))
	ids := make([]int, len(m.conversations))
	existing := map[string]bool{}
	for i, c := range m.conversations {
		labels[i] = m.label(c) + " " + c.Name
		if c.IsIM {
			for _, user := range m.directory {
				if user.ID == c.User {
					labels[i] += " " + cleanLine(user.Name) + " " + cleanLine(user.RealName) + " " + cleanLine(user.Profile.RealName)
					break
				}
			}
		}
		ids[i] = i
		if c.IsIM {
			existing[c.User] = true
		}
	}
	m.finderUsers = nil
	for _, user := range m.directory {
		if user.Deleted || user.IsBot || existing[user.ID] || user.ID == "USLACKBOT" {
			continue
		}
		m.finderUsers = append(m.finderUsers, user.ID)
		ids = append(ids, -len(m.finderUsers))
		labels = append(labels, "@"+cleanLine(user.DisplayName())+" "+cleanLine(user.Name)+" "+cleanLine(user.RealName))
	}
	m.matches = nil
	if m.query.Value() == "" {
		m.matches = ids
	} else {
		for _, match := range fuzzy.Find(m.query.Value(), labels) {
			m.matches = append(m.matches, ids[match.Index])
		}
	}
	m.matchSelected = min(m.matchSelected, max(0, len(m.matches)-1))
}
func (m *Model) updateFinder(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.finderGeneration++
		m.mode = normal
		m.query.Blur()
		return *m, nil
	case "ctrl+j", "ctrl+n", "down":
		m.matchSelected = min(m.matchSelected+1, max(0, len(m.matches)-1))
		return *m, nil
	case "ctrl+k", "ctrl+p", "up":
		m.matchSelected = max(0, m.matchSelected-1)
		return *m, nil
	case "enter":
		if strings.HasPrefix(m.query.Value(), "?") && m.query.Value() != m.searchQuery {
			return *m, m.searchMessages()
		}
		if len(m.matches) > 0 {
			index := m.matches[m.matchSelected]
			if index <= messageMatchBase {
				return *m, m.openSearchHit(m.searchHits[messageMatchBase-index])
			}
			if index < 0 {
				return *m, m.openFinderUser(m.finderUsers[-index-1])
			}
			m.selected = index
			m.mode = normal
			m.query.Blur()
			cmd := m.openSelected()
			if m.active == m.conversations[m.selected].ID {
				m.revealSidebarSelection()
			}
			return *m, cmd
		}
		return *m, nil
	}
	var cmd tea.Cmd
	m.query, cmd = m.query.Update(key)
	m.matchSelected = 0
	m.updateMatches()
	return *m, cmd
}

func (m *Model) resize() {
	w := max(1, m.width-m.sidebarWidth()-5)
	if m.thread != nil {
		w = max(1, (m.width-m.sidebarWidth()-2)/2-4)
	}
	m.viewport.SetWidth(w)
	m.viewport.SetHeight(max(1, m.mainHeight()-3-m.summaryHeight()))
	m.summaryViewport.SetWidth(max(1, m.width-4))
	m.summaryViewport.SetHeight(max(1, m.mainHeight()-3))
	m.input.SetWidth(max(1, m.width-6))
	m.query.SetWidth(max(1, m.width-8))
	m.renderHistory()
}
func (m Model) mainHeight() int { return max(4, m.height-8) }

func (m Model) sidebarWidth() int { return min(40, max(20, m.width/3)) }
func (m Model) label(c slack.Conversation) string {
	if c.IsIM {
		name := m.users[c.User]
		if name == "" {
			name = c.User
		}
		return "@" + cleanLine(name)
	}
	if c.IsMPIM {
		name := strings.TrimPrefix(c.Name, "mpdm-")
		name = regexp.MustCompile(`-[0-9]+$`).ReplaceAllString(name, "")
		return "@" + cleanLine(strings.ReplaceAll(name, "--", ", "))
	}
	if c.IsPrivate {
		return "🔒 " + cleanLine(c.Name)
	}
	return "#" + cleanLine(c.Name)
}
func (m Model) activeLabel() string {
	for _, c := range m.conversations {
		if c.ID == m.active {
			return m.label(c)
		}
	}
	return "Conversations"
}

func (m *Model) renderHistory() {
	if m.destination != "" {
		m.renderDestination()
		return
	}
	if len(m.history) == 0 {
		m.blockStarts = nil
		empty := "No messages in this conversation."
		if m.active == "" {
			empty = ""
		}
		m.viewport.SetContent(empty)
		return
	}
	found := false
	for _, message := range m.history {
		if message.TS == m.selectedMessage {
			found = true
			break
		}
	}
	if !found {
		m.selectedMessage = m.history[len(m.history)-1].TS
	}
	blocks := make([]string, 0, len(m.history))
	m.blockStarts = map[string]int{}
	line := 0
	for _, message := range m.history {
		block := m.messageBlock(message, m.viewport.Width())
		m.blockStarts[message.TS] = line
		line += lipgloss.Height(block) + 1
		blocks = append(blocks, block)
	}
	m.viewport.SetContent(strings.Join(blocks, "\n\n"))
}

func (m Model) threadTS() string {
	if m.thread != nil {
		return m.thread.ts
	}
	return ""
}
func (m Model) draftKey() string {
	if m.thread != nil {
		return m.active + "/" + m.thread.ts
	}
	return m.active
}
func (m *Model) moveBlock(delta int) {
	if m.destination == threadsDestination {
		m.moveFeed(delta)
		return
	}
	for i, message := range m.history {
		if message.TS == m.selectedMessage {
			m.selectBlock(i + delta)
			return
		}
	}
	m.selectBlock(len(m.history) - 1)
}
func (m *Model) selectBlock(index int) {
	if len(m.history) == 0 {
		return
	}
	index = max(0, min(index, len(m.history)-1))
	m.selectedMessage = m.history[index].TS
	m.renderHistory()
	start := m.blockStarts[m.selectedMessage]
	height := lipgloss.Height(m.messageBlock(m.history[index], m.viewport.Width()))
	if start < m.viewport.YOffset() || height > m.viewport.Height() {
		m.viewport.SetYOffset(start)
	} else if start+height > m.viewport.YOffset()+m.viewport.Height() {
		m.viewport.SetYOffset(start + height - m.viewport.Height())
	}
}
func (m *Model) openThread() tea.Cmd {
	if m.thread != nil || !m.canRequest() {
		return nil
	}
	for _, message := range m.history {
		if message.TS != m.selectedMessage {
			continue
		}
		m.drafts[m.draftKey()] = m.input.Value()
		ts := message.TS
		if message.ThreadTS != "" {
			ts = message.ThreadTS
		}
		m.readEnabled = true
		m.thread = &threadState{ts: ts, history: m.history, cursor: m.cursor, selection: m.selectedMessage, offset: m.viewport.YOffset()}
		m.remoteRead[m.active+"/"+ts] = laterTS(m.remoteRead[m.active+"/"+ts], message.LastRead)
		for _, thread := range m.feed {
			if thread.Root.Channel == m.active && thread.Root.TS == ts {
				m.remoteRead[thread.Key()] = laterTS(m.remoteRead[thread.Key()], thread.Root.LastRead)
			}
		}
		m.newMessages = map[string]bool{}
		m.generation++
		m.loading = false
		m.history = []slack.Message{message}
		m.cursor = ""
		m.input.SetValue(m.drafts[m.draftKey()])
		m.input.CursorEnd()
		m.focus = messages
		m.resize()
		m.viewport.SetYOffset(0)
		return m.fetchHistory("")
	}
	return nil
}
func (m *Model) closeThread() {
	if m.thread == nil {
		return
	}
	m.drafts[m.draftKey()] = m.input.Value()
	state := m.thread
	if state.fromFeed {
		m.thread = nil
		m.active = ""
		m.destination = threadsDestination
		m.history = nil
		m.cursor = ""
		m.loading = false
		m.generation++
		m.newMessages = map[string]bool{}
		m.input.Reset()
		m.mode = normal
		m.input.Blur()
		m.focus = messages
		m.resize()
		m.status = "Back to threads"
		return
	}
	m.thread = nil
	m.newMessages = map[string]bool{}
	m.generation++
	m.loading = false
	m.history = state.history
	m.cursor = state.cursor
	m.selectedMessage = state.selection
	m.input.SetValue(m.drafts[m.draftKey()])
	m.input.CursorEnd()
	m.mode = normal
	m.input.Blur()
	m.focus = messages
	m.resize()
	m.viewport.SetYOffset(state.offset)
	m.status = "Back to channel"
}

// Keep the full block, including its frame, inside the message pane. The body
// wraps here so viewport scrolling cannot split a border into another line.
func (m Model) messageBlock(message slack.Message, width int) string {
	width = max(10, width)
	inner := width - 4 // two border columns and one space of padding on each side
	name := m.users[message.User]
	if name == "" {
		name = message.Username
	}
	if name == "" {
		name = message.User
	}
	if name == "" {
		name = "Slack"
	}
	name = cleanLine(name)
	unread := m.messageUnread(message)
	if unread {
		name = "● NEW · " + name
	}
	selected := m.focus == messages && message.TS == m.selectedMessage
	if selected {
		name = "▶ " + name
	}
	timestamp := ""
	ts, _, _ := strings.Cut(message.TS, ".")
	if seconds, err := strconv.ParseInt(ts, 10, 64); err == nil {
		timestamp = time.Unix(seconds, 0).Local().Format("Jan 02 15:04")
	}
	// At narrow widths put time below the author rather than clipping either.
	header := lipgloss.NewStyle().Bold(true).Foreground(m.accent()).Render(ansi.Truncate(name, inner, "…"))
	if timestamp != "" {
		timeStyle := lipgloss.NewStyle().Foreground(m.muted())
		if lipgloss.Width(header)+len(timestamp)+2 <= inner {
			header += strings.Repeat(" ", inner-lipgloss.Width(header)-len(timestamp)) + timeStyle.Render(timestamp)
		} else {
			header += "\n" + timeStyle.Render(ansi.Truncate(timestamp, inner, "…"))
		}
	}
	body := strings.ReplaceAll(m.messageText(message.Text), "\t", "    ")
	body = ansi.Hardwrap(body, inner, true)
	content := header + "\n\n" + body
	if message.ReplyCount > 0 {
		word := "replies"
		if message.ReplyCount == 1 {
			word = "reply"
		}
		replies := fmt.Sprintf("↳ %d %s", message.ReplyCount, word)
		content += "\n\n" + lipgloss.NewStyle().Foreground(m.muted()).Render(ansi.Hardwrap(replies, inner, true))
	}
	if badges := m.reactionBadges(message.Reactions, inner); badges != "" {
		content += "\n\n" + badges
	}
	border := lipgloss.Color("#46505E")
	if unread {
		border = lipgloss.Color("#F4B860")
	}
	if selected {
		border = m.accent()
	}
	style := lipgloss.NewStyle().Width(width).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(border)
	if unread {
		style = style.Background(lipgloss.Color("#211C14"))
	}
	return style.Render(content)
}

var mention = regexp.MustCompile(`<@([A-Z0-9]+)>`)

func (m Model) messageText(text string) string {
	text = mention.ReplaceAllStringFunc(text, func(value string) string {
		id := value[2 : len(value)-1]
		if name := m.users[id]; name != "" {
			return "@" + name
		}
		return "@" + id
	})
	return m.emojify(clean(html.UnescapeString(text)))
}

// Strip terminal control sequences before rendering workspace-controlled content.
var escapeSequence = regexp.MustCompile("\x1b(?:\\[[0-?]*[ -/]*[@-~]|\\][^\x07\x1b]*(?:\x07|\x1b\\\\)|[@-_])")

func cleanLine(value string) string {
	return strings.Join(strings.Fields(clean(value)), " ")
}

func clean(value string) string {
	value = escapeSequence.ReplaceAllString(value, "")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}

func mergeMessages(a, b []slack.Message) []slack.Message {
	seen := make(map[string]slack.Message, len(a)+len(b))
	for _, message := range a {
		if message.TS != "" {
			seen[message.TS] = message
		}
	}
	for _, message := range b {
		if message.TS != "" {
			seen[message.TS] = message
		}
	}
	result := make([]slack.Message, 0, len(seen))
	for _, message := range seen {
		result = append(result, message)
	}
	// Slack timestamps have fixed-width seconds and fractional parts; compare
	// their integer components rather than converting to lossy float64 values.
	sort.Slice(result, func(i, j int) bool {
		is, ifrac, _ := strings.Cut(result[i].TS, ".")
		js, jfrac, _ := strings.Cut(result[j].TS, ".")
		if len(is) != len(js) {
			return len(is) < len(js)
		}
		if is != js {
			return is < js
		}
		return ifrac < jfrac
	})
	return result
}

func (m Model) View() tea.View {
	if m.width < 60 || m.height < 12 {
		v := tea.NewView("Please resize the terminal to at least 60 columns × 12 rows. Ctrl+C quits.")
		v.AltScreen = true
		return v
	}
	accent := m.accent()
	muted := m.muted()
	title := lipgloss.NewStyle().Bold(true).Foreground(accent)
	header := "slackline  ·  " + cleanLine(m.session.Team) + "  ·  , Settings"
	if m.demo {
		header += "  ·  OFFLINE DEMO"
	}
	var content string
	if m.mode == settingsMode {
		content = m.settingsView()
	} else if m.mode == summaryMode {
		content = m.paneFrame("PRIVATE SUMMARY · Esc closes", m.summaryViewport.View(), m.width, m.mainHeight(), true)
	} else if m.mode == finder {
		lines := []string{m.query.View(), "Names/users · ? text then Enter searches messages (14 days)"}
		count := max(1, m.mainHeight()-5)
		start := max(0, m.matchSelected-count+1)
		for i := start; i < min(len(m.matches), start+count); i++ {
			label := m.finderLabel(m.matches[i])
			if i == m.matchSelected {
				label = title.Render("› " + label)
			} else {
				label = "  " + label
			}
			lines = append(lines, label)
		}
		if len(m.matches) == 0 {
			empty := "No matching conversations or users"
			if strings.HasPrefix(m.query.Value(), "?") {
				empty = "Enter searches messages from the last 14 days"
			}
			if m.searchLoading {
				empty = "Searching…"
			}
			lines = append(lines, empty)
		}
		content = m.paneFrame("FIND CONVERSATION", strings.Join(lines, "\n"), m.width, m.mainHeight(), true)
	} else {
		sw := m.sidebarWidth()
		rows := m.sidebarRows()
		cursor := m.sidebarPosition(rows)
		count := max(1, m.viewport.Height())
		shown := []int{}
		pinned := min(pinnedRows, len(rows), count)
		if count <= pinnedRows {
			pinned = 0
		}
		for i := 0; i < pinned; i++ {
			shown = append(shown, i)
		}
		remaining := count - pinned
		start := max(pinned, cursor-remaining+1)
		for i := start; i < min(len(rows), start+remaining); i++ {
			shown = append(shown, i)
		}
		lines := []string{}
		for _, i := range shown {
			row := rows[i]
			label := row.label
			badge := ""
			unread := row.group == "local-unread"
			if row.index >= 0 {
				id := m.conversations[row.index].ID
				prefix := "  "
				if id == m.active {
					prefix = "• "
				}
				badge = m.unreadBadge(id)
				if badge != "" {
					prefix = "● "
					unread = true
					badge = " [" + badge + "]"
				}
				label = prefix + label
			} else if row.destination == threadsDestination && m.feedUnread > 0 {
				badge = fmt.Sprintf(" [%d]", m.feedUnread)
				unread = true
			}
			if row.index >= 0 && m.channelTyping(m.conversations[row.index].ID) {
				badge = " [typing]" + badge
			}
			label = ansi.Truncate(label, max(1, sw-4-lipgloss.Width(badge)), "…") + badge
			if row.destination != "" && (row.destination == m.destination || (row.destination == threadsDestination && m.thread != nil && m.thread.fromFeed)) {
				label = "• " + strings.TrimPrefix(strings.TrimPrefix(label, "✦ "), "↳ ")
			}
			if i == cursor && m.focus == sidebar {
				label = lipgloss.NewStyle().Width(sw - 4).Bold(true).Foreground(lipgloss.Color("#101418")).Background(accent).Render(ansi.Truncate(label, sw-4, "…"))
			} else if unread {
				label = lipgloss.NewStyle().Width(sw - 4).Bold(true).Foreground(lipgloss.Color("#F4B860")).Background(lipgloss.Color("#211C14")).Render(label)
			} else if row.index < 0 {
				label = lipgloss.NewStyle().Bold(true).Foreground(muted).Render(label)
			}
			lines = append(lines, label)
		}
		if len(rows) == pinnedRows && count > pinnedRows {
			lines = append(lines, "No recent conversations")
		}
		side := m.paneFrame(m.sidebarTitle(), strings.Join(lines, "\n"), sw, m.mainHeight(), m.focus == sidebar)
		messagePane := m.conversationPanes(m.width - sw - 1)
		content = lipgloss.JoinHorizontal(lipgloss.Top, side, " ", messagePane)
	}
	modeLabel := "NORMAL"
	if m.mode == insert {
		modeLabel = "INSERT"
	}
	if m.mode == finder {
		modeLabel = "FIND"
	}
	focusLabel := []string{"channels", "replies/messages", "composer", "conversation"}[m.focus]
	footer := fmt.Sprintf("%s · %s · %s", modeLabel, focusLabel, m.status)
	soundLabel := "sound on"
	if !m.soundEnabled {
		soundLabel = "muted"
	}
	liveLabel := "polling"
	if m.liveState == "live" {
		liveLabel = "LIVE"
	} else if m.liveState == "connecting" || m.liveState == "reconnecting" {
		liveLabel = m.liveState + " · polling"
	}
	footer = liveLabel + " · " + footer
	footer += " · " + m.filterLabel() + " · " + soundLabel
	if m.stateWarning != "" {
		footer += " · " + m.stateWarning
	}
	help := "j/k blocks · Enter thread · r reply · R reload · s summary · S expand · , settings · / find"
	if m.focus == sidebar {
		help = "j/k select · Enter open/fold · Space fold · f recent/all · m mute · , settings · / find"
	}
	if m.mode == insert {
		help = "Enter sends · Esc closes rightmost pane · Ctrl+C quits"
	}
	if m.mode == settingsMode {
		help = "j/k fields · Enter edit/toggle · Ctrl+S save · Esc cancel"
	}
	if m.mode == summaryMode {
		help = "j/k scroll · Ctrl+u/d page · Esc closes private summary"
	}
	if m.mode == finder {
		help = "Type to filter · Ctrl+j/k select · Enter opens · Esc closes"
	}
	composerLabel, composerBody := "COMPOSER · "+modeLabel, m.input.View()
	if m.destination == aiAssistant || m.destination == aiSummaries {
		composerLabel = "AI DISABLED"
		composerBody = "Workspace assistant/daily digests are not available yet. Use s inside a conversation for a private summary."
	}
	if m.destination == threadsDestination {
		composerLabel = "THREADS"
		composerBody = "Select a thread and press Enter to read or reply."
	}
	if m.destination == "" {
		if typing := m.typingText(m.active, m.threadTS()); typing != "" {
			composerLabel = typing + " · " + composerLabel
		}
	}
	composerPane := m.paneFrame(composerLabel, composerBody, m.width, 4, m.focus == composer && m.mode != finder)
	content = title.Render(header) + "\n\n" + content + "\n" + composerPane + "\n" + lipgloss.NewStyle().Foreground(muted).MaxWidth(m.width).Render(footer) + "\n" + lipgloss.NewStyle().Foreground(muted).MaxWidth(m.width).Render(help)
	view := tea.NewView(lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(content))
	view.AltScreen = true
	return view
}

// Focus is shown in the border, the heading, and the selected channel row.
func (m Model) paneFrame(label, body string, width, height int, active bool) string {
	inner := max(1, width-4)
	color := lipgloss.Color("#46505E")
	heading := ansi.Truncate(label, inner, "…")
	if active {
		color = m.accent()
		badge := " · SELECTED"
		if inner < lipgloss.Width(label)+2+lipgloss.Width(badge) {
			badge = ""
		}
		heading = "▶ " + ansi.Truncate(label, max(1, inner-2-lipgloss.Width(badge)), "…") + badge
	}
	heading = lipgloss.NewStyle().Bold(true).Foreground(color).Render(heading)
	return lipgloss.NewStyle().Width(width).Height(height).MaxWidth(width).MaxHeight(height).
		Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(color).Render(heading + "\n" + body)
}

func (m *Model) leaveConversation() {
	m.destination = ""
	m.readEnabled = false
	m.newMessages = map[string]bool{}
	if m.active != "" {
		m.drafts[m.draftKey()] = m.input.Value()
	}
	m.active = ""
	m.thread = nil
	m.history = nil
	m.selectedMessage = ""
	m.cursor = ""
	m.loading = false
	m.generation++
	m.mode = normal
	m.focus = sidebar
	m.input.Reset()
	m.input.Blur()
	m.finderGeneration++
	m.revealMessage = ""
	m.status = "Select a conversation"
	m.resize()
}
