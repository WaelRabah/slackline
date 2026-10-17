package tui

import (
	"fmt"
	"strings"
)

type sidebarGroup struct {
	id, title string
	indices   []int
}
type sidebarRow struct {
	label, group string
	destination  string
	index        int
}

func (m Model) sidebarGroups() []sidebarGroup {
	byID := map[string]int{}
	for i, c := range m.conversations {
		if m.visibleConversation(c.ID) {
			byID[c.ID] = i
		}
	}
	used := map[int]bool{}
	groups := []sidebarGroup{}
	unread := sidebarGroup{id: "local-unread", title: "Unread"}
	for i, c := range m.conversations {
		if _, ok := m.unread[c.ID]; ok {
			unread.indices = append(unread.indices, i)
			used[i] = true
		}
	}
	if len(unread.indices) > 0 {
		groups = append(groups, unread)
	}
	// Native sections form a linked list; keep response order for extra heads.
	sections := m.sections
	pointed := map[string]bool{}
	for _, s := range sections {
		if s.Next != "" {
			pointed[s.Next] = true
		}
	}
	order := []int{}
	visited := map[int]bool{}
	var walk func(int)
	walk = func(index int) {
		for !visited[index] {
			visited[index] = true
			order = append(order, index)
			next := -1
			for i, s := range sections {
				if s.ID == sections[index].Next && s.ID != "" {
					next = i
					break
				}
			}
			if next < 0 {
				break
			}
			index = next
		}
	}
	for i, s := range sections {
		if !pointed[s.ID] {
			walk(i)
		}
	}
	for i := range sections {
		if !visited[i] {
			walk(i)
		}
	}
	names := map[string]string{"channels": "Channels", "direct_messages": "Direct messages", "stars": "Starred", "recent_apps": "Apps", "agents": "Agents & apps", "slack_connect": "External connections", "salesforce_records": "Salesforce"}
	for _, index := range order {
		s := sections[index]
		if s.Redacted || s.Type == "temporary_channels" {
			continue
		}
		title := cleanLine(s.Name)
		if title == "" {
			title = names[s.Type]
		}
		if title == "" {
			title = strings.ReplaceAll(s.Type, "_", " ")
		}
		g := sidebarGroup{id: s.ID, title: title}
		for _, id := range s.Page.Channels {
			if i, ok := byID[id]; ok && !used[i] {
				g.indices = append(g.indices, i)
				used[i] = true
			}
		}
		if len(g.indices) > 0 || ((!m.recentOnly || !m.filterReady()) && (s.Type == "standard" || s.Type == "stars")) {
			groups = append(groups, g)
		}
	}
	fallback := []sidebarGroup{{id: "local-channels", title: "Channels"}, {id: "local-private", title: "Private channels"}, {id: "local-dms", title: "Direct messages"}, {id: "local-group-dms", title: "Group DMs"}}
	for i, c := range m.conversations {
		if used[i] || !m.visibleConversation(c.ID) {
			continue
		}
		group := 0
		switch {
		case c.IsMPIM:
			group = 3
		case c.IsIM:
			group = 2
		case c.IsPrivate:
			group = 1
		}
		fallback[group].indices = append(fallback[group].indices, i)
	}
	for _, g := range fallback {
		if len(g.indices) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}
func (m Model) sidebarRows() []sidebarRow {
	rows := []sidebarRow{{label: "✦ AI assistant", destination: aiAssistant, index: -2}, {label: "✦ AI summaries", destination: aiSummaries, index: -2}, {label: "↳ Threads", destination: threadsDestination, index: -2}}
	for _, group := range m.sidebarGroups() {
		marker := "▾ "
		if m.collapsed[group.id] {
			marker = "▸ "
		}
		rows = append(rows, sidebarRow{label: fmt.Sprintf("%s%s (%d)", marker, group.title, len(group.indices)), group: group.id, index: -1})
		if m.collapsed[group.id] {
			continue
		}
		for _, i := range group.indices {
			rows = append(rows, sidebarRow{label: m.label(m.conversations[i]), group: group.id, index: i})
		}
	}
	return rows
}
func (m Model) indexInGroup(index int, id string) bool {
	for _, g := range m.sidebarGroups() {
		if g.id == id {
			for _, i := range g.indices {
				if i == index {
					return true
				}
			}
		}
	}
	return false
}

// A stable row key keeps selection on its heading when a section is folded.
func (m Model) rowKey(row sidebarRow) string {
	if row.destination != "" {
		return "destination:" + row.destination
	}
	if row.index < 0 {
		return "section:" + row.group
	}
	return "conversation:" + m.conversations[row.index].ID
}
func (m Model) sidebarPosition(rows []sidebarRow) int {
	for i, row := range rows {
		if m.rowKey(row) == m.sidebarKey {
			return i
		}
	}
	for i, row := range rows {
		if row.index == m.selected {
			return i
		}
	}
	for i, row := range rows {
		if row.index < 0 && m.indexInGroup(m.selected, row.group) {
			return i
		}
	}
	return 0
}
func (m *Model) revealSidebarSelection() {
	for _, group := range m.sidebarGroups() {
		for _, index := range group.indices {
			if index == m.selected {
				m.collapsed[group.id] = false
			}
		}
	}
}
func (m Model) sidebarSelection() (sidebarRow, bool) {
	rows := m.sidebarRows()
	if len(rows) == 0 {
		return sidebarRow{}, false
	}
	return rows[m.sidebarPosition(rows)], true
}
func (m *Model) selectSidebar(index int) {
	rows := m.sidebarRows()
	if len(rows) == 0 {
		m.sidebarKey = ""
		return
	}
	row := rows[max(0, min(index, len(rows)-1))]
	m.sidebarKey = m.rowKey(row)
	if row.index >= 0 {
		m.selected = row.index
	}
}
func (m Model) selectedSectionCollapsed() bool {
	row, ok := m.sidebarSelection()
	return ok && m.collapsed[row.group]
}
func (m *Model) toggleSection() {
	row, ok := m.sidebarSelection()
	if !ok || row.destination != "" {
		return
	}
	m.collapsed[row.group] = !m.collapsed[row.group]
	m.sidebarKey = "section:" + row.group
}
func (m *Model) moveConversation(delta int) {
	rows := m.sidebarRows()
	if len(rows) > 0 {
		m.selectSidebar(m.sidebarPosition(rows) + delta)
	}
}
