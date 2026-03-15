package chat

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"openclaw-tui/internal/msg"
	"openclaw-tui/internal/transport"
)

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
