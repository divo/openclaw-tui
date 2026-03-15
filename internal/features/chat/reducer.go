package chat

import (
	"context"
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
			// Timeout: the message may already have been delivered to the agent.
			// Retrying would send it a second time — don't.
			if isTimeoutError(m.Err) {
				state.Lines = append(state.Lines,
					fmt.Sprintf("↳ [%03d] timed out after %s — check the main chat for a reply", m.MessageID, sendTimeout),
				)
				trimLines(&state)
				return ClearActive(state), ReduceResult{}
			}

			if isSessionLockError(m.Err) {
				if m.Attempt < MaxLockAttempts {
					nextAttempt := m.Attempt + 1
					delay := lockRetryDelay(nextAttempt)
					state.Lines = append(state.Lines,
						fmt.Sprintf("↳ [%03d] session busy (lock) — retrying in %s (%d/%d)", m.MessageID, delay.Round(time.Second), nextAttempt, MaxLockAttempts),
					)
					state.PendingMsg = m.Prompt
					state.PendingMsgID = m.MessageID
					state.PendingAttempt = nextAttempt
					state.ActiveAttempt = nextAttempt
					state.Sending = false
					state.StartedAt = time.Time{}
					trimLines(&state)
					return state, ReduceResult{Cmd: RetryPendingCmd(delay)}
				}
				state.Lines = append(state.Lines,
					fmt.Sprintf("↳ [%03d] failed after %d/%d lock retries", m.MessageID, m.Attempt, MaxLockAttempts),
					"Amerish [error]: "+m.Err.Error(),
				)
				trimLines(&state)
				return ClearActive(state), ReduceResult{}
			}

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

// ProcessTailLines consumes a ChatTailMsg from the JSONL poller and updates
// the chat state. Returns the updated state and whether the turn is complete.
func ProcessTailLines(state State, m msg.ChatTailMsg) (State, bool) {
	sawFinal := false
	for _, line := range m.Lines {
		p := ParseJSONLLine(line)
		if p == nil {
			continue
		}
		switch {
		case p.IsError:
			state.Lines = append(state.Lines,
				fmt.Sprintf("↳ [%03d] agent error: %s", state.ActiveMsgID, p.StopReason),
			)
			sawFinal = true
		case len(p.ToolNames) > 0:
			state.Lines = append(state.Lines,
				fmt.Sprintf("↳ [%03d] ⚙  %s", state.ActiveMsgID, strings.Join(p.ToolNames, ", ")),
			)
		case p.IsFinal:
			state.Lines = append(state.Lines, fmt.Sprintf("↳ [%03d] done", state.ActiveMsgID))
			for _, l := range strings.Split(p.Text, "\n") {
				state.Lines = append(state.Lines, "Amerish: "+compactLine(l, 180))
			}
			sawFinal = true
		}
	}
	state.FollowTail = true
	trimLines(&state)

	if m.Err != nil && !sawFinal {
		state.Lines = append(state.Lines,
			fmt.Sprintf("↳ [%03d] tail error: %s", state.ActiveMsgID, m.Err.Error()),
		)
		return ClearActive(state), true
	}

	done := m.Done || sawFinal
	if done {
		return ClearActive(state), true
	}
	return state, false
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	return err == context.DeadlineExceeded || err == context.Canceled ||
		strings.Contains(err.Error(), "context deadline exceeded") ||
		strings.Contains(err.Error(), "context canceled")
}

func isSessionLockError(err error) bool {
	if err == nil {
		return false
	}
	raw := strings.ToLower(err.Error())
	return strings.Contains(raw, "session file locked") || strings.Contains(raw, ".jsonl.lock")
}

func lockRetryDelay(nextAttempt int) time.Duration {
	switch {
	case nextAttempt <= 2:
		return 2 * time.Second
	case nextAttempt <= 4:
		return 4 * time.Second
	case nextAttempt <= 7:
		return 8 * time.Second
	case nextAttempt <= 12:
		return 12 * time.Second
	default:
		return 15 * time.Second
	}
}
