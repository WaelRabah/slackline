# AI destinations and deferred inference

The application pins the AI assistant and AI summaries above ordinary conversations. The assistant answers questions from recent conversations, and conversation summaries run automatically. Daily digest generation and scheduled reminders are still pending. Neither AI view can post to Slack.

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

Open the AI assistant, press `i`, and ask a question. The assistant reads the last 14 days of the first 15 conversations in your list (channels, DMs and group DMs, plus thread replies), sends one bounded transcript to the chat-completions endpoint, and shows the answer in the pane. Answers cite Slack message links and are told to separate evidence from inference. Partial coverage is labeled on the answer. It reads only what your account can already access and posts nothing to Slack.

Limits: the first 15 conversations only, not a full workspace search; 150 KB of transcript per question; questions up to 1,000 characters. Answers are not saved between sessions.

## AI summaries

Generate a daily catch-up and a return-to-client catch-up covering unread/missed activity, bounded by the same two-week retrieval window. Organize it as an overview, top to-dos, updates/decisions, and messages awaiting the user's reply. Tie action items and pending replies to source messages, distinguish explicit commitments from suggested follow-ups, and avoid inventing deadlines or treating a model guess as a confirmed todo.

A local last-client-visit timestamp is persisted now. Slack's per-conversation read markers are also tracked. Neither provides an exact global last-login timestamp across all native Slack clients, and the future catch-up must explain its coverage accordingly. Scheduling, daily delivery time, summary persistence/deduplication, and any operation while the app is closed belong to the inference integration phase. No background daemon is installed now.

## Activation work

Extend the on-demand provider and collection pipeline into workspace retrieval, persist coverage watermarks, and implement daily scheduling before enabling the workspace destinations. Keep credentials private. Any AI draft intended for Slack must remain an explicit user-confirmed post; these local destinations never become real Slack channels.
