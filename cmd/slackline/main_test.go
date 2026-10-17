package main

import (
	"context"
	"errors"
	"flag"
	"testing"
	"time"
)

func TestLoginOptions(t *testing.T) {
	opts, err := parseLoginOptions(nil)
	if err != nil || opts.manual || opts.browser.Timeout != 10*time.Minute {
		t.Fatal("browser login is not default")
	}
	opts, err = parseLoginOptions([]string{"--arc-extension"})
	if err != nil || !opts.browser.ArcExtension {
		t.Fatal("explicit extension fallback missing")
	}
	opts, err = parseLoginOptions([]string{"--manual"})
	if err != nil || !opts.manual {
		t.Fatal("manual fallback missing")
	}
	opts, err = parseLoginOptions([]string{"--workspace", "team", "--browser", "/custom/chrome", "--timeout", "3m"})
	if err != nil || opts.browser.Workspace != "team" || opts.browser.BrowserPath != "/custom/chrome" || opts.browser.Timeout != 3*time.Minute {
		t.Fatal("browser options not parsed")
	}
	for _, args := range [][]string{{"--manual", "--arc-extension"}, {"--manual", "--workspace", "team"}, {"--manual", "--browser", "chrome"}, {"--timeout", "0s"}, {"--timeout", "-1s"}, {"extra"}, {"--workspace", "https://attacker.test"}} {
		if _, err := parseLoginOptions(args); err == nil {
			t.Error("accepted invalid login options")
		}
	}
	if _, err := parseLoginOptions([]string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Error("missing login help")
	}
}

func TestLoginHelpDoesNotLaunchBrowser(t *testing.T) {
	if err := login(context.Background(), "/does-not-exist/config.toml", []string{"--help"}); err != nil {
		t.Fatal(err)
	}
}
