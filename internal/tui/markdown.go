package tui

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
)

// Slack mrkdwn is not full Markdown: emphasis markers are single characters,
// styles do not nest, and links/mentions/channels arrive as <...> control
// tokens. Code blocks and inline code keep their literal contents; everything
// else is rendered for the terminal, with http(s) targets wrapped in OSC 8 so
// they are clickable in supporting terminals.
var emojiShortcode = regexp.MustCompile(`^:[A-Za-z0-9_+.-]+:(?::skin-tone-[2-6]:)?`)

func (m Model) renderMarkdown(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	var code []string
	fenced := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if fenced {
			if strings.HasPrefix(trimmed, "```") {
				out = append(out, m.codeBlock(code))
				code = nil
				fenced = false
			} else {
				code = append(code, line)
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			if rest != "" && strings.HasSuffix(rest, "```") {
				out = append(out, m.codeBlock([]string{strings.TrimSuffix(rest, "```")}))
			} else if rest == "" || !strings.ContainsAny(rest, " \t`") && !strings.HasPrefix(rest, ":") {
				// A language tag on the opening fence is ignored.
				fenced = true
			} else {
				out = append(out, m.renderLine(line))
			}
			continue
		}
		out = append(out, m.renderLine(line))
	}
	if fenced {
		out = append(out, m.codeBlock(code))
	}
	return strings.Join(out, "\n")
}

func (m Model) renderLine(line string) string {
	trimmed := strings.TrimLeft(line, " ")
	if strings.HasPrefix(trimmed, ">") && (len(trimmed) == 1 || trimmed[1] == ' ') {
		body := strings.TrimPrefix(trimmed[1:], " ")
		bar := lipgloss.NewStyle().Foreground(m.muted()).Render("▎")
		return bar + " " + m.renderInline(body)
	}
	return m.renderInline(line)
}

func (m Model) codeBlock(lines []string) string {
	style := lipgloss.NewStyle().Background(lipgloss.Color("#1E242E")).Foreground(lipgloss.Color("#D8DEE9"))
	rendered := make([]string, len(lines))
	for i, line := range lines {
		rendered[i] = style.Render(" " + line + " ")
	}
	if len(rendered) == 0 {
		return style.Render(" ")
	}
	return strings.Join(rendered, "\n")
}

func (m Model) inlineCode(text string) string {
	return lipgloss.NewStyle().Background(lipgloss.Color("#2A303B")).Foreground(lipgloss.Color("#E6C07B")).Render(text)
}

func (m Model) mentionText(text string) string {
	return lipgloss.NewStyle().Foreground(m.accent()).Bold(true).Render(text)
}

func (m Model) linkText(target, label string) string {
	style := lipgloss.NewStyle().Foreground(m.accent()).Underline(true)
	return "\x1b]8;;" + target + "\x1b\\" + style.Render(label) + "\x1b]8;;\x1b\\"
}

func (m Model) renderInline(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); {
		switch text[i] {
		case '`':
			if end := strings.IndexByte(text[i+1:], '`'); end > 0 {
				out.WriteString(m.inlineCode(text[i+1 : i+1+end]))
				i += end + 2
				continue
			}
		case '<':
			if end := strings.IndexByte(text[i+1:], '>'); end >= 0 {
				if rendered, ok := m.controlToken(text[i+1 : i+1+end]); ok {
					out.WriteString(rendered)
					i += end + 2
					continue
				}
			}
		case '*', '_', '~':
			if rendered, next, ok := m.emphasis(text, i); ok {
				out.WriteString(rendered)
				i = next
				continue
			}
		case ':':
			if loc := emojiShortcode.FindStringIndex(text[i:]); loc != nil {
				name := strings.Trim(text[i:i+loc[1]], ":")
				out.WriteString(m.emojiGlyph(name))
				i += loc[1]
				continue
			}
		case 'h':
			if rendered, next, ok := m.bareLink(text, i); ok {
				out.WriteString(rendered)
				i = next
				continue
			}
		}
		out.WriteByte(text[i])
		i++
	}
	return out.String()
}

func (m Model) controlToken(inner string) (string, bool) {
	switch {
	case strings.HasPrefix(inner, "@"):
		id, label, _ := strings.Cut(inner[1:], "|")
		name := label
		if name == "" {
			name = m.users[id]
		}
		if name == "" {
			name = id
		}
		if name == "" {
			return "", false
		}
		return m.mentionText("@" + cleanLine(name)), true
	case strings.HasPrefix(inner, "#"):
		id, label, _ := strings.Cut(inner[1:], "|")
		name := label
		if name == "" {
			for _, conversation := range m.conversations {
				if conversation.ID == id {
					name = conversation.Name
					break
				}
			}
		}
		if name == "" {
			name = id
		}
		return m.mentionText("#" + cleanLine(name)), true
	case inner == "!here", inner == "!channel", inner == "!everyone":
		return m.mentionText("@" + inner[1:]), true
	case strings.HasPrefix(inner, "!subteam^"):
		_, label, _ := strings.Cut(inner, "|")
		if label == "" {
			return "", false
		}
		return m.mentionText(cleanLine(label)), true
	case strings.HasPrefix(inner, "http://"), strings.HasPrefix(inner, "https://"), strings.HasPrefix(inner, "mailto:"):
		target, label, hasLabel := strings.Cut(inner, "|")
		if hasLabel && strings.TrimSpace(cleanLine(label)) != "" {
			label = m.emojify(strings.TrimSpace(cleanLine(label)))
		} else {
			label = target
		}
		if safe, ok := safeLink(target); ok {
			return m.linkText(safe, label), true
		}
		return label, true
	}
	return "", false
}

func (m Model) emphasis(text string, start int) (string, int, bool) {
	marker := text[start]
	if start > 0 && !emphasisBoundary(text[start-1]) {
		return "", 0, false
	}
	rest := text[start+1:]
	end := strings.IndexByte(rest, marker)
	if end <= 0 || rest[0] == ' ' || rest[0] == '\t' || rest[end-1] == ' ' || rest[end-1] == '\t' {
		return "", 0, false
	}
	inner := m.emojify(rest[:end])
	switch marker {
	case '*':
		inner = lipgloss.NewStyle().Bold(true).Render(inner)
	case '_':
		inner = lipgloss.NewStyle().Italic(true).Render(inner)
	case '~':
		inner = lipgloss.NewStyle().Strikethrough(true).Render(inner)
	}
	return inner, start + 1 + end + 1, true
}

func emphasisBoundary(value byte) bool {
	return strings.IndexByte(" \t([{\"'-,!?;:", value) >= 0
}

func (m Model) bareLink(text string, start int) (string, int, bool) {
	rest := text[start:]
	prefix := ""
	for _, candidate := range []string{"https://", "http://"} {
		if strings.HasPrefix(rest, candidate) {
			prefix = candidate
			break
		}
	}
	if prefix == "" {
		return "", 0, false
	}
	end := len(prefix)
	for end < len(rest) && !strings.ContainsRune(" \t<>", rune(rest[end])) {
		end++
	}
	// Colons are valid URL characters (and close emoji shortcodes), so only
	// strip sentence punctuation that commonly follows a pasted link.
	for end > len(prefix) && strings.ContainsRune(".,;!?)]}", rune(rest[end-1])) {
		end--
	}
	raw := rest[:end]
	safe, ok := safeLink(raw)
	if !ok {
		return "", 0, false
	}
	return m.linkText(safe, raw), start + end, true
}

// safeLink rejects control characters and non-web schemes before they reach an
// OSC 8 escape sequence.
func safeLink(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 4096 {
		return "", false
	}
	for _, value := range raw {
		// C0/C1 controls can terminate or forge OSC sequences; whitespace is
		// not valid in a URL and can break the target for the terminal.
		if unicode.IsControl(value) || unicode.IsSpace(value) {
			return "", false
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		if parsed.Host == "" {
			return "", false
		}
	case "mailto":
		if parsed.Opaque == "" && parsed.Path == "" {
			return "", false
		}
	default:
		return "", false
	}
	return raw, true
}
