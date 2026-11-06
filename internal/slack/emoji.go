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

// ReactAPI adds and removes the signed-in user's own reaction. It is optional
// so a restricted client still reads messages.
type ReactAPI interface {
	AddReaction(ctx context.Context, channel, ts, name string) error
	RemoveReaction(ctx context.Context, channel, ts, name string) error
}

// AddReaction is idempotent: Slack's already_reacted means the goal is met.
func (c *Client) AddReaction(ctx context.Context, channel, ts, name string) error {
	err := c.call(ctx, "reactions.add", url.Values{"channel": {channel}, "timestamp": {ts}, "name": {name}}, nil)
	if apiErr, ok := err.(*APIError); ok && apiErr.Code == "already_reacted" {
		return nil
	}
	return err
}

// RemoveReaction is idempotent: Slack's no_reaction means it is already gone.
func (c *Client) RemoveReaction(ctx context.Context, channel, ts, name string) error {
	err := c.call(ctx, "reactions.remove", url.Values{"channel": {channel}, "timestamp": {ts}, "name": {name}}, nil)
	if apiErr, ok := err.(*APIError); ok && apiErr.Code == "no_reaction" {
		return nil
	}
	return err
}
