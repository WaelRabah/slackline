package slack

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"
)

const (
	contextHistoryPages = 10 // 200 messages per page
	contextThreads      = 40 // thread roots whose replies are fetched per conversation
	contextReplyPages   = 3
)

type ContextPage struct {
	Messages []Message
	Partial  bool
}
type ContextAPI interface {
	ConversationContext(context.Context, string, time.Time, time.Time) (ContextPage, error)
}
type DirectoryAPI interface {
	OpenDM(context.Context, string) (Conversation, error)
}

func (c *Client) OpenDM(ctx context.Context, user string) (Conversation, error) {
	var result struct {
		Channel Conversation `json:"channel"`
	}
	err := c.call(ctx, "conversations.open", url.Values{"users": {user}, "return_im": {"true"}}, &result)
	if err == nil && result.Channel.ID == "" {
		err = errors.New("Slack returned no conversation")
	}
	result.Channel.IsIM = true
	result.Channel.User = user
	return result.Channel, err
}

// tsParam formats a time as the Slack timestamp parameter used by history calls.
func tsParam(t time.Time) string { return fmt.Sprintf("%d.000000", t.Unix()) }

// ConversationContext reads a channel's messages in [since, until] with
// conversations.history, then adds replies for thread roots in that window.
// Every message is checked against the exact channel and timestamps. Caps are
// reported through Partial rather than presented as complete coverage.
func (c *Client) ConversationContext(ctx context.Context, channel string, since, until time.Time) (ContextPage, error) {
	result := ContextPage{}
	seen := map[string]bool{}
	add := func(message Message) {
		at := TimestampTime(message.TS)
		if at.Before(since) || at.After(until) || seen[message.TS] {
			return
		}
		message.Channel = channel
		seen[message.TS] = true
		result.Messages = append(result.Messages, message)
	}

	var roots []string
	cursor := ""
	finished := false
	for page := 0; page < contextHistoryPages; page++ {
		var history History
		err := c.call(ctx, "conversations.history", url.Values{"channel": {channel}, "oldest": {tsParam(since)}, "latest": {tsParam(until)}, "inclusive": {"true"}, "limit": {"200"}, "cursor": {cursor}}, &history)
		if err != nil {
			return ContextPage{}, err
		}
		for _, message := range history.Messages {
			add(message)
			if message.ReplyCount > 0 && (message.ThreadTS == "" || message.ThreadTS == message.TS) {
				roots = append(roots, message.TS)
			}
		}
		cursor = history.ResponseMetadata.NextCursor
		if cursor == "" || !history.HasMore {
			finished = true
			break
		}
	}
	if !finished {
		result.Partial = true
	}

	if len(roots) > contextThreads {
		roots = roots[:contextThreads]
		result.Partial = true
	}
	for _, root := range roots {
		replyCursor := ""
		for page := 0; page < contextReplyPages; page++ {
			var replies History
			err := c.call(ctx, "conversations.replies", url.Values{"channel": {channel}, "ts": {root}, "limit": {"200"}, "cursor": {replyCursor}}, &replies)
			if err != nil {
				return ContextPage{}, err
			}
			for _, message := range replies.Messages {
				if message.TS != root {
					add(message)
				}
			}
			replyCursor = replies.ResponseMetadata.NextCursor
			if replyCursor == "" || !replies.HasMore {
				break
			}
			if page == contextReplyPages-1 {
				result.Partial = true
			}
		}
	}
	return result, nil
}
