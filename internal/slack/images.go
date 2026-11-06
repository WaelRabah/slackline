package slack

import (
	"context"
	"encoding/json"
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
	Filetype   string `json:"filetype"`
	Permalink  string `json:"permalink"`
	URLPrivate string `json:"url_private"`
	Thumb360   string `json:"thumb_360"`
	Thumb720   string `json:"thumb_720"`
	ThumbVideo string `json:"thumb_video"`
	Size       int64  `json:"size"`
}

func (f File) IsImage() bool {
	return strings.HasPrefix(f.Mimetype, "image/") || imageFiletypes[f.Filetype]
}

func (f File) IsVideo() bool {
	return strings.HasPrefix(f.Mimetype, "video/") || videoFiletypes[f.Filetype]
}

func (f File) IsAudio() bool {
	return strings.HasPrefix(f.Mimetype, "audio/") || audioFiletypes[f.Filetype]
}

var (
	imageFiletypes = map[string]bool{"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true, "heic": true}
	videoFiletypes = map[string]bool{"mp4": true, "mov": true, "webm": true, "mkv": true, "m4v": true}
	audioFiletypes = map[string]bool{"mp3": true, "m4a": true, "wav": true, "ogg": true, "flac": true}
)

// PreviewURL never returns a video's URLPrivate: only still images can be
// decoded by the preview pipeline.
func (f File) PreviewURL() string {
	for _, value := range []string{f.Thumb360, f.Thumb720, f.ThumbVideo} {
		if value != "" {
			return value
		}
	}
	if f.IsImage() {
		return f.URLPrivate
	}
	return ""
}

type Attachment struct {
	Title        string `json:"title"`
	Pretext      string `json:"pretext"`
	Text         string `json:"text"`
	Fallback     string `json:"fallback"`
	ImageURL     string `json:"image_url"`
	ThumbURL     string `json:"thumb_url"`
	FromURL      string `json:"from_url"`
	OriginalURL  string `json:"original_url"`
	VideoURL     string `json:"video_url"`
	ProviderName string `json:"provider_name"`
	ServiceName  string `json:"service_name"`
}

func (a Attachment) LinkURL() string {
	for _, value := range []string{a.OriginalURL, a.FromURL} {
		if value != "" {
			return value
		}
	}
	return ""
}

type ImageBlock struct {
	Type         string          `json:"type"`
	ImageURL     string          `json:"image_url"`
	ThumbnailURL string          `json:"thumbnail_url"`
	VideoURL     string          `json:"video_url"`
	AltText      string          `json:"alt_text"`
	Text         json.RawMessage `json:"text"`
	Accessory    *ImageBlock     `json:"accessory"`
	Elements     []ImageBlock    `json:"elements"`
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

// Previewable reports whether Image can fetch raw. External thumbnails (for
// example YouTube unfurls) are rendered as links instead of error blocks.
func Previewable(raw string) bool {
	if raw == "demo://landscape" {
		return true
	}
	_, valid := imageHost(raw)
	return valid
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
