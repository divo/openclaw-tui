package ui

import (
	"github.com/charmbracelet/lipgloss"
)

type Dimensions struct {
	BodyH     int
	LeftW     int
	RightW    int
	StatusH   int
	SessionsH int
	TasksH    int
	ChatH     int
	RunH      int
	TerminalH int
}

func ComputeDimensions(width, height int) Dimensions {
	bodyH := max(10, height-7)
	leftW := max(24, width/2)
	rightW := width - leftW

	// Left column: status, sessions, tasks, chat, run status
	runH := 3
	statusH := max(3, bodyH/6)
	sessionsH := max(3, bodyH/6)
	tasksH := max(3, bodyH/5)
	chatH := max(4, bodyH-statusH-sessionsH-tasksH-runH)

	// Right column: terminal takes full height
	terminalH := bodyH

	return Dimensions{
		BodyH:     bodyH,
		LeftW:     leftW,
		RightW:    rightW,
		StatusH:   statusH,
		SessionsH: sessionsH,
		TasksH:    tasksH,
		ChatH:     chatH,
		RunH:      runH,
		TerminalH: terminalH,
	}
}

func FocusLeft(p Pane) Pane {
	if p == PaneTerminal {
		return PaneChat
	}
	return p
}

func FocusRight(p Pane) Pane {
	switch p {
	case PaneStatus, PaneSessions, PaneTasks, PaneChat:
		return PaneTerminal
	default:
		return p
	}
}

func FocusUp(p Pane) Pane {
	switch p {
	case PaneSessions:
		return PaneStatus
	case PaneTasks:
		return PaneSessions
	case PaneChat:
		return PaneTasks
	default:
		return p
	}
}

func FocusDown(p Pane) Pane {
	switch p {
	case PaneStatus:
		return PaneSessions
	case PaneSessions:
		return PaneTasks
	case PaneTasks:
		return PaneChat
	default:
		return p
	}
}

func PaneBox(title string, focused bool, width, height int, content string) string {
	b := lipgloss.NormalBorder()
	st := lipgloss.NewStyle().Border(b).Padding(0, 1).Width(max(8, width-4)).Height(max(3, height-2))
	if focused {
		st = st.BorderForeground(lipgloss.Color("45"))
		title = "● " + title
	}
	return st.Render(title + "\n" + content)
}
