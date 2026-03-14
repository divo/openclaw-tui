package chat

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"openclaw-tui/internal/msg"
	"openclaw-tui/internal/transport"
)

func SendChatCmd(t transport.Transport, sessionKey, prompt string, messageID, attempt int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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

func UITickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
		return msg.UITickMsg{At: t}
	})
}
