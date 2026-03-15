package chat

import (
	"fmt"
	"time"
)

var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const MaxSendAttempts = 3
const MaxLockAttempts = 20

type State struct {
	Lines      []string
	Input      string
	Sending    bool
	StartedAt  time.Time
	SpinnerIdx int
	Offset     int
	FollowTail bool

	InputHistory []string
	HistoryIndex int // -1 when not navigating history

	NextMessageID int
	ActiveMsgID   int
	ActiveAttempt int
	ActivePrompt  string

	PendingMsg     string
	PendingMsgID   int
	PendingAttempt int

	// Tail tracks the async reply stream via the session JSONL file.
	TailFilePath string
	TailOffset   int64
	Tailing      bool
}

func InitialState() State {
	return State{
		Lines: []string{
			"Amerish: Ready.",
			"MOVE mode: h/j/k/l changes pane focus.",
			"EDIT mode: focus Chat + i, type, Enter sends, Esc returns to MOVE.",
			"Scroll: J/K line, Ctrl+d/Ctrl+u page.",
		},
		FollowTail:   true,
		HistoryIndex: -1,
	}
}

// BeginSendAsync marks a turn as in-flight for the async (JSONL tail) path.
// Unlike BeginSend it doesn't show an attempt counter — there is only one shot.
func BeginSendAsync(state State) State {
	if state.ActiveMsgID == 0 {
		return state
	}
	state.Sending = true
	state.StartedAt = time.Now()
	state.Lines = append(state.Lines,
		fmt.Sprintf("↳ [%03d] sending...", state.ActiveMsgID),
	)
	state.FollowTail = true
	trimLines(&state)
	return state
}

func StartSend(state State, prompt string) State {
	state.NextMessageID++
	state.ActiveMsgID = state.NextMessageID
	state.ActiveAttempt = 1
	state.ActivePrompt = prompt
	state.Input = ""
	state.Sending = false
	state.StartedAt = time.Time{}
	state.HistoryIndex = -1
	if prompt != "" {
		if len(state.InputHistory) == 0 || state.InputHistory[len(state.InputHistory)-1] != prompt {
			state.InputHistory = append(state.InputHistory, prompt)
			if len(state.InputHistory) > 100 {
				state.InputHistory = state.InputHistory[len(state.InputHistory)-100:]
			}
		}
	}

	state.Lines = append(state.Lines,
		fmt.Sprintf("You[%03d]: %s", state.ActiveMsgID, prompt),
		fmt.Sprintf("↳ [%03d] queued", state.ActiveMsgID),
	)
	state.FollowTail = true
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
	state.FollowTail = true
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
		state.FollowTail = true
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

func HistoryPrev(state State) State {
	if len(state.InputHistory) == 0 {
		return state
	}
	if state.HistoryIndex == -1 {
		state.HistoryIndex = len(state.InputHistory) - 1
	} else if state.HistoryIndex > 0 {
		state.HistoryIndex--
	}
	state.Input = state.InputHistory[state.HistoryIndex]
	return state
}

func HistoryNext(state State) State {
	if len(state.InputHistory) == 0 {
		return state
	}
	if state.HistoryIndex == -1 {
		return state
	}
	if state.HistoryIndex < len(state.InputHistory)-1 {
		state.HistoryIndex++
		state.Input = state.InputHistory[state.HistoryIndex]
		return state
	}
	state.HistoryIndex = -1
	state.Input = ""
	return state
}

func Scroll(state State, delta int) State {
	if state.FollowTail {
		state.FollowTail = false
		if len(state.Lines) > 0 {
			state.Offset = len(state.Lines) - 1
		}
	}
	state.Offset += delta
	if state.Offset < 0 {
		state.Offset = 0
	}
	if state.Offset > max(0, len(state.Lines)-1) {
		state.Offset = max(0, len(state.Lines)-1)
	}
	return state
}

func FollowLatest(state State) State {
	state.FollowTail = true
	state.Offset = 0
	return state
}

func trimLines(state *State) {
	if len(state.Lines) > 200 {
		state.Lines = state.Lines[len(state.Lines)-200:]
	}
}
