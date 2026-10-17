package slack

import (
	"context"
	"fmt"
	"time"
)

type Session struct {
	Team   string `json:"team"`
	TeamID string `json:"team_id"`
	User   string `json:"user"`
	UserID string `json:"user_id"`
}

type Conversation struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	User       string `json:"user"`
	IsIM       bool   `json:"is_im"`
	IsMPIM     bool   `json:"is_mpim"`
	IsPrivate  bool   `json:"is_private"`
	IsArchived bool   `json:"is_archived"`
}

type User struct {
	Deleted  bool   `json:"deleted"`
	IsBot    bool   `json:"is_bot"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	RealName string `json:"real_name"`
	Profile  struct {
		DisplayName string `json:"display_name"`
		RealName    string `json:"real_name"`
	} `json:"profile"`
}

func (u User) DisplayName() string {
	for _, name := range []string{u.Profile.DisplayName, u.Profile.RealName, u.RealName, u.Name, u.ID} {
		if name != "" {
			return name
		}
	}
	return "unknown"
}

type Message struct {
	BotID       string `json:"bot_id"`
	Channel     string `json:"channel"`
	LastRead    string `json:"last_read"`
	LatestReply string `json:"latest_reply"`
	TS          string `json:"ts"`
	User        string `json:"user"`
	Text        string `json:"text"`
	Username    string `json:"username"`
	ThreadTS    string `json:"thread_ts"`
	ReplyCount  int    `json:"reply_count"`
	Subtype     string `json:"subtype"`
}

type History struct {
	Messages         []Message `json:"messages"`
	HasMore          bool      `json:"has_more"`
	ResponseMetadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

// API is shared by the HTTP client, offline demo, and UI test fixtures.
type API interface {
	AuthTest(context.Context) (Session, error)
	Conversations(context.Context) ([]Conversation, error)
	Users(context.Context) ([]User, error)
	History(context.Context, string, string) (History, error)
	Replies(context.Context, string, string, string) (History, error)
	SendReply(context.Context, string, string, string) (Message, error)
	Send(context.Context, string, string) (Message, error)
}

type APIError struct{ Method, Code string }

func (e *APIError) Error() string {
	switch e.Code {
	case "invalid_auth", "not_authed", "token_revoked", "token_expired", "account_inactive":
		return "Slack session is invalid or expired; run auth login with a fresh xoxc token and matching d cookie"
	default:
		return fmt.Sprintf("Slack %s: %s", e.Method, e.Code)
	}
}

type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("Slack rate limited this request; retry in %s", e.RetryAfter)
}
