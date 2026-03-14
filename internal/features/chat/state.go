package chat

import "time"

var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type State struct {
	Lines      []string
	Input      string
	Sending    bool
	StartedAt  time.Time
	SpinnerIdx int
	PendingMsg string
	Offset     int
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
