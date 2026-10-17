package slack

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Demo implements the same API without credentials or network access.
type Demo struct {
	mu            sync.Mutex
	messages      map[string][]Message
	sequence      int
	threads       map[string][]Message
	startedAt     time.Time
	readPositions map[string]string
}

func NewDemo() *Demo {
	return &Demo{readPositions: map[string]string{}, startedAt: time.Now(), threads: map[string][]Message{"1792188120.000001": {{TS: "1792188121.000001", User: "UBOB", Text: "Threads are now supported. Backspace returns to the thread list.", ThreadTS: "1792188120.000001"}}}, messages: map[string][]Message{
		"CGENERAL": {
			{TS: "1792188000.000001", User: "UALICE", Text: ":wave: Welcome to slackline. This is an offline demo. Press v on this block for an enlarged image.", Files: []File{{ID: "FDEMO", Title: "Offline sunset preview", Mimetype: "image/png", URLPrivate: "demo://landscape"}}, Reactions: []Reaction{{Name: "tada", Count: 3, Users: []string{"UYOU"}}, {Name: "+1::skin-tone-3", Count: 2}}},
			{TS: "1792188060.000001", User: "UBOB", Text: "Use j/k to choose a conversation, Enter to open it, and i to compose."},
			{TS: "1792188120.000001", User: "UALICE", ReplyCount: 1, Text: "Press / to fuzzy-find channels and DMs. Tab switches panes; Esc leaves insert mode."},
		},
		"CDEV":   {{TS: "1792188180.000001", User: "UBOB", Text: "Session auth uses an xoxc token and its matching d cookie. AI and indexing are next."}},
		"DALICE": {{TS: "1792188240.000001", User: "UALICE", Text: "Try sending a message here; demo messages stay in memory."}},
	}}
}
func (*Demo) AuthTest(context.Context) (Session, error) {
	return Session{Team: "Demo workspace", TeamID: "TDEMO", User: "you", UserID: "UYOU"}, nil
}
func (*Demo) Conversations(context.Context) ([]Conversation, error) {
	return []Conversation{{ID: "CGENERAL", Name: "general"}, {ID: "CDEV", Name: "development"}, {ID: "DALICE", User: "UALICE", IsIM: true}}, nil
}
func (*Demo) Users(context.Context) ([]User, error) {
	return []User{{ID: "UALICE", RealName: "Alice"}, {ID: "UBOB", RealName: "Bob"}, {ID: "UYOU", RealName: "You"}}, nil
}
func (d *Demo) History(_ context.Context, channel, _ string) (History, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	source := d.messages[channel]
	result := History{Messages: make([]Message, len(source))}
	for i := range source {
		result.Messages[len(source)-1-i] = source[i]
	}
	return result, nil
}
func (d *Demo) Send(_ context.Context, channel, text string) (Message, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sequence++
	message := Message{TS: fmt.Sprintf("%d.%06d", time.Now().Unix(), d.sequence), User: "UYOU", Text: text}
	d.messages[channel] = append(d.messages[channel], message)
	return message, nil
}

func (d *Demo) Replies(_ context.Context, channel, thread, _ string) (History, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var messages []Message
	for _, message := range d.messages[channel] {
		if message.TS == thread {
			messages = append(messages, message)
			break
		}
	}
	messages = append(messages, d.threads[thread]...)
	return History{Messages: messages}, nil
}
func (d *Demo) SendReply(_ context.Context, channel, thread, text string) (Message, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sequence++
	message := Message{TS: fmt.Sprintf("%d.%06d", time.Now().Unix(), d.sequence), User: "UYOU", Text: text, ThreadTS: thread}
	d.threads[thread] = append(d.threads[thread], message)
	for i := range d.messages[channel] {
		if d.messages[channel][i].TS == thread {
			d.messages[channel][i].ReplyCount++
			break
		}
	}
	return message, nil
}

// The demo exposes the same polling metadata without making network requests.
func (d *Demo) Activity(context.Context) ([]Activity, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows := []Activity{}
	for id, messages := range d.messages {
		row := Activity{ID: id, LastRead: d.readPositions[id]}
		if row.LastRead == "" {
			row.LastRead = fmt.Sprintf("%d.000000", d.startedAt.Unix())
			if id == "CGENERAL" {
				row.LastRead = "1792188000.000001"
			}
			if id == "DALICE" {
				row.LastRead = "1792188239.000001"
			}
		}
		for _, message := range messages {
			if TimestampTime(message.TS).After(TimestampTime(row.Latest)) {
				row.Latest = message.TS
			}
		}
		count := 0
		for _, message := range messages {
			if message.User != "UYOU" && TimestampTime(message.TS).After(TimestampTime(row.LastRead)) {
				count++
			}
		}
		row.HasUnreads = count > 0
		row.UnreadCount = &count
		rows = append(rows, row)
	}
	return rows, nil
}
func (d *Demo) RecentSent(_ context.Context, since time.Time) (map[string]time.Time, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	recent := map[string]time.Time{}
	for id, messages := range d.messages {
		for _, message := range messages {
			at := TimestampTime(message.TS)
			if message.User == "UYOU" && !at.Before(since) && at.After(recent[id]) {
				recent[id] = at
			}
		}
	}
	return recent, nil
}

func (d *Demo) MarkRead(_ context.Context, channel, thread, ts string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := channel
	if thread != "" {
		key += "/" + thread
	}
	if TimestampTime(ts).After(TimestampTime(d.readPositions[key])) {
		d.readPositions[key] = ts
	}
	return nil
}
func (d *Demo) CountUnread(_ context.Context, channel, after, through, self string) (UnreadCount, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := UnreadCount{Exact: true}
	for _, message := range d.messages[channel] {
		at := TimestampTime(message.TS)
		if at.After(TimestampTime(after)) && !at.After(TimestampTime(through)) && message.User != self {
			result.Count++
		}
	}
	return result, nil
}
func (d *Demo) Threads(_ context.Context, before string) (ThreadPage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	page := ThreadPage{}
	for channel, messages := range d.messages {
		for _, root := range messages {
			replies := d.threads[root.TS]
			if len(replies) == 0 {
				continue
			}
			root.Channel = channel
			root.LastRead = d.readPositions[channel+"/"+root.TS]
			if root.LastRead == "" {
				root.LastRead = root.TS
			}
			thread := Thread{Root: root, LatestReplies: append([]Message{}, replies...)}
			for _, reply := range replies {
				if reply.User != "UYOU" && TimestampTime(reply.TS).After(TimestampTime(root.LastRead)) {
					thread.UnreadReplies = append(thread.UnreadReplies, reply)
					page.TotalUnread++
				}
			}
			if before == "" || TimestampTime(thread.Latest()).Before(TimestampTime(before)) {
				page.Threads = append(page.Threads, thread)
			}
		}
	}
	return page, nil
}

func (d *Demo) ConversationContext(ctx context.Context, channel string, since, until time.Time) (ContextPage, error) {
	h, err := d.History(ctx, channel, "")
	result := ContextPage{}
	for _, message := range h.Messages {
		at := TimestampTime(message.TS)
		if !at.Before(since) && !at.After(until) {
			result.Messages = append(result.Messages, message)
		}
		if message.ReplyCount > 0 {
			replies, e := d.Replies(ctx, channel, message.TS, "")
			if e != nil {
				return result, e
			}
			for _, reply := range replies.Messages {
				at = TimestampTime(reply.TS)
				if reply.TS != message.TS && !at.Before(since) && !at.After(until) {
					result.Messages = append(result.Messages, reply)
				}
			}
		}
	}
	return result, err
}
