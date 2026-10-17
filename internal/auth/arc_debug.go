package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

const arcInspectURL = "arc://inspect/#remote-debugging"

var debugBrowserPath = regexp.MustCompile(`^/devtools/browser(?:/[a-zA-Z0-9_-]{1,160})?$`)

// Read only the browser's published connection metadata, never Local State,
// cookie databases, passwords, or session storage files.
func arcEndpointFiles(home string) []string {
	base := filepath.Join(home, "Library", "Application Support", "Arc")
	return []string{filepath.Join(base, "User Data", "DevToolsActivePort"), filepath.Join(base, "DevToolsActivePort")}
}
func parseDebugEndpoint(data []byte) (string, error) {
	invalid := errors.New("Arc published invalid debugging metadata; toggle remote debugging off and on")
	if len(data) > 4096 {
		return "", invalid
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		return "", invalid
	}
	portText := strings.TrimSpace(lines[0])
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != portText {
		return "", invalid
	}
	path := strings.TrimSpace(lines[1])
	if !debugBrowserPath.MatchString(path) {
		return "", invalid
	}
	return "ws://" + net.JoinHostPort("127.0.0.1", portText) + path, nil
}
func readDebugEndpoint(paths []string) (string, error) {
	for _, path := range paths {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
			return "", errors.New("cannot read Arc debugging metadata; toggle remote debugging off and on")
		}
		f, err := os.Open(path)
		if err != nil {
			return "", errors.New("cannot read Arc debugging metadata")
		}
		data, err := io.ReadAll(io.LimitReader(f, 4097))
		f.Close()
		if err != nil {
			return "", errors.New("cannot read Arc debugging metadata")
		}
		return parseDebugEndpoint(data)
	}
	return "", os.ErrNotExist
}
func waitDebugEndpoint(ctx context.Context, paths []string) (string, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		endpoint, err := readDebugEndpoint(paths)
		if err == nil {
			return endpoint, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

// attachBrowser never owns Arc's process or tabs. Root commands only enumerate
// targets; WithDetachOnCancel on every attached tab keeps user tabs open.
func attachBrowser(ctx context.Context, endpoint string) (context.Context, func()) {
	alloc, closeAlloc := remote.NewAllocator(ctx, endpoint, remote.NoModifyURL,
		remote.WithDialTimeout(0),
		remote.WithDialHTTPHeader(http.Header{"User-Agent": {"slackline"}}))
	browser, closeBrowser := chromedp.NewContext(alloc, chromedp.WithDetachOnCancel(),
		chromedp.WithLogf(func(string, ...any) {}), chromedp.WithErrorf(func(string, ...any) {}))
	return browser, func() { closeBrowser(); closeAlloc() }
}

// Keep transport diagnostics actionable without returning raw errors: those can
// contain the private debugger endpoint or an arbitrary HTTP response body.
type arcConnectFailure struct {
	status      int
	unreachable bool
}

func (e *arcConnectFailure) Error() string {
	if e.status == 403 {
		return "Arc rejected the debugging connection (HTTP 403). If no approval dialog appeared, this Arc build may not support the approval UI; use --arc-extension or select Chrome with --browser"
	}
	if e.status > 0 {
		return fmt.Sprintf("Arc debugging returned HTTP %d; toggle remote debugging off and on, then retry", e.status)
	}
	if e.unreachable {
		return "Arc's debugging endpoint is no longer reachable; toggle remote debugging off and on, then retry"
	}
	return "Arc debugging connection failed before sign-in; retry or use --arc-extension / --manual"
}

var debugStatus = regexp.MustCompile(`unexpected status "([1-5][0-9]{2})\b`)

func arcConnectionError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return browserError(ctx, "")
	}
	if match := debugStatus.FindStringSubmatch(err.Error()); len(match) > 1 {
		status, _ := strconv.Atoi(match[1])
		return &arcConnectFailure{status: status}
	}
	var op *net.OpError
	if errors.As(err, &op) {
		return &arcConnectFailure{unreachable: true}
	}
	return &arcConnectFailure{}
}
func arcChromeFallback(err error, explicit string) bool {
	var failure *arcConnectFailure
	return explicit == "" && errors.As(err, &failure) && failure.status == 403
}

func arcLogin(ctx context.Context, startURL string) (slack.Credentials, error) {
	paths := arcEndpointFiles(userHome())
	fmt.Println("Arc login uses browser-approved remote debugging; no extension is required.")
	if _, err := readDebugEndpoint(paths); errors.Is(err, os.ErrNotExist) {
		fmt.Println("In Arc, enable ‘Allow remote debugging for this browser instance’ at " + arcInspectURL + ".")
		if err := openLittleArc(ctx, arcInspectURL); err != nil {
			return slack.Credentials{}, err
		}
	}
	endpoint, err := waitDebugEndpoint(ctx, paths)
	if err != nil {
		return slack.Credentials{}, browserError(ctx, "Arc debugging is unavailable; enable it at "+arcInspectURL+", or use --arc-extension / --manual")
	}
	fmt.Println("Requesting a connection to Arc. Approve Arc’s debugging dialog to continue; Ctrl+C cancels.")
	browser, detach := attachBrowser(ctx, endpoint)
	defer detach()
	before, err := chromedp.Targets(browser)
	if err != nil {
		return slack.Credentials{}, arcConnectionError(ctx, err)
	}
	excluded := map[target.ID]bool{}
	for _, info := range before {
		excluded[info.TargetID] = true
	}
	fmt.Println("Opening Little Arc for Slack sign-in. Choose your workspace and open Slack in this new window.")
	if err := openLittleArc(ctx, startURL); err != nil {
		return slack.Credentials{}, err
	}
	creds, err := captureNewSession(browser, excluded)
	if err != nil {
		return slack.Credentials{}, browserError(ctx, "Arc disconnected before this login finished; retry and open Slack in the new login window")
	}
	fmt.Println("Slack session captured. Disconnecting from Arc; your browser and tabs stay open.")
	return creds, nil
}
