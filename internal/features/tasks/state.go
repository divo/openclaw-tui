package tasks

import "openclaw-tui/internal/msg"

type State struct {
	Items  []msg.TaskItem
	Offset int
}
