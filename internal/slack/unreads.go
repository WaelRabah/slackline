package slack

import (
	"context"
	"net/url"
)

const unreadPages = 5 // 200 messages per page; larger backlogs are reported as partial

// systemSubtypes are channel events, not content. Bot, integration and file
// posts are kept: alerts and shared files are often the content people need.
var systemSubtypes = map[string]bool{
	"channel_join": true, "channel_leave": true, "channel_topic": true, "channel_purpose": true,
	"channel_name": true, "channel_archive": true, "channel_unarchive": true,
	"group_join": true, "group_leave": true, "group_topic": true, "group_purpose": true,
	"group_name": true, "group_archive": true, "group_unarchive": true,
	"pinned_item": true, "unpinned_item": true, "message_changed": true, "message_deleted": true,
	"message_replied": true, "tombstone": true, "ekm_access_denied": true,
}

// UnreadPage holds one channel's unread channel-level messages.
type UnreadPage struct {
	Messages []Message
	Partial  bool
}

type UnreadsAPI interface {
	UnreadMessages(context.Context, string, string, string, string) (UnreadPage, error)
}

// UnreadMessages returns messages in (after, through] for one channel, excluding
// the user's own messages and system subtypes. Thread replies come from the
// Threads feed, not from here.
func (c *Client) UnreadMessages(ctx context.Context, channel, after, through, self string) (UnreadPage, error) {
	result := UnreadPage{}
	seen := map[string]bool{}
	afterAt, throughAt := TimestampTime(after), TimestampTime(through)
	cursor := ""
	finished := false
	for page := 0; page < unreadPages; page++ {
		var history History
		err := c.call(ctx, "conversations.history", url.Values{"channel": {channel}, "oldest": {after}, "latest": {through}, "inclusive": {"true"}, "limit": {"200"}, "cursor": {cursor}}, &history)
		if err != nil {
			return UnreadPage{}, err
		}
		for _, message := range history.Messages {
			at := TimestampTime(message.TS)
			if !at.After(afterAt) || at.After(throughAt) || message.User == self || seen[message.TS] {
				continue
			}
			if systemSubtypes[message.Subtype] {
				continue
			}
			seen[message.TS] = true
			message.Channel = channel
			result.Messages = append(result.Messages, message)
		}
		cursor = history.ResponseMetadata.NextCursor
		if cursor == "" || !history.HasMore {
			finished = true
			break
		}
	}
	result.Partial = !finished
	return result, nil
}
