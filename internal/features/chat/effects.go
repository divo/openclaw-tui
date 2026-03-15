package chat

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"openclaw-tui/internal/msg"
	"openclaw-tui/internal/transport"
)

const tailPollInterval = 600 * time.Millisecond

// sendTimeout is generous: Claude replies can easily take 60-120 s on complex
// prompts. A tight timeout causes spurious retries that double-send messages.
const sendTimeout = 3 * time.Minute

func SendChatCmd(t transport.Transport, sessionKey, prompt string, messageID, attempt int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		defer cancel()
		reply, err := t.SendAgent(ctx, sessionKey, prompt)
		if err != nil {
			return msg.ChatReplyMsg{
				Err:        err,
				Prompt:     prompt,
				MessageID:  messageID,
				Attempt:    attempt,
				MaxAttempt: MaxSendAttempts,
			}
		}
		return msg.ChatReplyMsg{
			Reply:      strings.TrimSpace(reply),
			Prompt:     prompt,
			MessageID:  messageID,
			Attempt:    attempt,
			MaxAttempt: MaxSendAttempts,
		}
	}
}

// SendAgentFireCmd starts an agent turn in the background (fire-and-forget).
// The done channel is passed so the tail loop can detect early exit on error.
func SendAgentFireCmd(t transport.Transport, sessionKey, prompt string, messageID int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		done := t.SendAgentFire(ctx, sessionKey, prompt)
		// Return immediately — the tail loop will pick up the reply.
		// We embed the done channel into a message so update.go can monitor it.
		return msg.ChatAgentFiredMsg{
			Done:      done,
			Cancel:    cancel,
			MessageID: messageID,
		}
	}
}

// TailCmd polls the session JSONL file for new lines and emits a ChatTailMsg.
// agentDone is non-nil if we're actively tracking an agent turn; it signals
// that the underlying openclaw agent process has finished.
func TailCmd(t transport.Transport, filePath string, offset int64, agentDone <-chan error) tea.Cmd {
	return tea.Tick(tailPollInterval, func(time.Time) tea.Msg {
		lines, newOffset, err := t.ReadNewJSONLLines(filePath, offset)

		// Check if the agent process completed without blocking.
		var agentErr error
		agentFinished := false
		if agentDone != nil {
			select {
			case e := <-agentDone:
				agentFinished = true
				agentErr = e
			default:
			}
		}

		// Determine if the turn is done from the JSONL side (preferred).
		done := agentFinished
		for _, line := range lines {
			if p := ParseJSONLLine(line); p != nil && p.IsFinal {
				done = true
				break
			}
		}

		return msg.ChatTailMsg{
			Lines:     lines,
			NewOffset: newOffset,
			Done:      done,
			Err:       coalesceErr(err, agentErr),
		}
	})
}

func coalesceErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func RetryPendingCmd(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return msg.ChatRetryPendingMsg{}
	})
}

func UITickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
		return msg.UITickMsg{At: t}
	})
}
