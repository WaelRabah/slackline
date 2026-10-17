package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxResponse = 16 << 20

var errorCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,79}$`)

type Client struct {
	creds   Credentials
	http    *http.Client
	baseURL string
}

func NewClient(creds Credentials) *Client {
	return &Client{
		creds: creds,
		// Never forward the d cookie or token to a redirect destination.
		http:    &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		baseURL: "https://slack.com/api/",
	}
}

func (c *Client) call(ctx context.Context, method string, params url.Values, out any) error {
	if params == nil {
		params = url.Values{}
	}
	// Like Slack's web client, keep the token in the POST body, never the URL.
	params.Set("token", c.creds.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+method, strings.NewReader(params.Encode()))
	if err != nil {
		return errors.New("cannot create Slack request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "slackline/0.1")
	req.AddCookie(&http.Cookie{Name: "d", Value: c.creds.cookie})
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Transport errors can embed request details; do not log the raw error.
		return errors.New("Slack request failed; check your connection (a message send may have reached Slack)")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		seconds, err := strconv.Atoi(resp.Header.Get("Retry-After"))
		if err != nil || seconds < 1 || seconds > 86400 {
			seconds = 60
		}
		return &RateLimitError{RetryAfter: time.Duration(seconds) * time.Second}
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Slack %s returned HTTP %d", method, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return errors.New("cannot read Slack response")
	}
	if len(data) > maxResponse {
		return errors.New("Slack response exceeded size limit")
	}
	var envelope struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return errors.New("Slack returned an invalid JSON response")
	}
	if !envelope.OK {
		code := envelope.Error
		if !errorCode.MatchString(code) || strings.Contains(code, c.creds.token) || strings.Contains(code, c.creds.cookie) {
			code = "request_failed"
		}
		return &APIError{Method: method, Code: code}
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return errors.New("cannot decode Slack response")
		}
	}
	return nil
}

func (c *Client) AuthTest(ctx context.Context) (Session, error) {
	var session Session
	err := c.call(ctx, "auth.test", nil, &session)
	return session, err
}

func (c *Client) Conversations(ctx context.Context) ([]Conversation, error) {
	var result []Conversation
	cursor := ""
	seen := map[string]bool{}
	for {
		var page struct {
			Channels         []Conversation `json:"channels"`
			ResponseMetadata struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		err := c.call(ctx, "users.conversations", url.Values{"types": {"public_channel,private_channel,im,mpim"}, "exclude_archived": {"true"}, "limit": {"200"}, "cursor": {cursor}}, &page)
		if err != nil {
			return nil, err
		}
		result = append(result, page.Channels...)
		cursor = strings.TrimSpace(page.ResponseMetadata.NextCursor)
		if cursor == "" {
			return result, nil
		}
		if seen[cursor] {
			return nil, errors.New("Slack repeated a conversation pagination cursor")
		}
		seen[cursor] = true
	}
}

func (c *Client) Users(ctx context.Context) ([]User, error) {
	var result []User
	cursor := ""
	seen := map[string]bool{}
	for {
		var page struct {
			Members          []User `json:"members"`
			ResponseMetadata struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := c.call(ctx, "users.list", url.Values{"limit": {"200"}, "cursor": {cursor}}, &page); err != nil {
			return nil, err
		}
		result = append(result, page.Members...)
		cursor = strings.TrimSpace(page.ResponseMetadata.NextCursor)
		if cursor == "" {
			return result, nil
		}
		if seen[cursor] {
			return nil, errors.New("Slack repeated a user pagination cursor")
		}
		seen[cursor] = true
	}
}

func (c *Client) History(ctx context.Context, channel, cursor string) (History, error) {
	var result History
	// A modest page size also accommodates installations with stricter limits.
	err := c.call(ctx, "conversations.history", url.Values{"channel": {channel}, "limit": {"15"}, "cursor": {cursor}}, &result)
	return result, err
}

func (c *Client) Replies(ctx context.Context, channel, thread, cursor string) (History, error) {
	var result History
	err := c.call(ctx, "conversations.replies", url.Values{"channel": {channel}, "ts": {thread}, "limit": {"15"}, "cursor": {cursor}}, &result)
	return result, err
}
func (c *Client) Send(ctx context.Context, channel, text string) (Message, error) {
	return c.sendMessage(ctx, channel, "", text)
}
func (c *Client) SendReply(ctx context.Context, channel, thread, text string) (Message, error) {
	return c.sendMessage(ctx, channel, thread, text)
}
func (c *Client) sendMessage(ctx context.Context, channel, thread, text string) (Message, error) {
	if strings.TrimSpace(text) == "" {
		return Message{}, errors.New("message is empty")
	}
	var result struct {
		TS      string  `json:"ts"`
		Message Message `json:"message"`
	}
	// Do not automatically retry writes: ambiguous network failures could duplicate a post.
	params := url.Values{"channel": {channel}, "text": {text}, "unfurl_links": {"false"}, "unfurl_media": {"false"}}
	if thread != "" {
		params.Set("thread_ts", thread)
	}
	err := c.call(ctx, "chat.postMessage", params, &result)
	if err != nil {
		return Message{}, err
	}
	if result.Message.TS == "" {
		result.Message.TS = result.TS
	}
	if result.Message.Text == "" {
		result.Message.Text = text
	}
	if thread != "" {
		result.Message.ThreadTS = thread
	}
	return result.Message, nil
}
