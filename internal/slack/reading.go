package slack

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type UnreadCount struct {
	Count int
	Exact bool
}
type ReadAPI interface {
	MarkRead(context.Context, string, string, string) error
	CountUnread(context.Context, string, string, string, string) (UnreadCount, error)
}

func (c *Client) MarkRead(ctx context.Context, channel, thread, ts string) error {
	if channel == "" || TimestampTime(ts).IsZero() {
		return errors.New("cannot mark an invalid read position")
	}
	method := "conversations.mark"
	params := url.Values{"channel": {channel}, "ts": {ts}}
	if thread != "" {
		method = "subscriptions.thread.mark"
		params.Set("thread_ts", thread)
	}
	err := c.call(ctx, method, params, nil)
	if benignError(method, err) {
		// The thread or message was deleted; there is nothing left to mark.
		return nil
	}
	return err
}

// benignError reports failures that leave nothing to do. Marking a thread whose
// root Slack no longer has is one: the position it would record is gone.
func benignError(method string, err error) bool {
	if err == nil {
		return false
	}
	switch method {
	case "subscriptions.thread.mark":
		return strings.Contains(err.Error(), "message_not_found")
	case "reactions.add":
		return strings.Contains(err.Error(), "already_reacted")
	case "reactions.remove":
		return strings.Contains(err.Error(), "no_reaction")
	}
	return false
}
func (c *Client) CountUnread(ctx context.Context, channel, after, through, self string) (UnreadCount, error) {
	result := UnreadCount{}
	cursor := ""
	seen := map[string]bool{}
	messages := map[string]bool{}
	// Bound each background job; a large backlog is honestly displayed as N+.
	for page := 0; page < 5; page++ {
		var history History
		err := c.call(ctx, "conversations.history", url.Values{"channel": {channel}, "oldest": {after}, "latest": {through}, "inclusive": {"true"}, "limit": {"200"}, "cursor": {cursor}}, &history)
		if err != nil {
			return result, err
		}
		for _, message := range history.Messages {
			at := TimestampTime(message.TS)
			if at.After(TimestampTime(after)) && !at.After(TimestampTime(through)) && message.User != self && !messages[message.TS] {
				messages[message.TS] = true
				result.Count++
			}
		}
		cursor = history.ResponseMetadata.NextCursor
		if cursor == "" && !history.HasMore {
			result.Exact = true
			return result, nil
		}
		if cursor == "" || seen[cursor] {
			return result, nil
		}
		seen[cursor] = true
	}
	return result, nil
}

type Thread struct {
	Root          Message   `json:"root_msg"`
	UnreadReplies []Message `json:"unread_replies"`
	LatestReplies []Message `json:"latest_replies"`
}

func (t Thread) Key() string { return t.Root.Channel + "/" + t.Root.TS }
func (t Thread) Latest() string {
	latest := t.Root.TS
	for _, ts := range []string{t.Root.LatestReply} {
		if TimestampTime(ts).After(TimestampTime(latest)) {
			latest = ts
		}
	}
	for _, reply := range append(append([]Message{}, t.LatestReplies...), t.UnreadReplies...) {
		if TimestampTime(reply.TS).After(TimestampTime(latest)) {
			latest = reply.TS
		}
	}
	return latest
}

type ThreadPage struct {
	Threads     []Thread `json:"threads"`
	HasMore     bool     `json:"has_more"`
	TotalUnread int      `json:"total_unread_replies"`
	Next        string
}
type ThreadsAPI interface {
	Threads(context.Context, string) (ThreadPage, error)
}

func (c *Client) Threads(ctx context.Context, before string) (ThreadPage, error) {
	if before == "" {
		before = fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
	}
	var result ThreadPage
	if err := c.call(ctx, "subscriptions.thread.getView", url.Values{"current_ts": {before}}, &result); err != nil {
		return result, err
	}
	for _, thread := range result.Threads {
		latest := thread.Latest()
		if result.Next == "" || TimestampTime(latest).Before(TimestampTime(result.Next)) {
			result.Next = latest
		}
	}
	if result.HasMore && (result.Next == "" || !TimestampTime(result.Next).Before(TimestampTime(before))) {
		return ThreadPage{}, errors.New("Slack did not advance thread pagination")
	}
	if !result.HasMore {
		result.Next = ""
	}
	return result, nil
}
