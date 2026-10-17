package slack

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/WaelRabah/slackline/internal/media"
)

type File struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Title      string `json:"title"`
	Mimetype   string `json:"mimetype"`
	URLPrivate string `json:"url_private"`
	Thumb360   string `json:"thumb_360"`
	Thumb720   string `json:"thumb_720"`
}

func (f File) PreviewURL() string {
	for _, value := range []string{f.Thumb360, f.Thumb720, f.URLPrivate} {
		if value != "" {
			return value
		}
	}
	return ""
}

type Attachment struct {
	Title    string `json:"title"`
	Fallback string `json:"fallback"`
	ImageURL string `json:"image_url"`
	ThumbURL string `json:"thumb_url"`
}
type ImageBlock struct {
	Type      string       `json:"type"`
	ImageURL  string       `json:"image_url"`
	AltText   string       `json:"alt_text"`
	Accessory *ImageBlock  `json:"accessory"`
	Elements  []ImageBlock `json:"elements"`
}

type ImageAPI interface {
	Image(context.Context, string) ([]byte, error)
}

// URLs come from workspace content. Private credentials are attached only to
// Slack's file hosts; custom emoji CDN requests are public and credential-free.
func imageHost(raw string) (private bool, valid bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return false, false
	}
	switch strings.ToLower(u.Hostname()) {
	case "files.slack.com", "files-origin.slack.com":
		return true, true
	case "emoji.slack-edge.com":
		return false, true
	}
	return false, false
}
func (c *Client) Image(ctx context.Context, raw string) ([]byte, error) {
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for range 4 {
		private, valid := imageHost(raw)
		if !valid {
			return nil, errors.New("preview is available only for Slack-hosted images")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, errors.New("cannot create image preview request")
		}
		if private {
			req.Header.Set("Authorization", "Bearer "+c.creds.token)
			req.AddCookie(&http.Cookie{Name: "d", Value: c.creds.cookie})
		}
		response, err := client.Do(req)
		if err != nil {
			return nil, errors.New("image preview download failed")
		}
		switch response.StatusCode {
		case 301, 302, 303, 307, 308:
			next, err := response.Location()
			response.Body.Close()
			if err != nil {
				return nil, errors.New("image preview redirect was invalid")
			}
			raw = next.String()
			continue
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, errors.New("image preview unavailable; reload the conversation to retry")
		}
		if response.ContentLength > media.MaxBytes {
			response.Body.Close()
			return nil, errors.New("image exceeds the 8 MiB preview limit")
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, media.MaxBytes+1))
		response.Body.Close()
		if err != nil {
			return nil, errors.New("image preview download failed")
		}
		if len(data) > media.MaxBytes {
			return nil, errors.New("image exceeds the 8 MiB preview limit")
		}
		return data, nil
	}
	return nil, errors.New("image preview redirected too many times")
}
