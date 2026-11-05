package slack

import (
	"context"
	"net/url"
	"strings"
)

// MutedAPI reads the conversations the user has muted in Slack.
type MutedAPI interface {
	MutedChannels(context.Context) (map[string]bool, error)
}

// MutedChannels returns the IDs of muted conversations from the user's Slack
// preferences. It only reads; nothing is changed.
func (c *Client) MutedChannels(ctx context.Context) (map[string]bool, error) {
	var result struct {
		Prefs struct {
			MutedChannels string `json:"muted_channels"`
		} `json:"prefs"`
	}
	if err := c.call(ctx, "users.prefs.get", url.Values{}, &result); err != nil {
		return nil, err
	}
	muted := map[string]bool{}
	for _, id := range strings.Split(result.Prefs.MutedChannels, ",") {
		if id = strings.TrimSpace(id); id != "" {
			muted[id] = true
		}
	}
	return muted, nil
}
