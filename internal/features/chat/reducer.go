package chat

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"openclaw-tui/internal/msg"
)

type ReduceResult struct {
	NeedSessionDiscover bool
	Cmd                 tea.Cmd
}

func Reduce(state State, incoming tea.Msg) (State, ReduceResult) {
	switch m := incoming.(type) {
	case msg.UITickMsg:
		state.SpinnerIdx = (state.SpinnerIdx + 1) % len(SpinnerFrames)
		return state, ReduceResult{Cmd: UITickCmd()}
	case msg.ChatReplyMsg:
		state.Sending = false
		state.StartedAt = time.Time{}
		if m.Err != nil {
			if m.RetryCount < 3 {
				state.Lines = append(state.Lines, fmt.Sprintf("⚠ send failed (retry %d/3) — reconnecting...", m.RetryCount+1))
				state.PendingMsg = m.Prompt
				state.Sending = true
				state.StartedAt = time.Now()
				return state, ReduceResult{NeedSessionDiscover: true}
			}
			state.Lines = append(state.Lines, "Amerish [error]: "+m.Err.Error())
			return state, ReduceResult{}
		}
		reply := strings.TrimSpace(m.Reply)
		if reply == "" {
			reply = "(no reply text)"
		}
		for _, line := range strings.Split(reply, "\n") {
			state.Lines = append(state.Lines, "Amerish: "+compactLine(line, 180))
		}
		if len(state.Lines) > 120 {
			state.Lines = state.Lines[len(state.Lines)-120:]
		}
		return state, ReduceResult{}
	default:
		return state, ReduceResult{}
	}
}

func StartSend(state State, prompt string) State {
	state.Lines = append(state.Lines, "You: "+prompt)
	state.Input = ""
	state.Sending = true
	state.StartedAt = time.Now()
	return state
}

func compactLine(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if maxLen <= 3 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
