package chat

import (
	"fmt"
	"time"
)

var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const MaxSendAttempts = 3

type State struct {
	Lines      []string
	Input      string
	Sending    bool
	StartedAt  time.Time
	SpinnerIdx int
	Offset     int

	NextMessageID int
	ActiveMsgID   int
	ActiveAttempt int
	ActivePrompt  string

	PendingMsg     string
	PendingMsgID   int
	PendingAttempt int
}

func InitialState() State {
	return State{
		Lines: []string{
			"Amerish: Ready.",
			"MOVE mode: h/j/k/l changes pane focus.",
			"EDIT mode: focus Chat + i, type, Enter sends, Esc returns to MOVE.",
			"Scroll: J/K line, Ctrl+d/Ctrl+u page.",
		},
	}
}

func StartSend(state State, prompt string) State {
	state.NextMessageID++
	state.ActiveMsgID = state.NextMessageID
	state.ActiveAttempt = 1
	state.ActivePrompt = prompt
	state.Input = ""
	state.Sending = false
	state.StartedAt = time.Time{}

	state.Lines = append(state.Lines,
		fmt.Sprintf("You[%03d]: %s", state.ActiveMsgID, prompt),
		fmt.Sprintf("↳ [%03d] queued", state.ActiveMsgID),
	)
	trimLines(&state)
	return state
}

func BeginSend(state State) State {
	if state.ActiveMsgID == 0 {
		return state
	}
	state.Sending = true
	state.StartedAt = time.Now()
	state.Lines = append(state.Lines,
		fmt.Sprintf("↳ [%03d] sending (attempt %d/%d)", state.ActiveMsgID, state.ActiveAttempt, MaxSendAttempts),
	)
	trimLines(&state)
	return state
}

func QueueForReconnect(state State, reason string) State {
	if state.ActivePrompt == "" || state.ActiveMsgID == 0 {
		return state
	}
	state.PendingMsg = state.ActivePrompt
	state.PendingMsgID = state.ActiveMsgID
	if state.ActiveAttempt <= 0 {
		state.ActiveAttempt = 1
	}
	state.PendingAttempt = state.ActiveAttempt
	state.Sending = false
	state.StartedAt = time.Time{}
	if reason != "" {
		state.Lines = append(state.Lines, fmt.Sprintf("↳ [%03d] %s", state.ActiveMsgID, reason))
		trimLines(&state)
	}
	return state
}

func BeginPendingSend(state State) State {
	if state.PendingMsg == "" || state.PendingMsgID == 0 {
		return state
	}
	state.ActiveMsgID = state.PendingMsgID
	state.ActivePrompt = state.PendingMsg
	if state.PendingAttempt <= 0 {
		state.PendingAttempt = 1
	}
	state.ActiveAttempt = state.PendingAttempt
	state.PendingMsg = ""
	state.PendingMsgID = 0
	state.PendingAttempt = 0
	return BeginSend(state)
}

func ClearActive(state State) State {
	state.ActiveMsgID = 0
	state.ActivePrompt = ""
	state.ActiveAttempt = 0
	state.Sending = false
	state.StartedAt = time.Time{}
	state.PendingMsg = ""
	state.PendingMsgID = 0
	state.PendingAttempt = 0
	return state
}

func trimLines(state *State) {
	if len(state.Lines) > 200 {
		state.Lines = state.Lines[len(state.Lines)-200:]
	}
}
