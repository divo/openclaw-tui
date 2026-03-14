package chat

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"openclaw-tui/internal/msg"
	"openclaw-tui/internal/transport"
)

func SendChatCmd(t transport.Transport, sessionKey, prompt string, retryCount int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		reply, err := t.SendAgent(ctx, sessionKey, prompt)
		if err != nil {
			return msg.ChatReplyMsg{Err: err, RetryCount: retryCount, Prompt: prompt}
		}
		return msg.ChatReplyMsg{Reply: strings.TrimSpace(reply)}
	}
}

func UITickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
		return msg.UITickMsg{At: t}
	})
}
