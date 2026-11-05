package tui

import (
	"net/url"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// renderMarkdown renders the standard Markdown that language models produce for
// the terminal: headings, emphasis, inline and fenced code, lists, quotes, rules
// and links. Link targets are wrapped in OSC 8 so supporting terminals make them
// clickable. Anything unsupported stays literal text.
func renderMarkdown(text string, width int) string {
	width = max(1, width)
	bold := lipgloss.NewStyle().Bold(true)
	code := lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B"))
	quote := lipgloss.NewStyle().Foreground(lipgloss.Color("#8792A2"))
	var out []string
	fenced := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			out = append(out, "  "+code.Render(line))
			continue
		}
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		switch {
		case trimmed == "":
			out = append(out, "")
		case isRule(trimmed):
			out = append(out, lipgloss.NewStyle().Foreground(lipgloss.Color("#8792A2")).Render(strings.Repeat("─", min(width, 40))))
		case strings.HasPrefix(trimmed, "#"):
			heading := strings.TrimLeft(trimmed, "#")
			out = append(out, bold.Render(renderInline(strings.TrimSpace(heading), code)))
		case strings.HasPrefix(trimmed, "> "):
			out = append(out, quote.Render("│ "+renderInline(strings.TrimPrefix(trimmed, "> "), code)))
		case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "), strings.HasPrefix(trimmed, "+ "):
			out = append(out, indent+"• "+renderInline(trimmed[2:], code))
		default:
			out = append(out, indent+renderInline(trimmed, code))
		}
	}
	return ansi.Hardwrap(strings.Join(out, "\n"), width, true)
}

func isRule(line string) bool {
	if len(line) < 3 {
		return false
	}
	for _, r := range line {
		if r != '-' && r != '*' && r != '_' {
			return false
		}
	}
	return true
}

// renderInline handles code spans, bold, italic and links. Markers without a
// closing partner are left as typed.
func renderInline(text string, code lipgloss.Style) string {
	bold := lipgloss.NewStyle().Bold(true)
	italic := lipgloss.NewStyle().Italic(true)
	var b strings.Builder
	for i := 0; i < len(text); {
		rest := text[i:]
		switch {
		case rest[0] == '`':
			if j := strings.IndexByte(rest[1:], '`'); j >= 0 {
				b.WriteString(code.Render(rest[1 : 1+j]))
				i += j + 2
				continue
			}
		case strings.HasPrefix(rest, "**"):
			if j := strings.Index(rest[2:], "**"); j > 0 {
				b.WriteString(bold.Render(rest[2 : 2+j]))
				i += j + 4
				continue
			}
		case rest[0] == '*' && len(rest) > 1 && rest[1] != ' ':
			if j := strings.IndexByte(rest[1:], '*'); j > 0 && rest[j] != ' ' {
				b.WriteString(italic.Render(rest[1 : 1+j]))
				i += j + 2
				continue
			}
		case rest[0] == '[':
			if label, target, n, ok := parseLink(rest); ok {
				if safe, ok := safeLinkTarget(target); ok {
					b.WriteString(ansi.SetHyperlink(safe) + lipgloss.NewStyle().Underline(true).Render(label) + ansi.ResetHyperlink())
				} else {
					b.WriteString(label)
				}
				i += n
				continue
			}
		}
		b.WriteByte(rest[0])
		i++
	}
	return b.String()
}

// parseLink reads "[label](target)" at the start of text and returns its length.
func parseLink(text string) (label, target string, length int, ok bool) {
	end := strings.IndexByte(text, ']')
	if end <= 0 || end+1 >= len(text) || text[end+1] != '(' {
		return "", "", 0, false
	}
	close := strings.IndexByte(text[end+2:], ')')
	if close < 0 {
		return "", "", 0, false
	}
	return text[1:end], text[end+2 : end+2+close], end + 3 + close, true
}

// safeLinkTarget allows only plain http(s) URLs, so terminal escapes cannot be injected.
func safeLinkTarget(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", false
	}
	if strings.ContainsFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f || r == ' ' }) {
		return "", false
	}
	return u.String(), true
}
