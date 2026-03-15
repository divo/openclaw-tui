package msg

import (
	"context"
	"time"
)

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
	SessionKey      string
	SessionFilePath string // path to the JSONL conversation file
	Err             error
}

type ChatReplyMsg struct {
	Reply      string
	Err        error
	Prompt     string
	MessageID  int
	Attempt    int
	MaxAttempt int
}

// ChatTailMsg is emitted periodically while tailing the session JSONL file.
// Lines contains new raw JSONL strings since the last poll.
// Done is set when the turn is complete (stopReason=stop found, or agent process exited).
type ChatTailMsg struct {
	Lines     []string
	NewOffset int64
	Done      bool
	Err       error
}

// ChatAgentFiredMsg is returned immediately when the agent turn is started
// in background. It carries the done channel so the tail loop can detect
// early agent exit (e.g. error before the JSONL file gets a stop message).
type ChatAgentFiredMsg struct {
	Done      <-chan error
	Cancel    context.CancelFunc
	MessageID int
}

type UITickMsg struct {
	At time.Time
}

type ChatRetryPendingMsg struct{}
