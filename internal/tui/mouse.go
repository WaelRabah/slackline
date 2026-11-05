package tui

import tea "charm.land/bubbletea/v2"

// sidebarBodyTop is the screen row of the first sidebar entry: header, blank
// line, frame top, then the "CHANNELS" title.
const sidebarBodyTop = 4

// mouseClick opens the sidebar entry under a left click. Clicks anywhere else do nothing.
func (m *Model) mouseClick(msg tea.MouseClickMsg) tea.Cmd {
	mouse := msg.Mouse()
	if mouse.Button != tea.MouseLeft || m.mode != normal || m.width < 60 || m.height < 12 {
		return nil
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
