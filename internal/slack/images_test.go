package slack

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/WaelRabah/slackline/internal/media"
)

type imageTransport func(*http.Request) (*http.Response, error)

func (f imageTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func imageResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
func TestPrivateImageCredentialsAndPublicEmojiRedirect(t *testing.T) {
	client := NewClient(testCredentials(t))
	calls := 0
	client.http.Transport = imageTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host == "files.slack.com" {
			if r.Header.Get("Authorization") != "Bearer xoxc-test-session" || r.Header.Get("Cookie") != "d=xoxd-test%2Fencoded%3D" {
				t.Error("private image session missing")
			}
			response := imageResponse(302, "")
			response.Header.Set("Location", "https://emoji.slack-edge.com/public.png")
			response.Request = r
			return response, nil
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials leaked to public CDN")
		}
		return imageResponse(200, "image bytes"), nil
	})
	data, err := client.Image(context.Background(), "https://files.slack.com/private.png?signature=private-fixture")
	if err != nil || string(data) != "image bytes" || calls != 2 {
		t.Fatal("private/public preview transport failed")
	}
}
func TestImageURLAndRedirectBoundaries(t *testing.T) {
	client := NewClient(testCredentials(t))
	calls := 0
	client.http.Transport = imageTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		response := imageResponse(302, "")
		response.Header.Set("Location", "https://external.example/image?private-fixture")
		response.Request = r
		return response, nil
	})
	for _, raw := range []string{"http://files.slack.com/a", "https://files.slack.com.evil.test/a", "https://127.0.0.1/a", "https://user:secret@files.slack.com/a", "https://files.slack.com:444/a", "file:///tmp/image", "https://files.slack.com/a#fragment"} {
		if _, err := client.Image(context.Background(), raw); err == nil {
			t.Fatal("accepted unsafe preview URL")
		}
	}
	if calls != 0 {
		t.Fatal("untrusted URL made a request")
	}
	_, err := client.Image(context.Background(), "https://files.slack.com/private.png")
	if err == nil || calls != 1 || strings.Contains(err.Error(), "private-fixture") {
		t.Fatal("redirect leaked credentials, signed URL, or made an external request")
	}
}
func TestImageSizeAndErrorsAreBoundedAndRedacted(t *testing.T) {
	client := NewClient(testCredentials(t))
	client.http.Transport = imageTransport(func(*http.Request) (*http.Response, error) {
		response := imageResponse(200, "")
		response.ContentLength = media.MaxBytes + 1
		return response, nil
	})
	if _, err := client.Image(context.Background(), "https://files.slack.com/a"); err == nil {
		t.Fatal("accepted excessive content length")
	}
	client.http.Transport = imageTransport(func(*http.Request) (*http.Response, error) {
		return imageResponse(200, strings.Repeat("x", media.MaxBytes+1)), nil
	})
	if _, err := client.Image(context.Background(), "https://files.slack.com/a"); err == nil {
		t.Fatal("accepted excessive streamed body")
	}
	client.http.Transport = imageTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret-ticket xoxc-private") })
	_, err := client.Image(context.Background(), "https://files.slack.com/a?ticket=private-fixture")
	if err == nil || strings.Contains(err.Error(), "xoxc-") || strings.Contains(err.Error(), "private-fixture") || strings.Contains(err.Error(), "secret-ticket") {
		t.Fatal("preview error leaked credentials")
	}
}
