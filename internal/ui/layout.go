package ui

import (
	"github.com/charmbracelet/lipgloss"
)

type Dimensions struct {
	BodyH     int
	TopH      int
	BottomH   int
	LeftW     int
	RightW    int
	StatusH   int
	SessionsH int
	RunH      int
	ChatH     int
	TerminalH int
}

func ComputeDimensions(width, height int) Dimensions {
	bodyH := max(10, height-7)
	topH := max(6, bodyH/2)
	bottomH := bodyH - topH
	leftW := max(24, width/2)
	rightW := width - leftW
	statusH := max(3, topH/2)
	sessionsH := topH - statusH
	runH := 4
	terminalH := max(7, bottomH/2)
	chatH := max(6, bottomH-terminalH-runH)

	return Dimensions{
		BodyH:     bodyH,
		TopH:      topH,
		BottomH:   bottomH,
		LeftW:     leftW,
		RightW:    rightW,
		StatusH:   statusH,
		SessionsH: sessionsH,
		RunH:      runH,
		ChatH:     chatH,
		TerminalH: terminalH,
	}
}

func FocusLeft(p Pane) Pane {
	switch p {
	case PaneTasks:
		return PaneStatus
	case PaneChat:
		return PaneSessions
	default:
		return p
	}
}

func FocusRight(p Pane) Pane {
	switch p {
	case PaneStatus, PaneSessions:
		return PaneTasks
	default:
		return p
	}
}

func FocusUp(p Pane) Pane {
	switch p {
	case PaneSessions:
		return PaneStatus
	case PaneChat:
		return PaneSessions
	case PaneTerminal:
		return PaneChat
	default:
		return p
	}
}

func FocusDown(p Pane) Pane {
	switch p {
	case PaneStatus:
		return PaneSessions
	case PaneSessions, PaneTasks:
		return PaneChat
	case PaneChat:
		return PaneTerminal
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
