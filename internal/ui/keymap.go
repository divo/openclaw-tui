package ui

type Pane int

const (
	PaneStatus Pane = iota
	PaneSessions
	PaneTasks
	PaneChat
)

type Mode int

const (
	ModeMove Mode = iota
	ModeEdit
)
