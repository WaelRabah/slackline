// Package auth imports a Slack web session through a browser login started by the user.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

const DefaultTimeout = 10 * time.Minute

// Options contains no credentials. Arc attaches with browser-approved debugging;
// other browsers use an isolated temporary profile.
type Options struct {
	ArcExtension bool
	BrowserPath  string
	Workspace    string
	Timeout      time.Duration
}

var slug = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,78}[a-z0-9])?$`)
var teamPath = regexp.MustCompile(`^/client/(T[A-Z0-9]+)(?:/|$)`)

func SignInURL(workspace string) (string, error) {
	workspace = strings.ToLower(strings.TrimSpace(workspace))
	if workspace == "" {
		return "https://slack.com/signin", nil
	}
	if strings.HasPrefix(workspace, "https://") || strings.HasSuffix(workspace, ".slack.com") {
		if !strings.HasPrefix(workspace, "https://") {
			workspace = "https://" + workspace
		}
		u, err := url.Parse(workspace)
		if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || !strings.HasSuffix(u.Host, ".slack.com") {
			return "", errors.New("workspace must be a Slack workspace name or https://name.slack.com")
		}
		workspace = strings.TrimSuffix(u.Host, ".slack.com")
	}
	if !slug.MatchString(workspace) {
		return "", errors.New("workspace must be a Slack workspace name or https://name.slack.com")
	}
	return "https://" + workspace + ".slack.com/", nil
}

func findBrowser(explicit string) (string, error) {
	// An explicit choice bypasses OS detection and always wins.
	if explicit != "" {
		return selectBrowser(explicit, "", nil, exec.LookPath)
	}
	candidates := []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "brave-browser", "microsoft-edge"}
	switch runtime.GOOS {
	case "darwin":
		for _, base := range []string{"/Applications", filepath.Join(userHome(), "Applications")} {
			for _, app := range []string{"Arc.app/Contents/MacOS/Arc", "Google Chrome.app/Contents/MacOS/Google Chrome", "Chromium.app/Contents/MacOS/Chromium", "Brave Browser.app/Contents/MacOS/Brave Browser", "Microsoft Edge.app/Contents/MacOS/Microsoft Edge"} {
				candidates = append(candidates, filepath.Join(base, app))
			}
		}
	case "windows":
		candidates = append(candidates, "chrome.exe", "msedge.exe")
		for _, base := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")} {
			if base != "" {
				candidates = append(candidates, filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"), filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"))
			}
		}
	}

	return selectBrowser("", systemDefaultBrowser(), candidates, exec.LookPath)
}

func selectBrowser(explicit, preferred string, candidates []string, lookup func(string) (string, error)) (string, error) {
	if explicit != "" {
		if path, err := lookup(explicit); err == nil {
			return path, nil
		}
		return "", errors.New("cannot find the selected browser executable; use --browser with its executable path")
	}
	for _, candidate := range append([]string{preferred}, candidates...) {
		if candidate == "" {
			continue
		}
		if path, err := lookup(candidate); err == nil {
			return path, nil
		}
	}
	return "", errors.New("browser login needs Arc, Chrome, Chromium, Brave, or Edge; install one, use --browser, or use auth login --manual")
}

func findIsolatedFallback(home string, lookup func(string) (string, error)) (string, error) {
	candidates := []string{}
	for _, base := range []string{"/Applications", filepath.Join(home, "Applications")} {
		for _, app := range []string{"Google Chrome.app/Contents/MacOS/Google Chrome", "Chromium.app/Contents/MacOS/Chromium", "Brave Browser.app/Contents/MacOS/Brave Browser", "Microsoft Edge.app/Contents/MacOS/Microsoft Edge"} {
			candidates = append(candidates, filepath.Join(base, app))
		}
	}
	return selectBrowser("", "", candidates, lookup)
}
func isolatedBrowserName(path string) string {
	switch filepath.Base(path) {
	case "Google Chrome":
		return "Chrome"
	case "Chromium":
		return "Chromium"
	case "Brave Browser":
		return "Brave"
	case "Microsoft Edge":
		return "Edge"
	}
	return "browser"
}

func userHome() string { home, _ := os.UserHomeDir(); return home }

// Browser returns the Slack token/cookie pair from a user-started login.
// It does not read browser cookie databases, passwords, or storage files.
func Browser(ctx context.Context, opts Options) (slack.Credentials, error) {
	startURL, err := SignInURL(opts.Workspace)
	if err != nil {
		return slack.Credentials{}, err
	}
	path, err := findBrowser(opts.BrowserPath)
	if err != nil {
		return slack.Credentials{}, err
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if timeout < 0 {
		return slack.Credentials{}, errors.New("login timeout must be positive")
	}
	ctx, stop := context.WithTimeout(ctx, timeout)
	defer stop()
	if runtime.GOOS == "darwin" && filepath.Base(path) == "Arc" {
		if opts.ArcExtension {
			return arcExtensionLogin(ctx, startURL)
		}
		creds, arcErr := arcLogin(ctx, startURL)
		if !arcChromeFallback(arcErr, opts.BrowserPath) {
			return creds, arcErr
		}
		fallback, findErr := findIsolatedFallback(userHome(), exec.LookPath)
		if findErr != nil {
			return slack.Credentials{}, arcErr
		}
		fmt.Println("Arc rejected the debugging connection (HTTP 403). Opening a temporary " + isolatedBrowserName(fallback) + " login window instead; Arc remains your default browser.")
		path = fallback
	}
	if opts.ArcExtension {
		return slack.Credentials{}, errors.New("--arc-extension requires Arc as the selected browser")
	}
	// A minimal allocator keeps Chrome's normal protections enabled. The
	// current chromedp release uses a private pipe instead of a listening port.
	alloc, closeAlloc := chromedp.NewExecAllocator(ctx,
		chromedp.ExecPath(path), chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck, chromedp.VisibleWindow,
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-sync", true),
	)
	defer closeAlloc() // also removes the allocator's private temporary profile
	browser, closeBrowser := chromedp.NewContext(alloc,
		chromedp.WithLogf(func(string, ...any) {}),
		chromedp.WithErrorf(func(string, ...any) {}),
	)
	defer closeBrowser()
	if err := chromedp.Do(browser); err != nil {
		return slack.Credentials{}, browserError(ctx, "could not open browser; try auth login --manual")
	}
	// Navigate without waiting for the entire login/SSO page to finish loading.
	if _, err := chromedp.Call(browser, page.Navigate, page.NavigateParams{URL: startURL}); err != nil {
		return slack.Credentials{}, browserError(ctx, "could not open Slack sign-in; try auth login --manual")
	}
	creds, err := captureSession(browser)
	if err != nil {
		return slack.Credentials{}, browserError(ctx, "browser closed before a Slack session was captured; retry or use auth login --manual")
	}
	return creds, nil
}

func browserError(ctx context.Context, fallback string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("browser login timed out; finish signing in and open the workspace in Slack's web client, or use auth login --manual")
	}
	if ctx.Err() != nil {
		return errors.New("browser login cancelled")
	}
	// Never include raw browser errors: they can contain URLs, JS, or sessions.
	return errors.New(fallback)
}

// snapshotExpression returns one active workspace, never an arbitrary token from
// a multi-workspace account. Origin and location are checked inside the browser
// as well as in Go, because a tab may navigate between discovery and evaluation.
const snapshotExpression = `(() => {
    if (location.protocol !== 'https:' || !(location.hostname === 'slack.com' || location.hostname.endsWith('.slack.com'))) return null;
    const match = location.pathname.match(/^\/client\/(T[A-Z0-9]+)(?:\/|$)/);
    if (!match) return null;
    try {
        const config = JSON.parse(localStorage.getItem('localConfig_v2') || '{}');
        const token = config.teams?.[match[1]]?.token;
        if (typeof token !== 'string' || !token.startsWith('xoxc-')) return null;
        return {url: location.origin + location.pathname, team: match[1], token};
    } catch (_) { return null; }
})()`

type snapshot struct {
	URL   string `json:"url"`
	Team  string `json:"team"`
	Token string `json:"token"`
}

func (snapshot) String() string     { return "browser session [redacted]" }
func (s snapshot) GoString() string { return s.String() }

func workspaceID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "slack.com" && !strings.HasSuffix(host, ".slack.com") {
		return ""
	}
	match := teamPath.FindStringSubmatch(u.Path)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func sessionCredentials(s *snapshot, cookies []*network.Cookie) (slack.Credentials, bool) {
	if s == nil || workspaceID(s.URL) == "" || workspaceID(s.URL) != s.Team {
		return slack.Credentials{}, false
	}
	for _, cookie := range cookies {
		if cookie.Name != "d" || (cookie.Domain != ".slack.com" && cookie.Domain != "slack.com") || !cookie.Secure {
			continue
		}
		creds, err := slack.NewCredentials(s.Token, cookie.Value)
		if err == nil {
			return creds, true
		}
	}
	return slack.Credentials{}, false
}

func captureSession(browser context.Context) (slack.Credentials, error) {
	return captureNewSession(browser, nil)
}

// excluded contains all tabs that predate the Arc login. They must never be
// inspected for credentials or closed on completion/cancellation.
func captureNewSession(browser context.Context, excluded map[target.ID]bool) (slack.Credentials, error) {
	type tab struct {
		ctx   context.Context
		close context.CancelFunc
	}
	tabs := map[target.ID]tab{}
	defer func() {
		for _, t := range tabs {
			t.close()
		}
	}()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		probe, stopProbe := context.WithTimeout(browser, 3*time.Second)
		targets, err := chromedp.Targets(probe)
		stopProbe()
		if err != nil {
			if browser.Err() != nil {
				return slack.Credentials{}, browser.Err()
			}
		} else {
			present := map[target.ID]bool{}
			pages := 0
			for _, info := range targets {
				if info.Type == "page" {
					pages++
				}
			}
			if pages == 0 {
				return slack.Credentials{}, errors.New("login browser has no open tabs")
			}
			for _, info := range targets {
				present[info.TargetID] = true
				if excluded[info.TargetID] || info.Type != "page" || workspaceID(info.URL) == "" {
					continue
				}
				t, ok := tabs[info.TargetID]
				if !ok {
					t.ctx, t.close = chromedp.NewContext(browser, chromedp.WithTargetID(info.TargetID), chromedp.WithDetachOnCancel())
					// The first action owns the target event loop's lifetime.
					// Initialize on the login context before using per-probe
					// deadlines; otherwise ending one probe disables every
					// subsequent check while Slack finishes signing in.
					if err := chromedp.Do(t.ctx); err != nil {
						t.close()
						continue
					}
					tabs[info.TargetID] = t
				}
				probe, stop := context.WithTimeout(t.ctx, 3*time.Second)
				s, err := chromedp.Run(probe, chromedp.Evaluate[*snapshot](snapshotExpression))
				if err == nil && s != nil {
					// CDP can read HttpOnly cookies; page JavaScript cannot.
					result, cookieErr := chromedp.Call(probe, network.GetCookies, network.GetCookiesParams{URLs: []string{"https://slack.com/api/"}})
					if cookieErr == nil {
						if creds, ok := sessionCredentials(s, result.Cookies); ok {
							stop()
							return creds, nil
						}
					}
				}
				stop()
			}
			for id, t := range tabs {
				if !present[id] {
					t.close()
					delete(tabs, id)
				}
			}
		}
		select {
		case <-browser.Done():
			return slack.Credentials{}, browser.Err()
		case <-ticker.C:
		}
	}
}
