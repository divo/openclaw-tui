package ui

type Pane int

const (
	PaneStatus Pane = iota
	PaneSessions
	PaneTasks
	PaneChat
	PaneTerminal
)

type Mode int

const (
	ModeMove Mode = iota
	ModeEdit
)
