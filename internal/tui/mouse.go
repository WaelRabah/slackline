package tui

import tea "charm.land/bubbletea/v2"

// sidebarBodyTop is the screen row of the first sidebar entry: header, blank
// line, frame top, then the "CHANNELS" title. The message pane body starts on
// the same row.
const sidebarBodyTop = 4

// mouseClick opens the sidebar entry under a left click. In the message pane it
// selects the block under the pointer, and from Latest, Unreads and Threads it
// opens that message's conversation.
func (m *Model) mouseClick(msg tea.MouseClickMsg) tea.Cmd {
	mouse := msg.Mouse()
	if mouse.Button != tea.MouseLeft || m.mode != normal || m.width < 60 || m.height < 12 {
		return nil
	}
	if mouse.X > m.sidebarWidth() {
		return m.messageClick(mouse)
	}
	if mouse.X < 0 || mouse.X >= m.sidebarWidth() {
		return nil
	}
	rows := m.sidebarRows()
	if len(rows) == 0 {
		return nil
	}
	count := max(1, m.viewport.Height())
	shown := m.sidebarShown(rows, m.sidebarPosition(rows), count)
	k := mouse.Y - sidebarBodyTop
	if k < 0 || k >= len(shown) {
		return nil
	}
	m.focus = sidebar
	m.selectSidebar(shown[k])
	return m.activateSidebar()
}

// messageClick maps a click in the message pane to the block under it. The
// two-pane thread layout is keyboard only.
func (m *Model) messageClick(mouse tea.Mouse) tea.Cmd {
	row := mouse.Y - sidebarBodyTop
	if m.thread != nil || row < 0 || row >= m.viewport.Height() {
		return nil
	}
	line := row + m.viewport.YOffset()
	switch m.destination {
	case latestDestination:
		// Header takes two lines; every entry then takes a blank line plus three.
		if line < 2 || (line-2)/4 >= len(m.recent) {
			return nil
		}
		m.focus = messages
		m.latestIndex = (line - 2) / 4
		return m.openLatest()
	case unreadsDestination:
		_, _, spans := m.unreadsBlocks()
		for i, span := range spans {
			if line >= span[0] && line <= span[1] {
				m.focus = messages
				m.unreadIndex = i
				return m.openUnreadBlock(m.unreadItems())
			}
		}
	case threadsDestination:
		for _, thread := range m.feed {
			key := thread.Key()
			if start, ok := m.blockStarts[key]; ok && line >= start && line <= m.blockEnds[key] {
				m.focus = messages
				m.feedSelection = key
				return m.openFeedThread()
			}
		}
	case "":
		for i, message := range m.history {
			if start, ok := m.blockStarts[message.TS]; ok && line >= start && line <= m.blockEnds[message.TS] {
				m.focus = messages
				m.selectBlock(i)
				return nil
			}
		}
	}
	return nil
}
