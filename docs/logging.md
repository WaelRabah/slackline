# Logs

Slackline writes errors to `~/.slackline/slackline.log` so failures can be investigated after they happen.

- **What is logged:** every error the app shows in the status line, Slack API failures (with the method name), AI provider failures, failed background summaries and unread blocks, image load failures, sound and pop-up failures, live-connection drops, and recovered panics with a stack trace. Startup and shutdown are logged as information.
- **Format:** one line per event: time, level (`INFO`, `ERROR`, `PANIC`), the area (`slack`, `ai`, `ui`, `live`, `summary`, `unreads`, `image`, `sound`, `popup`, `app`), then the message.
- **Privacy:** tokens, cookies, API keys, bearer headers and UUIDs are replaced with `REDACTED` before anything is written. Message text is not logged.
- **Permissions:** the folder is `0700` and the file is `0600`.
- **Size:** when the log passes 5 MB it is renamed to `slackline.log.1` and a new log starts.
- **If logging fails** (for example, no home directory), the app keeps running and prints a warning.
