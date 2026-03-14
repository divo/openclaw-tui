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
			if m.Attempt < m.MaxAttempt {
				nextAttempt := m.Attempt + 1
				state.Lines = append(state.Lines,
					fmt.Sprintf("↳ [%03d] attempt %d/%d failed — reconnecting...", m.MessageID, m.Attempt, m.MaxAttempt),
				)
				state.PendingMsg = m.Prompt
				state.PendingMsgID = m.MessageID
				state.PendingAttempt = nextAttempt
				state.ActiveAttempt = nextAttempt
				state.Sending = false
				state.StartedAt = time.Time{}
				trimLines(&state)
				return state, ReduceResult{NeedSessionDiscover: true}
			}
			state.Lines = append(state.Lines,
				fmt.Sprintf("↳ [%03d] failed after %d/%d attempts", m.MessageID, m.Attempt, m.MaxAttempt),
				"Amerish [error]: "+m.Err.Error(),
			)
			trimLines(&state)
			return ClearActive(state), ReduceResult{}
		}

		reply := strings.TrimSpace(m.Reply)
		if reply == "" {
			reply = "(no reply text)"
		}
		state.Lines = append(state.Lines, fmt.Sprintf("↳ [%03d] sent", m.MessageID))
		for _, line := range strings.Split(reply, "\n") {
			state.Lines = append(state.Lines, "Amerish: "+compactLine(line, 180))
		}
		trimLines(&state)
		return ClearActive(state), ReduceResult{}

	default:
		return state, ReduceResult{}
	}
}

func compactLine(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if maxLen <= 3 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
