//go:build browser

package auth

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

// Real Chrome exercises CDP/JS/HttpOnly cookie capture. Every page request is
// fulfilled by fixtures before reaching a network server. No real Slack login.
func fixtureBrowser(t *testing.T) context.Context {
	t.Helper()
	path, err := findBrowser(os.Getenv("SLACKLINE_TEST_BROWSER"))
	if err != nil {
		t.Fatal("browser-tagged tests require an installed Chrome-compatible browser")
	}
	parent, stop := context.WithTimeout(context.Background(), 25*time.Second)
	t.Cleanup(stop)
	allocator, closeAlloc := chromedp.NewExecAllocator(parent,
		chromedp.ExecPath(path), chromedp.Headless, chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck, chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("host-resolver-rules", "MAP * ~NOTFOUND"),
	)
	t.Cleanup(closeAlloc)
	browser, closeBrowser := chromedp.NewContext(allocator, chromedp.WithLogf(func(string, ...any) {}), chromedp.WithErrorf(func(string, ...any) {}))
	t.Cleanup(closeBrowser)
	if err := chromedp.Do(browser); err != nil {
		t.Fatalf("launch fixture browser: %v", err)
	}
	return browser
}

func interceptFixture(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := chromedp.Do(ctx); err != nil {
		t.Fatal(err)
	}
	target := chromedp.FromContext(ctx).Target
	events := chromedp.Events(ctx, fetch.RequestPaused)
	go func() {
		for paused, err := range events {
			if err != nil {
				return
			}
			headers := []*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}
			code := int64(200)
			body := []byte(`<html><title>Sign-in fixture</title></html>`)
			switch {
			case strings.Contains(paused.Request.URL, "/redirect"):
				code = 302
				headers = append(headers, &fetch.HeaderEntry{Name: "Location", Value: "https://app.slack.com/client/TTEST/CTEST"})
			case strings.Contains(paused.Request.URL, "/client/"):
				headers = append(headers, &fetch.HeaderEntry{Name: "Set-Cookie", Value: "d=xoxd-browser-fixture%2Fencoded%3D; Domain=.slack.com; Path=/; Secure; HttpOnly; SameSite=Lax"})
				body = []byte(`<html><title>Workspace fixture</title><script>localStorage.setItem('localConfig_v2',JSON.stringify({teams:{TOTHER:{token:'xoxc-wrong-workspace'},TTEST:{token:'xoxc-browser-fixture'}}}));</script></html>`)
			}
			_, _ = cdp.Call(ctx, target, fetch.FulfillRequest, fetch.FulfillRequestParams{RequestID: paused.RequestID, ResponseCode: code, ResponseHeaders: headers, Body: body})
		}
	}()
	if _, err := chromedp.Call(ctx, fetch.Enable, fetch.EnableParams{}); err != nil {
		t.Fatal(err)
	}
}

func TestBrowserCaptureAcrossTabsAndRedirects(t *testing.T) {
	browser := fixtureBrowser(t)
	interceptFixture(t, browser)
	if err := chromedp.Do(browser, chromedp.Navigate("https://identity.example.test/login")); err != nil {
		t.Fatal(err)
	}
	tab, closeTab := chromedp.NewContext(browser)
	defer closeTab()
	interceptFixture(t, tab)
	if err := chromedp.Do(tab, chromedp.Navigate("https://identity.example.test/redirect")); err != nil {
		t.Fatal(err)
	}
	visible, err := chromedp.Run(tab, chromedp.Evaluate[string](`document.cookie`))
	if err != nil || strings.Contains(visible, "xoxd-") {
		t.Fatal("fixture cookie is not HttpOnly")
	}
	creds, err := captureSession(browser)
	if err != nil {
		t.Fatalf("capture fixture: %v", err)
	}
	if creds.Token() != "xoxc-browser-fixture" || creds.Cookie() != "xoxd-browser-fixture%2Fencoded%3D" {
		t.Fatal("captured wrong workspace or altered cookie")
	}
}

func TestBrowserRejectsNonSlackOrigin(t *testing.T) {
	browser := fixtureBrowser(t)
	interceptFixture(t, browser)
	if err := chromedp.Do(browser, chromedp.Navigate("https://slack.com.attacker.test/client/TTEST/CTEST")); err != nil {
		t.Fatal(err)
	}
	s, err := chromedp.Run(browser, chromedp.Evaluate[*snapshot](snapshotExpression))
	if err != nil || s != nil {
		t.Fatal("snapshot accepted a lookalike Slack origin")
	}
	ctx, cancel := context.WithTimeout(browser, 200*time.Millisecond)
	defer cancel()
	if _, err := captureSession(ctx); err == nil {
		t.Fatal("capture accepted an untrusted tab")
	}
}

// Real sign-in opens the client before its session data has finished loading.
// The first probe must be allowed to miss without destroying later probes.
func TestBrowserCaptureWaitsForSessionReadiness(t *testing.T) {
	browser := fixtureBrowser(t)
	interceptFixture(t, browser)
	if err := chromedp.Do(browser, chromedp.Navigate("https://app.slack.com/client/TTEST/CTEST")); err != nil {
		t.Fatal(err)
	}
	script := `(() => {
        const getItem = Storage.prototype.getItem;
        let checks = 0;
        Storage.prototype.getItem = function(key) {
            if (key === 'localConfig_v2' && ++checks === 1) return null;
            return getItem.call(this,key);
        };
    })()`
	if err := chromedp.Do(browser, chromedp.Evaluate[chromedp.Void](script)); err != nil {
		t.Fatal(err)
	}
	waiting, stop := context.WithTimeout(browser, 5*time.Second)
	defer stop()
	creds, err := captureSession(waiting)
	if err != nil {
		t.Fatalf("session did not recover after first empty probe: %v", err)
	}
	if creds.Token() != "xoxc-browser-fixture" {
		t.Fatal("wrong workspace captured")
	}
}

// A running browser may already have other signed-in workspaces. The new login
// must not import credentials from one of those existing tabs.
func TestArcStyleCaptureIgnoresExistingWorkspaceTabs(t *testing.T) {
	browser := fixtureBrowser(t)
	interceptFixture(t, browser)
	if err := chromedp.Do(browser, chromedp.Navigate("https://app.slack.com/client/TTEST/COLD")); err != nil {
		t.Fatal(err)
	}
	before, err := chromedp.Targets(browser)
	if err != nil {
		t.Fatal(err)
	}
	excluded := map[target.ID]bool{}
	for _, info := range before {
		excluded[info.TargetID] = true
	}
	onlyOld, stop := context.WithTimeout(browser, 150*time.Millisecond)
	_, err = captureNewSession(onlyOld, excluded)
	stop()
	if err == nil {
		t.Fatal("captured an excluded existing workspace")
	}
	// Capture a later tab after the first attempt was cancelled. Detach cleanup
	// must not close the old workspace or the new sign-in tab.
	tab, closeTab := chromedp.NewContext(browser, chromedp.WithDetachOnCancel())
	defer closeTab()
	interceptFixture(t, tab)
	if err := chromedp.Do(tab, chromedp.Navigate("https://app.slack.com/client/TTEST/CNEW")); err != nil {
		t.Fatal(err)
	}
	creds, err := captureNewSession(browser, excluded)
	if err != nil || creds.Token() != "xoxc-browser-fixture" {
		t.Fatal("new login tab not captured")
	}
	after, err := chromedp.Targets(browser)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, info := range after {
		if info.Type == "page" {
			count++
		}
	}
	oldPages := 0
	for _, info := range before {
		if info.Type == "page" {
			oldPages++
		}
	}
	if count < oldPages+1 {
		t.Fatal("capture closed user tabs")
	}
}

func TestArcStyleRemoteDetachLeavesBrowserAndTabsOpen(t *testing.T) {
	path, err := findBrowser(os.Getenv("SLACKLINE_TEST_BROWSER"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	parent, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	alloc, closeAlloc := chromedp.NewExecAllocator(parent, chromedp.ExecPath(path), chromedp.Headless,
		chromedp.NoFirstRun, chromedp.NoDefaultBrowserCheck, remote.WebSocket, chromedp.UserDataDir(dir),
		chromedp.Flag("disable-background-networking", true), chromedp.Flag("host-resolver-rules", "MAP * ~NOTFOUND"))
	defer closeAlloc()
	owner, closeOwner := chromedp.NewContext(alloc, chromedp.WithLogf(func(string, ...any) {}), chromedp.WithErrorf(func(string, ...any) {}))
	defer closeOwner()
	if err := chromedp.Do(owner); err != nil {
		t.Fatal("could not launch owned fixture browser")
	}
	endpoint, err := readDebugEndpoint([]string{filepath.Join(dir, "DevToolsActivePort")})
	if err != nil {
		t.Fatal("fixture did not publish metadata")
	}
	attached, detach := attachBrowser(parent, endpoint)
	targets, err := chromedp.Targets(attached)
	if err != nil {
		detach()
		t.Fatal("could not attach")
	}
	if len(targets) == 0 {
		detach()
		t.Fatal("fixture has no targets")
	}
	existing, release := chromedp.NewContext(attached, chromedp.WithTargetID(chromedp.FromContext(owner).Target.TargetID), chromedp.WithDetachOnCancel())
	if err := chromedp.Do(existing); err != nil {
		release()
		detach()
		t.Fatal("could not attach existing tab")
	}
	release()
	detach()
	targets, err = chromedp.Targets(owner)
	if err != nil || len(targets) == 0 {
		t.Fatal("remote cleanup closed existing browser or tab")
	}
}
