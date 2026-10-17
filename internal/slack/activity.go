package slack

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Activity is a conversation snapshot from Slack's internal session interface.
// LastRead is the timestamp of a message, not the time the user opened the UI.
type Activity struct {
	UnreadCount *int   `json:"unread_count"`
	ID          string `json:"id"`
	LastRead    string `json:"last_read"`
	Latest      string `json:"latest"`
	HasUnreads  bool   `json:"has_unreads"`
}

type ActivityAPI interface {
	Activity(context.Context) ([]Activity, error)
}
type RecentAPI interface {
	RecentSent(context.Context, time.Time) (map[string]time.Time, error)
}

func (c *Client) Activity(ctx context.Context) ([]Activity, error) {
	var result struct {
		Channels []Activity `json:"channels"`
		Groups   []Activity `json:"groups"`
		IMs      []Activity `json:"ims"`
		MPIMs    []Activity `json:"mpims"`
	}
	if err := c.call(ctx, "client.counts", nil, &result); err != nil {
		return nil, err
	}
	all := append(result.Channels, result.Groups...)
	all = append(all, result.IMs...)
	return append(all, result.MPIMs...), nil
}

// TimestampTime retains Slack's subsecond precision without using float64.
func TimestampTime(ts string) time.Time {
	seconds, fraction, _ := strings.Cut(ts, ".")
	sec, err := strconv.ParseInt(seconds, 10, 64)
	if err != nil || sec <= 0 {
		return time.Time{}
	}
	if len(fraction) > 9 {
		return time.Time{}
	}
	nsec, err := strconv.ParseInt(fraction+strings.Repeat("0", 9-len(fraction)), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, nsec)
}

// Search includes authored thread replies, without fetching every channel's history.
func (c *Client) RecentSent(ctx context.Context, since time.Time) (map[string]time.Time, error) {
	result := map[string]time.Time{}
	// Search's day boundary can be exclusive; filter the exact rolling cutoff below.
	query := "from:me after:" + since.AddDate(0, 0, -1).UTC().Format("2006-01-02")
	for page := 1; page <= 100; page++ {
		var response struct {
			Messages struct {
				Matches []struct {
					TS      string `json:"ts"`
					Channel struct {
						ID string `json:"id"`
					} `json:"channel"`
				} `json:"matches"`
				Paging struct {
					Pages int `json:"pages"`
					Total int `json:"total"`
				} `json:"paging"`
				Pagination struct {
					Pages int `json:"page_count"`
					Total int `json:"total_count"`
				} `json:"pagination"`
			} `json:"messages"`
		}
		err := c.call(ctx, "search.messages", url.Values{"query": {query}, "sort": {"timestamp"}, "sort_dir": {"desc"}, "count": {"100"}, "page": {fmt.Sprint(page)}}, &response)
		if err != nil {
			return nil, err
		}
		for _, match := range response.Messages.Matches {
			at := TimestampTime(match.TS)
			if match.Channel.ID != "" && !at.Before(since) && at.After(result[match.Channel.ID]) {
				result[match.Channel.ID] = at
			}
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
	return nil, errors.New("recent message search exceeded pagination limit; showing all conversations")
}
