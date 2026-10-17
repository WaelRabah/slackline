package slack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// LiveEvent never carries the signed socket URL or session credentials.
type LiveEvent struct {
	Reaction   *ReactionChange
	Type       string
	State      string
	Message    Message
	Changed    *Message
	DeletedTS  string
	RetryAfter time.Duration
}

type ReactionChange struct {
	Name, User, Channel, TS, EventTS string
}
type LiveAPI interface {
	Listen(context.Context, func(LiveEvent)) error
}

func ParseLiveEvent(data []byte) (LiveEvent, error) {
	var wire struct {
		Type      string   `json:"type"`
		Changed   *Message `json:"message"`
		DeletedTS string   `json:"deleted_ts"`
		Reaction  string   `json:"reaction"`
		EventTS   string   `json:"event_ts"`
		Item      struct {
			Type, Channel, TS string
		} `json:"item"`
	}
	var message Message
	if json.Unmarshal(data, &wire) != nil || json.Unmarshal(data, &message) != nil {
		return LiveEvent{}, errors.New("invalid live event")
	}
	event := LiveEvent{Type: wire.Type, Message: message, Changed: wire.Changed, DeletedTS: wire.DeletedTS}
	if (wire.Type == "reaction_added" || wire.Type == "reaction_removed") && wire.Item.Type == "message" {
		event.Reaction = &ReactionChange{wire.Reaction, message.User, wire.Item.Channel, wire.Item.TS, wire.EventTS}
	}
	return event, nil
}
func (c *Client) socketURL(ctx context.Context) (string, error) {
	var response struct {
		URL     string `json:"url"`
		Primary string `json:"primary_websocket_url"`
	}
	err := c.call(ctx, "rtm.connect", nil, &response)
	if err != nil {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || (apiErr.Code != "enterprise_is_restricted" && apiErr.Code != "not_allowed_token_type" && apiErr.Code != "unknown_method" && apiErr.Code != "missing_scope") {
			return "", err
		}
		if err = c.call(ctx, "client.getWebSocketURL", nil, &response); err != nil {
			return "", err
		}
	}
	raw := response.URL
	if raw == "" {
		raw = response.Primary
	}
	if !c.validSocketURL(raw) {
		return "", errors.New("Slack returned an unsupported live endpoint")
	}
	return raw, nil
}
func (c *Client) validSocketURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if u.Scheme == "wss" && (u.Port() == "" || u.Port() == "443") && (host == "slack.com" || strings.HasSuffix(host, ".slack.com")) {
		return true
	}
	// Local HTTP fixtures can exercise the entire transport without real credentials.
	base, err := url.Parse(c.baseURL)
	return err == nil && base.Scheme == "http" && (base.Hostname() == "127.0.0.1" || base.Hostname() == "localhost") && u.Scheme == "ws" && u.Host == base.Host
}
func (c *Client) Listen(ctx context.Context, emit func(LiveEvent)) error {
	raw, err := c.socketURL(ctx)
	if err != nil {
		return err
	}
	headers := http.Header{}
	headers.Set("Cookie", "d="+c.creds.cookie)
	headers.Set("User-Agent", "slackline/0.1")
	client := *c.http
	client.Timeout = 0 // The HTTP timeout must not close an upgraded idle socket.
	dialCtx, cancelDial := context.WithTimeout(ctx, 15*time.Second)
	conn, resp, err := websocket.Dial(dialCtx, raw, &websocket.DialOptions{HTTPClient: &client, HTTPHeader: headers})
	cancelDial()
	if err != nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Slack live connection failed")
	}
	conn.SetReadLimit(maxResponse)
	streamCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-streamCtx.Done():
				return
			case <-ticker.C:
				pingCtx, stop := context.WithTimeout(streamCtx, 5*time.Second)
				err := conn.Ping(pingCtx)
				stop()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); conn.CloseNow(); <-done }()
	helloCtx, stopHello := context.WithTimeout(streamCtx, 10*time.Second)
	_, data, err := conn.Read(helloCtx)
	stopHello()
	if err != nil {
		return errors.New("Slack live handshake failed")
	}
	hello, err := ParseLiveEvent(data)
	if err != nil || hello.Type != "hello" {
		return errors.New("Slack live handshake was not accepted")
	}
	emit(LiveEvent{Type: "_connection", State: "live"})
	for {
		_, data, err := conn.Read(streamCtx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("Slack live connection interrupted")
		}
		event, err := ParseLiveEvent(data)
		if err != nil {
			continue
		}
		switch event.Type {
		case "goodbye", "error":
			return errors.New("Slack requested a live reconnect")
		case "pong", "hello", "reconnect_url":
			continue
		}
		emit(event)
	}
}

// RunLive owns reconnection. Errors remain credential-safe and are never logged raw.
func RunLive(ctx context.Context, api LiveAPI, emit func(LiveEvent)) {
	delay := time.Second
	for ctx.Err() == nil {
		emit(LiveEvent{Type: "_connection", State: "connecting"})
		connectedAt := time.Time{}
		err := api.Listen(ctx, func(event LiveEvent) {
			if event.Type == "_connection" && event.State == "live" {
				connectedAt = time.Now()
			}
			emit(event)
		})
		if ctx.Err() != nil {
			return
		}
		wait := delay
		var rate *RateLimitError
		if errors.As(err, &rate) {
			wait = max(wait, rate.RetryAfter)
		}
		emit(LiveEvent{Type: "_connection", State: "reconnecting", RetryAfter: wait})
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if !connectedAt.IsZero() && time.Since(connectedAt) >= 30*time.Second {
			delay = time.Second
		} else {
			delay = min(30*time.Second, delay*2)
		}
	}
}
