package slack

import (
	"encoding/json"
	"strings"
)

// DisplayText returns the readable text of a message: its own text, then any
// attachment and block text. Bot and integration posts (alerts, deploy notices)
// often carry their content only in attachments or blocks.
func (m Message) DisplayText() string {
	var parts []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range parts {
			if existing == value {
				return
			}
		}
		parts = append(parts, value)
	}
	add(m.Text)
	for _, attachment := range m.Attachments {
		add(attachment.Pretext)
		add(attachment.Title)
		add(attachment.Text)
		if attachment.Text == "" && attachment.Title == "" {
			add(attachment.Fallback)
		}
	}
	var walk func([]ImageBlock, int)
	walk = func(blocks []ImageBlock, depth int) {
		if depth > 6 {
			return
		}
		for _, block := range blocks {
			add(blockText(block.Text))
			if block.Accessory != nil {
				walk([]ImageBlock{*block.Accessory}, depth+1)
			}
			walk(block.Elements, depth+1)
		}
	}
	walk(m.Blocks, 0)
	return strings.Join(parts, "\n")
}

// blockText reads the text of a Block Kit text object, which is an object
// ({"type":"mrkdwn","text":"..."}) rather than a plain string.
func blockText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var object struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &object) == nil {
		return object.Text
	}
	return ""
}
