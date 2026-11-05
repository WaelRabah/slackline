package tui

import (
	"regexp"
	"strconv"
	"time"
)

// Slack renders dates as <!date^UNIX^format|fallback>. The fallback is the text
// people expect to read; without one, the time is formatted locally.
var slackDateWithFallback = regexp.MustCompile(`<!date\^\d+\^[^|>]*\|([^>]*)>`)
var slackDateBare = regexp.MustCompile(`<!date\^(\d+)\^[^>]*>`)

// expandSlackDates replaces Slack date tokens with readable dates.
func expandSlackDates(text string) string {
	text = slackDateWithFallback.ReplaceAllString(text, "$1")
	return slackDateBare.ReplaceAllStringFunc(text, func(token string) string {
		match := slackDateBare.FindStringSubmatch(token)
		seconds, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			return token
		}
		return time.Unix(seconds, 0).Local().Format("Mon Jan 2, 15:04")
	})
}
