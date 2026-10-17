# AI destinations and deferred inference

The application pins AI assistant and AI summaries above ordinary conversations. Both remain disabled local workspace views: assistant retrieval, daily digest generation, and scheduled reminders are still pending. Their composer cannot post to Slack.

## Private conversation summaries (implemented)

Settings (`,`) configures the OpenAI-compatible provider, masked key, chat model, and explicit `ai.enabled` switch (off by default). `s` inside a conversation retrieves accessible messages and replies from a rolling 14-day window and requests a catch-up: what's happening, decisions/blockers, and next steps. The transcript identifies authors, timestamps, thread roots where available, and source URLs. Model instructions distinguish evidence and suggestions, avoid invented owners/deadlines, and treat messages as untrusted data.

Slack search includes recent replies on roots older than two weeks. Exact channel/time checks apply to every result. Collection caps at ten pages of 100 matches and a 100 KB transcript; capped context is labeled partial. Slack retention/search preferences can further limit coverage. Failures never present an incomplete fetch as complete. No read markers or Slack posts are made by summary collection/generation.

The result is a fixed bottom card, with `S` for a scrollable full view. It persists only in the private workspace/user state (up to 128 conversation summaries); it is never a workspace-visible Slack pin. Its timestamp remains visible and regeneration is manual. HTTP provider requests have time/size bounds, refuse redirects, require HTTPS except local providers, and hide raw response/error bodies. Demo never invokes inference. Embeddings are not needed for this feature.

## AI assistant

Once inference and retrieval are implemented, accept prompts about accessible Slack conversations from the rolling last 14 days. Include channels, DMs, group DMs, and thread replies, independently of sidebar recent/filter/folding choices. Fetch and index only data the authenticated user can access. Answers must link back to source conversations/messages and distinguish evidence from inference. Do not imply complete workspace coverage when collection is partial or retention limits apply.

## AI summaries

Generate a daily catch-up and a return-to-client catch-up covering unread/missed activity, bounded by the same two-week retrieval window. Organize it as an overview, top to-dos, updates/decisions, and messages awaiting the user's reply. Tie action items and pending replies to source messages, distinguish explicit commitments from suggested follow-ups, and avoid inventing deadlines or treating a model guess as a confirmed todo.

A local last-client-visit timestamp is persisted now. Slack's per-conversation read markers are also tracked. Neither provides an exact global last-login timestamp across all native Slack clients, and the future catch-up must explain its coverage accordingly. Scheduling, daily delivery time, summary persistence/deduplication, and any operation while the app is closed belong to the inference integration phase. No background daemon is installed now.

## Activation work

Extend the on-demand provider and collection pipeline into workspace retrieval, persist coverage watermarks, and implement daily scheduling before enabling the workspace destinations. Keep credentials private. Any AI draft intended for Slack must remain an explicit user-confirmed post; these local destinations never become real Slack channels.
