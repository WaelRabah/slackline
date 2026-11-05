# AI destinations and deferred inference

The application pins the AI assistant and AI summaries above ordinary conversations. The assistant answers questions from recent conversations, and conversation summaries run automatically. Scheduled daily delivery and reminders are still pending. Neither AI view can post to Slack.

## Private conversation summaries (implemented)

Settings (`,`) configures the OpenAI-compatible provider, masked key, chat model, and explicit `ai.enabled` switch (off by default). `s` inside a conversation retrieves accessible messages and replies from a rolling 14-day window and requests a catch-up: what's happening, decisions/blockers, and next steps. The transcript identifies authors, timestamps, thread roots where available, and source URLs. Model instructions distinguish evidence and suggestions, avoid invented owners/deadlines, and treat messages as untrusted data.

Retrieval uses `conversations.history` over the 14-day window, then `conversations.replies` for thread roots in that window (up to 40 threads). Every message is checked against the exact channel and timestamps. Collection caps at 2,000 messages per conversation and a 100 KB transcript; capped context is labeled partial. Slack retention can further limit coverage. Failures never present an incomplete fetch as complete. No read markers or Slack posts are made by summary collection/generation.

Summaries run automatically for the open conversation. A summary runs when the conversation opens and when a new message arrives in it. A stored summary is reused while no newer message exists, so opening the same conversation again costs nothing. Pressing `s` always regenerates. A failed run is not retried automatically, so a provider error cannot loop. Other conversations are summarized only when opened.

The result is a fixed bottom card, with `S` for a scrollable full view. It persists only in the private workspace/user state (up to 128 conversation summaries); it is never a workspace-visible Slack pin. Its timestamp remains visible and regeneration is manual. HTTP provider requests have time/size bounds, refuse redirects, require HTTPS except local providers, and hide raw response/error bodies. Demo never invokes inference. Embeddings are not needed for this feature.

## Speech to text (implemented)

In the composer (insert mode), `ctrl+t` records 10 seconds from the default microphone, transcribes it, and adds the text at the cursor of the current draft. Review it, then Enter to send. Nothing is sent without that confirmation.

Set `ai.transcription_model` in `config.toml` to the model you assign for `/v1/audio/transcriptions`. The provider is the same `ai.base_url` and `ai.api_key` used by summaries, and `ai.enabled` must be true. An empty model disables the feature.

Capture uses `ffmpeg` (`brew install ffmpeg`) through macOS AVFoundation. The terminal running Slackline needs microphone access in System Settings > Privacy & Security > Microphone. In Warp that is Warp itself. Clips are capped at 20 MB, requests use the same HTTPS-only, no-redirect policy as summaries, and error text never includes audio or responses.

## AI assistant

Open the AI assistant, press `i`, and ask a question. The assistant works through tools on the chat-completions endpoint (function calling):

- `list_conversations` returns your channels, DMs, group DMs and followed threads, with IDs and unread counts.
- `get_messages` reads messages from any conversation the model picks, or one thread (`thread_ts`), over a date range. The default is the last 14 days; the maximum is 90 days.

The model calls these as needed, up to 8 rounds per question. Answers are Markdown with Slack message links and are told to separate evidence from inference. Partial coverage is labeled on the answer. Tool results are untrusted data, and the assistant cannot post to Slack.

Limits: 60 KB per tool result, 300 KB of Slack content per question, and questions up to 1,000 characters. Answers are not saved between sessions. Providers without function calling will answer without reading Slack.

Answers are rendered from Markdown in the pane: headings, bold, italics, code, lists, quotes, rules and clickable links.

## AI summaries

The AI Summaries view is your catch-up. It reads the unread messages across your conversations, the same set the Unreads Summaries view uses, and writes one Markdown summary with the headings "What you missed", "Top to-dos", "Updates and decisions" and "Messages waiting for your reply". Each bullet names its conversation and links to the source message. Top to-dos appear only when someone asked for an action, and no owners or deadlines are invented.

Today's catch-up runs automatically at 11:00 local time, once per day. The app has to be open at 11:00; if it starts later the same day, the catch-up runs at startup. It runs when the view opens and reruns only when unread messages change. `s` regenerates. A chime plays when a summary is ready (sounds can be muted in Settings). It reads up to 20 conversations and 150 KB of transcript; anything cut off is labeled partial. Failures are shown in the view and retried the next time it opens. Without AI enabled, the view explains how to turn it on.

Bot, alert and integration posts are read too, including their attachments and block text, so alert channels are not empty.

A local last-client-visit timestamp is persisted now. Slack's per-conversation read markers are also tracked. Neither provides an exact global last-login timestamp across all native Slack clients, and the future catch-up must explain its coverage accordingly. Scheduling, daily delivery time, summary persistence/deduplication, and any operation while the app is closed belong to the inference integration phase. No background daemon is installed now.

## Unreads Summaries

The sidebar's "Unreads Summaries" entry shows one block per channel with unread messages, most unread first. With AI enabled, each block is sorted into "To reply", "To do", "Decisions and updates" and "FYI" with source links, rendered as Markdown. Thread replies from the Threads feed are included when that feed has loaded.

Keys: `n` or `j` moves to the next block, `p` or `k` to the previous one (both wrap). `x` marks the shown block read in Slack, including its threads. The block leaves the view only after Slack confirms; if the call fails, it stays and the error is shown. `s` regenerates the summaries.

The summaries run when the view opens and rerun only if the set of unread channels or their newest messages changed. Up to 20 channels per run and 40 KB of transcript per channel; anything cut off is labeled partial. Without AI, the view still shows the counts.

## Activation work

Extend the on-demand provider and collection pipeline into workspace retrieval, persist coverage watermarks, and implement daily scheduling before enabling the workspace destinations. Keep credentials private. Any AI draft intended for Slack must remain an explicit user-confirmed post; these local destinations never become real Slack channels.
