package slack

import (
	"context"
	"errors"
	"net/url"
)

type Section struct {
	ID       string `json:"channel_section_id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Next     string `json:"next_channel_section_id"`
	Redacted bool   `json:"is_redacted"`
	Page     struct {
		Channels []string `json:"channel_ids"`
		Count    int      `json:"count"`
		Cursor   string   `json:"cursor"`
	} `json:"channel_ids_page"`
}

// Sections is optional: restricted workspaces can use the local type grouping.
// This endpoint is internal to Slack's session client and may change.
func (c *Client) Sections(ctx context.Context) ([]Section, error) {
	var all []Section
	cursor := ""
	seen := map[string]bool{}
	for {
		var page struct {
			Sections []Section `json:"channel_sections"`
			Cursor   string    `json:"cursor"`
		}
		if err := c.call(ctx, "users.channelSections.list", url.Values{"cursor": {cursor}}, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Sections...)
		cursor = page.Cursor
		if cursor == "" {
			return all, nil
		}
		if seen[cursor] {
			return nil, errors.New("Slack repeated a sidebar pagination cursor")
		}
		seen[cursor] = true
	}
}
func (*Demo) Sections(context.Context) ([]Section, error) {
	s := Section{ID: "engineering", Name: "Engineering", Type: "standard"}
	s.Page.Channels = []string{"CDEV"}
	return []Section{s}, nil
}
