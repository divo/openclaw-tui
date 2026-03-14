package msg

import "time"

type TaskItem struct {
	Priority int
	Text     string
}

type RefreshResult struct {
	StatusRaw   string
	SessionsRaw string
	TaskItems   []TaskItem
	Errors      []string
	At          time.Time
}

type RefreshMsg RefreshResult

type SessionDiscoverMsg struct {
	SessionKey string
	Err        error
}

type ChatReplyMsg struct {
	Reply      string
	Err        error
	RetryCount int
	Prompt     string
}

type UITickMsg struct {
	At time.Time
}
