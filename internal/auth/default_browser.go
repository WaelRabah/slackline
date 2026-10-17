package auth

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Ask Launch Services without opening a URL or inspecting a browser profile.
// JXA avoids a cgo dependency so the Mac executable remains cross-compilable.
const defaultBrowserScript = `ObjC.import('AppKit');
const app = $.NSWorkspace.sharedWorkspace.URLForApplicationToOpenURL($.NSURL.URLWithString('https://slack.com'));
if (app.isNil()) { '{}'; } else {
    const bundle = $.NSBundle.bundleWithURL(app);
    JSON.stringify({id: ObjC.unwrap(bundle.bundleIdentifier), executable: ObjC.unwrap(bundle.executablePath)});
}`

func systemDefaultBrowser() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", defaultBrowserScript).Output()
	if err != nil {
		return ""
	}
	return compatibleDefaultBrowser(out)
}

func compatibleDefaultBrowser(data []byte) string {
	var app struct {
		ID         string `json:"id"`
		Executable string `json:"executable"`
	}
	if json.Unmarshal(data, &app) != nil || !filepath.IsAbs(app.Executable) {
		return ""
	}
	switch app.ID {
	case "company.thebrowser.Browser", "com.google.Chrome", "com.google.Chrome.beta", "com.google.Chrome.canary",
		"org.chromium.Chromium", "com.brave.Browser", "com.brave.Browser.beta", "com.brave.Browser.nightly",
		"com.microsoft.edgemac", "com.microsoft.edgemac.Beta", "com.microsoft.edgemac.Dev", "com.microsoft.edgemac.Canary":
		return app.Executable
	}
	return ""
}
