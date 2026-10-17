package slack

import (
	"context"
	"net/url"
)

type Reaction struct {
	Name  string   `json:"name"`
	Count int      `json:"count"`
	Users []string `json:"users"`
}

// EmojiAPI is optional: a restricted emoji directory must not prevent reading.
type EmojiAPI interface {
	Emojis(context.Context) (map[string]string, error)
}

func (c *Client) Emojis(ctx context.Context) (map[string]string, error) {
	var response struct {
		Emoji map[string]string `json:"emoji"`
	}
	err := c.call(ctx, "emoji.list", nil, &response)
	return response.Emoji, err
}

// ReactionAPI supplies authoritative counts even when the Users list is partial.
type ReactionAPI interface {
	Reactions(context.Context, string, string) ([]Reaction, error)
}

func (c *Client) Reactions(ctx context.Context, channel, ts string) ([]Reaction, error) {
	var response struct {
		Message Message `json:"message"`
	}
	err := c.call(ctx, "reactions.get", url.Values{"channel": {channel}, "timestamp": {ts}, "full": {"true"}}, &response)
	return response.Message.Reactions, err
}
