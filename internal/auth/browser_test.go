package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
)

func TestSignInURL(t *testing.T) {
	for input, want := range map[string]string{
		"": "https://slack.com/signin", "team": "https://team.slack.com/", "team-name": "https://team-name.slack.com/",
		"TEAM.slack.com": "https://team.slack.com/", "https://team.slack.com/": "https://team.slack.com/",
	} {
		got, err := SignInURL(input)
		if err != nil || got != want {
			t.Errorf("valid workspace rejected: %v", err)
		}
	}
	for _, input := range []string{"https://slack.com.attacker.test", "https://team.slack.com@attacker.test", "https://attacker.test@team.slack.com", "http://team.slack.com", "https://team.slack.com:8443", "https://team.slack.com/?token=secret", "https://team.slack.com/#fragment", "https://team.slack.com/path", "nested.team.slack.com", "team:443", "team/../other", "-team", "team-"} {
		if _, err := SignInURL(input); err == nil {
			t.Error("accepted an unsafe workspace URL")
		}
	}
}

func TestWorkspaceIDRequiresSlackClientOrigin(t *testing.T) {
	for _, u := range []string{"https://app.slack.com/client/T123/C456", "https://team.slack.com/client/T123", "https://app.slack.com:443/client/T123/"} {
		if workspaceID(u) != "T123" {
			t.Error("valid client URL rejected")
		}
	}
	for _, u := range []string{"http://app.slack.com/client/T123", "https://slack.com.attacker.test/client/T123", "https://attacker.test/client/T123", "https://app.slack.com:444/client/T123", "https://user@app.slack.com/client/T123", "https://app.slack.com/signin", "https://app.slack.com/client/T123.invalid", "https://app.slack.com/client/"} {
		if workspaceID(u) != "" {
			t.Error("accepted an untrusted or incomplete client URL")
		}
	}
}

func TestSessionRequiresMatchingWorkspaceAndSlackCookie(t *testing.T) {
	s := &snapshot{URL: "https://app.slack.com/client/T123", Team: "T123", Token: "xoxc-fixture"}
	good := &network.Cookie{Name: "d", Domain: ".slack.com", Value: "xoxd-fixture%2Fvalue%3D", Secure: true, HTTPOnly: true}
	credentials, ok := sessionCredentials(s, []*network.Cookie{good})
	if !ok || credentials.Cookie() != good.Value {
		t.Fatal("valid session rejected or cookie changed")
	}
	for _, cookie := range []*network.Cookie{
		{Name: "d", Domain: "attacker.test", Value: "secret", Secure: true},
		{Name: "other", Domain: ".slack.com", Value: "secret", Secure: true},
		{Name: "d", Domain: ".slack.com", Value: "secret", Secure: false},
		{Name: "d", Domain: ".slack.com", Value: "bad; cookie", Secure: true},
	} {
		if _, ok := sessionCredentials(s, []*network.Cookie{cookie}); ok {
			t.Error("accepted invalid cookie")
		}
	}
	for _, s := range []*snapshot{nil, {URL: "https://app.slack.com/client/T123", Team: "TOTHER", Token: "xoxc-fixture"}, {URL: "https://attacker.test/client/T123", Team: "T123", Token: "xoxc-fixture"}, {URL: "https://app.slack.com/client/T123", Team: "T123", Token: "xoxp-fixture"}} {
		if _, ok := sessionCredentials(s, []*network.Cookie{good}); ok {
			t.Error("accepted incomplete or mismatched session")
		}
	}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", *s, *s, *s), "xoxc-fixture") {
		t.Fatal("snapshot formatting leaked session")
	}
}

func TestBrowserErrorsAreActionableAndCredentialFree(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	timeout, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	if !strings.Contains(browserError(cancelled, "fallback").Error(), "cancelled") {
		t.Error("missing cancellation error")
	}
	if !strings.Contains(browserError(timeout, "fallback").Error(), "timed out") {
		t.Error("missing timeout error")
	}
	if _, err := findBrowser("/does-not-exist/xoxc-secret"); err == nil || strings.Contains(err.Error(), "xoxc-secret") {
		t.Error("browser path error leaked raw input")
	}
}

func TestDefaultBrowserSelection(t *testing.T) {
	arc := "/Applications/Arc.app/Contents/MacOS/Arc"
	chrome := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	lookup := func(path string) (string, error) {
		if path == arc || path == chrome {
			return path, nil
		}
		return "", errors.New("not installed")
	}
	got, err := selectBrowser("", arc, []string{chrome}, lookup)
	if err != nil || got != arc {
		t.Fatal("system default did not take priority")
	}
	got, err = selectBrowser(chrome, arc, nil, lookup)
	if err != nil || got != chrome {
		t.Fatal("explicit browser did not take priority")
	}
	got, err = selectBrowser("", "/missing/default", []string{chrome}, lookup)
	if err != nil || got != chrome {
		t.Fatal("missing default did not fall back")
	}
	if _, err := selectBrowser("/missing/explicit", arc, []string{chrome}, lookup); err == nil {
		t.Fatal("missing explicit choice silently fell back")
	}
}

func TestCompatibleDefaultBrowser(t *testing.T) {
	arc := compatibleDefaultBrowser([]byte(`{"id":"company.thebrowser.Browser","executable":"/Applications/Arc.app/Contents/MacOS/Arc"}`))
	if arc != "/Applications/Arc.app/Contents/MacOS/Arc" {
		t.Fatal("Arc not recognized")
	}
	for _, input := range []string{`{}`, `invalid JSON`, `{"id":"com.apple.Safari","executable":"/Applications/Safari.app/Contents/MacOS/Safari"}`, `{"id":"org.mozilla.firefox","executable":"/Applications/Firefox.app/Contents/MacOS/firefox"}`, `{"id":"company.thebrowser.Browser","executable":"relative/Arc"}`} {
		if compatibleDefaultBrowser([]byte(input)) != "" {
			t.Fatal("unsupported default was selected")
		}
	}
}
