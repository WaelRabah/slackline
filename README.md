# Slackline

Slack + command line = Slackline. A Slack terminal client built with Go and Bubble Tea. The first implementation supports session login, joined channels and existing DMs, message history and older pages, sending, clearly framed message blocks, per-conversation drafts, Vim-style navigation, and a local fuzzy conversation finder.

## Install

Download the package matching your operating system and CPU from [GitHub Releases](https://github.com/WaelRabah/slackline/releases). `arm64` is Apple Silicon / ARM64; `amd64` is Intel / x86-64.

| Platform | Installer | Installs the command to |
| --- | --- | --- |
| macOS | `slackline_<version>_darwin_<arch>.pkg` | `/usr/local/bin/slackline` |
| Debian / Ubuntu | `slackline_<version>_linux_<arch>.deb` | `/usr/bin/slackline` |
| Fedora / RHEL | `slackline_<version>_linux_<arch>.rpm` | `/usr/bin/slackline` |
| Either OS | `slackline_<version>_<os>_<arch>.tar.gz` | Extract and install to a directory on `PATH` |

For example:

```sh
# macOS; select the actual downloaded version and architecture.
sudo installer -pkg ./slackline_<version>_darwin_arm64.pkg -target /
# Debian / Ubuntu
sudo apt install ./slackline_<version>_linux_amd64.deb
# Fedora
sudo dnf install ./slackline_<version>_linux_amd64.rpm

slackline --version
slackline auth login
slackline
```

The macOS packages are currently **unsigned and not notarized**; Developer ID signing credentials are not configured. These are command-line installers, not GUI `.app` bundles. Archives are also available for a manual install, e.g. `install -m 0755 slackline "$HOME/.local/bin/slackline"` after extraction (ensure that directory exists and is on your `PATH`). Published downloads include `checksums.txt`; verify with `shasum -a 256 -c checksums.txt` on macOS or `sha256sum -c checksums.txt` on Linux after downloading all listed files.

Installers do not contain credentials or overwrite your personal configuration. Defaults live under `~/.config/slackline`; existing `slack_in_term` configuration and state remain compatible.

## Run

With Go 1.26 or newer:

```sh
go build -o bin/slackline ./cmd/slackline
./bin/slackline --demo
./bin/slackline auth login
./bin/slackline
```

The offline demo needs no credentials and makes no network requests. Its messages live only in memory. Private conversation summaries and provider/theme settings are available. Workspace assistant retrieval, daily digests, indexing, and semantic search remain planned. Unread grouping, read synchronization, and a followed-thread feed are available. A live WebSocket updates open conversations and replies immediately. Silent background polls reconcile unread/history state; `R` also refreshes manually.

Without a local Go installation, use Docker Compose:

```sh
docker compose run --rm dev go mod download
docker compose run --rm dev go test -race ./...
docker compose run --rm dev go vet ./...
docker compose run --rm dev sh -c 'GOOS=darwin GOARCH=arm64 go build -o bin/slackline ./cmd/slackline'
./bin/slackline --demo
```

For Intel Macs use `GOARCH=amd64`. The container only mounts this project and build caches; it does not mount your host credentials. Compose can also run the offline demo interactively: `docker compose run --rm dev go run ./cmd/slackline --demo`.

## Session authentication

Use an `xoxc-…` token with the value of the `d` cookie from the **same signed-in Slack session**. This follows Slack's web/desktop session model; no Slack app or OAuth installation is required for this implementation. These session interfaces are internal and may change. Workspace restrictions still apply.

Run `./bin/slackline auth login`. On macOS the client prefers your compatible system default browser. Arc attempts browser-approved remote debugging when enabled; if Arc rejects it with HTTP 403, default login recovers through an installed Chrome/Chromium/Brave/Edge temporary window. Arc remains the system default. Other supported browsers use a fresh temporary window, sign-in normally, then capture and verify the session automatically.

```sh
./bin/slackline auth login
./bin/slackline auth login --workspace your-team
./bin/slackline auth check
```

**Arc limitation confirmed on this Mac:** Arc 1.166.0 / Chromium 154 publishes a working loopback endpoint after enabling `arc://inspect/#remote-debugging`, but immediately returns **HTTP 403: Connection rejected**, without showing an approval dialog. The settings checkbox alone does not establish support for the complete approval flow. Bringing Arc to the foreground did not resolve it. The missing approval UI appears to be an Arc integration limitation; the exact Arc-side cause is unconfirmed.

Default `auth login` now detects that rejection and opens an installed Chrome/Chromium/Brave/Edge temporary login window instead. No extension is needed and your system default stays Arc. Errors distinguish HTTP rejection from an unreachable/stale endpoint, cancellation, and other transport failures; raw endpoints and response bodies stay private. Explicit `--browser /Applications/Arc.app/Contents/MacOS/Arc` preserves the choice and returns the Arc error instead of switching browsers. Ctrl+C cancellation never triggers fallback.

To skip the Arc attempt entirely:

```sh
./bin/slackline auth login --browser "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
```

For an Arc build that implements approval dialogs, `auth login` can still attach to its running instance: enable **Allow remote debugging for this browser instance** at `arc://inspect/#remote-debugging`, approve the connection, then sign into Slack in the new Little Arc window. The client captures only Slack tabs created during this login and disconnects without closing Arc or its tabs. It reads only published `DevToolsActivePort` connection metadata and accesses Slack through the approved browser connection; it does not read cookie/password databases or browser storage files. This connection mode was introduced in [Chrome 144](https://developer.chrome.com/blog/chrome-devtools-mcp-debug-your-browser-session), but Arc's UI implementation must also support it.

The old helper remains an explicit fallback: `auth login --arc-extension`. For that mode, enable Developer mode at `arc://extensions`, load this project's `browser-extension` folder unpacked, and click **Connect Slack** on the local login page. The extension posts directly to a one-time loopback callback. Neither flow puts credentials in URLs or terminal output.

Chrome, Chromium, Brave, and Edge use a fresh temporary profile without accessing your normal browser profile. That temporary profile is removed when login finishes or is cancelled. Use `--browser /path/to/browser-executable` to override selection, or `--timeout 15m` to change the default 10-minute wait. Run browser login on the host, rather than in the development container.

A normal redirect to localhost cannot carry Slack's session cookie: cookies are restricted to their domain, and `HttpOnly` cookies cannot be read by page JavaScript. The isolated browser flow reads the cookie locally through DevTools over a private pipe; the default Arc flow uses its explicitly approved local debugging connection. The optional extension flow uses a one-time local POST callback. Credentials are never put into callback URLs, browser history, or terminal output. An OAuth redirect would be a separate app-based authentication flow and would not provide this `xoxc`/`d` pair. See [browser cookie rules](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Set-Cookie).

Some SSO providers or workspace policies may reject a controlled browser. Use the manual fallback in that case:

```sh
./bin/slackline auth login --manual
```

1. Sign into Slack in your regular browser and open Developer Tools.
2. In **Network**, inspect a Slack API request such as `conversations.history`. Copy its `token` form field beginning with `xoxc-`.
3. In **Application / Storage → Cookies**, copy the value of the cookie named exactly `d` for the Slack domain.
4. Paste both values into the hidden terminal prompts. They must come from the same session.

The cookie may start with `xoxd-` and contain percent escapes: paste its exact value without decoding it. A leading `d=` is accepted. Do not paste a full Cookie header. If a session expires or is revoked, repeat login. The existing Arc session is accessed only through browser-approved debugging or the explicitly chosen helper. Direct extraction from browser profile databases or the Slack desktop app is not implemented.

Existing installations automatically reuse `~/.config/slack_in_term/config.toml` (and its adjacent state) until a new Slackline config is created. No credentials need to be copied or re-entered for the rename.

Credentials are stored at `~/.config/slackline/config.toml` (or under `$XDG_CONFIG_HOME`) with mode `0600`. Existing AI and other config values survive login; TOML comments and formatting are normalized. `--config /path/to/config.toml` selects another file. `SLACK_TOKEN` and `SLACK_COOKIE` override config independently, including when explicitly empty. Do not put session values in command arguments, screenshots, logs, or Git.

See [config.example.toml](config.example.toml) for the configuration structure. Provider settings support on-demand private conversation summaries, with explicit enablement in Settings; populated keys alone do not activate inference.

## Keys

| Key | Action |
| --- | --- |
| `h` / `l` | Focus channels / messages |
| `Tab` / `Shift+Tab` | Cycle channels, messages, composer |
| `j` / `k`, arrows | Select conversations or message blocks |
| `Enter` | Fold/unfold a section heading, open a conversation, or open a message thread |
| `Space` in sidebar | Collapse/expand a section; folding a conversation selects its heading |
| `f` in normal mode | Toggle recent conversations / all conversations |
| `m` in normal mode | Mute/unmute notification sounds |
| `Backspace` in a thread | Return to the channel, or Threads if opened from that feed |
| `gg` / `G` | First / last sidebar row, or top / bottom message block |
| `Ctrl+u` / `Ctrl+d` | Scroll half a page |
| `/`, `Ctrl+p`, or `Cmd+p` | Find conversation names and workspace users; `? text` searches messages |
| `Ctrl+j` / `Ctrl+k` in finder | Select a match |
| `i`, `a`, `I`, `A` | Enter insert mode in the composer |
| `Esc` | Close the rightmost pane, including while composing; thread → conversation → list |
| `h`, `l`, `w`, `b`, `0`, `$`, `x` in normal composer | Move or delete text |
| `Enter` in insert mode | Send the typed message |
| `r` / `R` | Reply to selected message in its thread / reload current conversation |
| `s` / `S` | Generate or refresh private 14-day summary / expand the pinned summary |
| `,` or forwarded `Cmd+,` | Open Settings; `Ctrl+s` saves, `Esc` cancels |
| `L` | Load an older history/replies page, or older followed threads |
| `q` in normal mode / `Ctrl+c` anywhere | Quit |

The sidebar uses native Slack section names/memberships where available, then groups remaining accessible conversations under Channels, Private channels, Direct messages, and Group DMs. Navigate headings and conversations with `j`/`k` or arrows. Folded headings remain selectable, including when every section is collapsed. Section counts reflect the current filter. Folded sections, filter choice, sound preference, and recent local interactions are saved on exit in a private `state-*.json` beside the config, separately for each workspace/user. The offline demo does not save state. This relies on Slack’s internal `users.channelSections.list` endpoint; if it is unavailable, type grouping still works. Unmapped membership entries fall back to type groups so conversations are not hidden.

The sidebar defaults to the last **14 days of interaction**: recently read messages reported by Slack, messages/replies you sent (using `search.messages`), and conversations opened/read or posted to in this client. A busy channel you have not interacted with does not qualify merely because other people posted. Slack provides the timestamp of the last message read, not an exact historical visit time: visiting an old inactive conversation in another client cannot be reliably detected. Local opens are tracked from now on. The cutoff rolls with the clock; read activity is checked every 15 seconds, authored-message search every 15 minutes. If initial metadata is unavailable or authored-message search fails, the sidebar shows all conversations until a successful retry. Slack's own search filters/retention can affect authored-message results.

Press `f` to show all conversations. `/`, Ctrl+P, and forwarded Cmd+P always search all loaded conversations and workspace users, even when the recent filter or a collapsed section hides it; opening a result adds it to recent interactions and expands its section. Set `[sidebar] recent_days = 0` to start with all conversations, or choose another number of days.

Sounds are enabled by default and play only for an actual new incoming `message` event from another sender. macOS uses **Glass**, other systems use the terminal bell. Polls, startup snapshots, connection/heartbeat events, typing, edits, deletions, own posts, and replayed message IDs stay silent. `m` mutes/unmutes; `[notifications] enabled = false` overrides the saved preference. These are in-app sound notifications; OS notification-center banners and Slack's notification/mute schedule are not imported.

The client automatically starts a live WebSocket using the saved xoxc/d session (`rtm.connect`, with the desktop endpoint as a fallback where needed). The footer shows **LIVE** after the authenticated handshake, or **connecting/reconnecting · polling** while it retries. It uses heartbeats, cancellation on exit, bounded reconnect backoff, and credential-safe errors. Signed socket URLs are never printed. New messages/replies merge directly into the active view; edits/deletions update it without chiming. Slow history/count/feed responses cannot overwrite newer live events. The 15-second activity poll and 30-second followed-thread poll remain silent recovery paths. If live access is unavailable, polling continues, but event-only sounds and typing are unavailable until reconnection.

Slack's real [user_typing events](https://docs.slack.dev/reference/events/user_typing/) show **Alice is typing…** above the composer and `[typing]` beside the relevant sidebar conversation. Multiple typers are grouped, own typing is ignored, and indicators expire six seconds after the last event or clear when that sender posts/disconnects. Thread scope is honored when Slack includes `thread_ts`; unscoped events are shown for the channel. These indicators receive real events; the client does not fabricate activity or broadcast your keystrokes.

Pane borders and SELECTED labels show keyboard focus. Message blocks have a separate selection outline. Enter splits the area beside the sidebar into conversation and replies panes. Tab cycles sidebar → conversation → replies → composer. Both message panes stay live. Lowercase `r` on a selected block opens its thread and focuses its reply composer. Esc closes the rightmost pane, even from insert mode; a second Esc closes the conversation. Backspace also closes a thread. Typing in the thread composer posts a thread reply. Channel and thread drafts remain separate.

**AI assistant** and **AI summaries** stay at the top of the sidebar, followed by **Threads**. They remain visible while scrolling groups on normal-height terminals; short terminals scroll all rows so the selected row stays visible. The AI entries are local destinations. Workspace assistant retrieval and daily digest scheduling remain disabled and are described explicitly there; configuring a provider only enables the on-demand conversation-summary feature. See [AI feature scope](docs/ai-features.md).

Open **Settings** with `,` (or forwarded Cmd+,). Configure an OpenAI-compatible base URL, masked API key, chat model, optional future embedding model, and explicitly enable conversation summaries. Settings also offers Mint, Ocean, and Amber accent themes, message sounds, and the recent-conversation window. Enter edits/toggles a field, Ctrl+S saves privately and applies immediately, Esc cancels. Existing Slack credentials and unknown config keys survive saves. Demo settings are temporary and inference stays offline.

In a conversation, press `s` to generate a private catch-up: **what's happening**, **decisions and blockers**, and **what's next** with source references. This explicitly sends the conversation's accessible text from the rolling last 14 days to your configured inference provider. Collection uses Slack search, including recent replies on older roots. Context is bounded to 1,000 search matches and 100 KB of transcript; truncated coverage is labeled **PARTIAL**, and Slack search filters/retention may also limit results. It does not mark retrieved messages read. The summary stays pinned at the bottom above the composer while messages scroll; `S` opens its full text with j/k and half-page scrolling. It is saved in the private workspace/user state file and is never posted or pinned in Slack. Each summary shows its generation time; it updates only when you press `s` again. Provider HTTP errors are displayed without credential-bearing bodies, and redirects are refused.

The finder defaults to fuzzy names/users across the loaded workspace directory, irrespective of sidebar filters or collapsed sections. Selecting a user opens/resumes their DM through [conversations.open](https://docs.slack.dev/reference/methods/conversations.open/) without posting a message. Type `? release plan` then Enter for a [Slack message search](https://docs.slack.dev/reference/methods/search.messages/) over the last 14 days; Enter on a result opens that conversation or thread. Message searches show at most the newest 100 matches; refine the query if more exist. This is text search, with no embedding provider required.

Unread conversations have amber highlighting, a `●` marker, and a right-aligned numeric badge. They appear once in **Unread**, even outside the recent filter. Opening one successfully clears its local badge and returns it to its normal section. Failed history loads keep the unread state. New message blocks use a warm background/border and a `NEW` label until the read marker is acknowledged. Read positions persist locally and sync to Slack through [conversations.mark](https://docs.slack.dev/reference/methods/conversations.mark/) or the session thread-mark endpoint, coalesced over five seconds. Startup's automatically displayed conversation is not marked read until explicitly opened. Read-sync failures are shown in the status line; local read state remains available. Quit before the debounce expires can leave Slack's remote marker unchanged; reopening that conversation retries synchronization.

Slack's activity snapshot provides an unread flag, not an exact total for every channel. Counts load in the background from history, serially, with rate-limit cooldowns. `1+` initially means at least one unread; `N+` means a lower bound when the bounded scan is incomplete (up to five pages per snapshot). Exact counts have no plus sign. New arrivals invalidate a cached count, and old count responses cannot restore an already-read badge. The count covers visible top-level conversation messages from other users; thread replies are tracked in Threads.

**Threads** uses Slack's session `subscriptions.thread.getView` feed for threads you follow or participate in, across channels. It shows root messages, latest-reply previews, unread reply counts, and selection. `j/k` select a thread; Enter loads its replies and enables the thread composer. Backspace returns to the feed and retains its selection/draft. `R` refreshes, `L` pages older threads. Live reply events update cached threads immediately; the feed also polls every 30 seconds for silent reconciliation. Only actual incoming message events can chime. Internal session endpoints can change or be restricted; errors are shown with an `R` retry hint. Opening a thread marks only loaded replies through their newest timestamp, not the rest of its channel.

Cmd+T has no app association. Some terminals reserve Cmd+P; configure yours to forward Super+p or map Cmd+P to send Ctrl+P. The client cannot override a terminal shortcut it never receives.

Vim support starts with these navigation and editing motions; full Vim operators are not implemented. Drafts survive conversation switches and failed sends for the current process. Writes are never retried automatically. After a network failure, refresh the conversation before resending because Slack may already have accepted the message. Slack `Retry-After` responses are surfaced and further UI requests pause until the cooldown ends. Pagination is cursor-based; rate limits are not assumed.

## Development

```sh
gofmt -w cmd internal
go test -race ./...
go vet ./...
```

Tests use local HTTP fixtures and the demo backend, including session cookie transport, redirect rejection, pagination, redaction, config permissions, stale history responses, and draft retention. They do not contact Slack or post real messages.

Browser integration tests use synthetic Slack pages, redirects, and an HttpOnly cookie in a real headless browser; fixture requests never reach Slack. With Go and a Chrome-compatible browser installed, run `go test -tags browser ./internal/auth`. On this Apple Silicon Mac without Go, cross-build and run them:

```sh
docker compose run --rm dev sh -c 'GOOS=darwin GOARCH=arm64 go test -c -tags browser -o bin/browser-auth.test ./internal/auth'
SLACKLINE_TEST_BROWSER="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" ./bin/browser-auth.test -test.v -test.timeout 60s
```

Authentication reference: [Slackdump manual session authentication](https://github.com/rusq/slackdump/blob/master/doc/login-manual.md). API references: [users.conversations](https://docs.slack.dev/reference/methods/users.conversations/), [conversations.history](https://docs.slack.dev/reference/methods/conversations.history/), [chat.postMessage](https://docs.slack.dev/reference/methods/chat.postMessage/).

The Arc extension callback and background worker have separate checks: `go test ./internal/auth` and `node --test browser-extension/tests/background.test.cjs`.

An opt-in, receive-only live-session check is separate from ordinary tests. It reports only connection success and event counts, never message contents or socket URLs. On this Mac:

```sh
docker compose run --rm dev sh -c 'GOOS=darwin GOARCH=arm64 go test -c -tags live -o bin/slack-live.test ./internal/slack'
SLACKLINE_LIVE_CHECK=1 SLACKLINE_LIVE_HEARTBEAT_CHECK=1 ./bin/slack-live.test -test.run '^TestRealLiveConnection$' -test.v -test.timeout 55s
```

The heartbeat check observes the connection for 45 seconds, without sending Slack messages, typing events, or read markers.

## CI and releases

[CI](.github/workflows/ci.yml) runs on pushes to `main`, pull requests, and manual requests. It checks formatting, module integrity, race tests, vet, helper-extension tests, and a native build on macOS and Linux. It never uses a real Slack session or inference provider.

[Release packages](.github/workflows/release.yml) builds all four macOS/Linux × amd64/arm64 targets, Linux `.deb` and `.rpm` packages, and macOS `.pkg` installers, then verifies SHA-256 checksums. The app is built with `CGO_ENABLED=0`, so Linux downloads do not require a specific glibc version. GoReleaser and Actions are pinned to specific versions/commits.

- **Test packaging:** run the Release packages workflow manually on `main`. It uploads a `slackline-installers` artifact with snapshot packages and checksums, without creating a GitHub release.
- **Publish a version:** push a SemVer tag such as `v0.1.0`. After tests and all installers succeed, the workflow creates a GitHub release with all downloads together. Tags with a prerelease suffix such as `v0.1.0-rc.1` become prereleases. Manual workflow runs never publish.
- Publication uses the workflow's `GITHUB_TOKEN` with `contents: write` only in the publish job. No personal access token is required. Signing/notarization would additionally require Apple Developer ID credentials.

Local packaging uses the same pinned [GoReleaser configuration](.goreleaser.yaml):

```sh
docker compose run --rm release check
docker compose run --rm release release --snapshot --clean --skip=publish
# On macOS, make a .pkg from either generated darwin archive:
scripts/package-macos.sh dist/slackline_<version>_darwin_arm64.tar.gz <version> dist
python3 scripts/checksums.py dist
```

The repository retains its [Apache 2.0 license](LICENSE).
