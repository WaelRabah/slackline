package slack

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"
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

// Search includes replies on roots older than the window. Every result is checked
// against the exact channel and timestamps, even if Slack search filters differ.
func (c *Client) ConversationContext(ctx context.Context, channel string, since, until time.Time) (ContextPage, error) {
	result := ContextPage{}
	seen := map[string]bool{}
	query := "in:" + channel + " after:" + since.AddDate(0, 0, -1).UTC().Format("2006-01-02")
	for page := 1; page <= 10; page++ {
		var response struct {
			Messages struct {
				Matches []struct {
					Message
					Channel struct {
						ID string `json:"id"`
					} `json:"channel"`
				} `json:"matches"`
				Paging     struct{ Pages, Total int } `json:"paging"`
				Pagination struct {
					Pages int `json:"page_count"`
					Total int `json:"total_count"`
				} `json:"pagination"`
			} `json:"messages"`
		}
		err := c.call(ctx, "search.messages", url.Values{"query": {query}, "sort": {"timestamp"}, "sort_dir": {"desc"}, "count": {"100"}, "page": {fmt.Sprint(page)}}, &response)
		if err != nil {
			return ContextPage{}, err
		}
		for _, match := range response.Messages.Matches {
			at := TimestampTime(match.TS)
			if match.Channel.ID != channel || at.Before(since) || at.After(until) || seen[match.TS] {
				continue
			}
			message := match.Message
			message.Channel = channel
			result.Messages = append(result.Messages, message)
			seen[match.TS] = true
		}
		pages := max(response.Messages.Paging.Pages, response.Messages.Pagination.Pages)
		total := max(response.Messages.Paging.Total, response.Messages.Pagination.Total)
		if pages == 0 && total > 0 {
			pages = (total + 99) / 100
		}
		if (pages > 0 && page >= pages) || (pages == 0 && len(response.Messages.Matches) < 100) {
			return result, nil
		}
	}
	result.Partial = true
	return result, nil
}
