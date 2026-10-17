package slack

import (
	"errors"
	"strings"
)

// Credentials are Slack web/desktop session credentials, not OAuth app tokens.
// The xoxc token is workspace-specific; its companion d cookie completes auth.
type Credentials struct{ token, cookie string }

func NewCredentials(token, cookie string) (Credentials, error) {
	token = strings.TrimSpace(token)
	cookie = strings.TrimSpace(cookie)
	cookie = strings.TrimPrefix(cookie, "d=")
	if !strings.HasPrefix(token, "xoxc-") || len(token) <= 5 {
		return Credentials{}, errors.New("Slack session token must start with xoxc-; run auth login")
	}
	if strings.ContainsAny(token, " \t\r\n") {
		return Credentials{}, errors.New("invalid Slack token format")
	}
	// Preserve URL encoding verbatim. Decoding/re-encoding can break Slack cookies.
	if cookie == "" {
		return Credentials{}, errors.New("Slack xoxc authentication requires the matching d cookie; run auth login")
	}
	for _, r := range cookie {
		if r <= 0x20 || r >= 0x7f || strings.ContainsRune(";\"\\,", r) {
			return Credentials{}, errors.New("provide only the value of Slack's d cookie, not a full Cookie header")
		}
	}
	return Credentials{token: token, cookie: cookie}, nil
}

func (c Credentials) Token() string    { return c.token }
func (c Credentials) Cookie() string   { return c.cookie }
func (c Credentials) String() string   { return "Slack session [redacted]" }
func (c Credentials) GoString() string { return c.String() }
