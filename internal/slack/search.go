package slack

import (
	"context"
	"net/url"
	"time"
)

type SearchHit struct {
	Message      Message
	Conversation Conversation
}
type SearchPage struct {
	Hits    []SearchHit
	Partial bool
}
type SearchAPI interface {
	SearchMessages(context.Context, string, time.Time) (SearchPage, error)
}

func (c *Client) SearchMessages(ctx context.Context, query string, since time.Time) (SearchPage, error) {
	var response struct {
		Messages struct {
			Matches []struct {
				Message
				Channel Conversation `json:"channel"`
			} `json:"matches"`
			Paging     struct{ Total int } `json:"paging"`
			Pagination struct {
				Total int `json:"total_count"`
			} `json:"pagination"`
		} `json:"messages"`
	}
	query += " after:" + since.AddDate(0, 0, -1).UTC().Format("2006-01-02")
	err := c.call(ctx, "search.messages", url.Values{"query": {query}, "sort": {"timestamp"}, "sort_dir": {"desc"}, "count": {"100"}}, &response)
	if err != nil {
		return SearchPage{}, err
	}
	result := SearchPage{Partial: max(response.Messages.Paging.Total, response.Messages.Pagination.Total) > len(response.Messages.Matches)}
	for _, hit := range response.Messages.Matches {
		if hit.Channel.ID != "" && !TimestampTime(hit.TS).Before(since) {
			message := hit.Message
			message.Channel = hit.Channel.ID
			result.Hits = append(result.Hits, SearchHit{message, hit.Channel})
		}
	}
	return result, nil
}
