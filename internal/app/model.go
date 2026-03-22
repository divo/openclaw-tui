package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"openclaw-tui/internal/features/chat"
	"openclaw-tui/internal/features/sessions"
	"openclaw-tui/internal/features/status"
	"openclaw-tui/internal/features/tasks"
	"openclaw-tui/internal/features/terminal"
	"openclaw-tui/internal/transport"
	"openclaw-tui/internal/ui"
)

const tasksPath = "/home/divo/code/obsidian/Amerish/TASKS.md"

type ConnState int

const (
	ConnConnecting ConnState = iota
	ConnConnected
	ConnDisconnected
)

func (c ConnState) String() string {
	switch c {
	case ConnConnecting:
		return "connecting"
	case ConnConnected:
		return "connected"
	default:
		return "disconnected"
	}
}

type Model struct {
	Width       int
	Height      int
	Status      string
	LastRefresh time.Time

	SessionKey      string
	SessionFilePath string
	Conn            ConnState
	Errors          []string

	Focus ui.Pane
	Mode  ui.Mode

	StatusPane   status.State
	SessionsPane sessions.State
	TasksPane    tasks.State
	ChatPane     chat.State
	TerminalPane terminal.State
	TerminalMgr  *terminal.Manager

	Transport transport.Transport
}

func NewModel(t transport.Transport) Model {
	mgr := terminal.NewManager()
	return Model{
		Status:       "Booting",
		LastRefresh:  time.Now(),
		Conn:         ConnConnecting,
		Focus:        ui.PaneChat,
		Mode:         ui.ModeMove,
		StatusPane:   status.State{ConnectionItems: []string{"Loading channels..."}},
		SessionsPane: sessions.State{Items: []string{"Loading sessions..."}},
		TasksPane:    tasks.State{Items: tasks.ReadTaskItems(tasksPath, 12)},
		ChatPane:     chat.InitialState(),
		TerminalPane: terminal.InitialState(),
		TerminalMgr:  mgr,
		Transport:    t,
	}
}

func (m Model) View() string {
	if m.Width == 0 || m.Height == 0 {
		return "Loading..."
	}

	headerStyle := ui.HeaderStyle()
	muted := ui.MutedStyle()
	errorStyle := ui.ErrorStyle()

	modeLabel := "MOVE"
	if m.Mode == ui.ModeEdit {
		modeLabel = "EDIT"
	}

	sessionLabel := m.SessionKey
	if sessionLabel == "" {
		sessionLabel = "—"
	}

	header := headerStyle.Render(fmt.Sprintf(
		"OpenClaw TUI | %s | %s | session=%s | refreshed=%s",
		modeLabel, status.ConnStateLabel(m.Conn.String()), sessionLabel, m.LastRefresh.Format("15:04:05"),
	))

	dims := ui.ComputeDimensions(m.Width, m.Height)

	// Left column: status, sessions, tasks, chat, run status
	statusPane := ui.PaneBox("Status", m.Focus == ui.PaneStatus, dims.LeftW, dims.StatusH, status.ViewList(m.StatusPane.ConnectionItems, m.StatusPane.Offset, dims.StatusH-2))
	sessionsPane := ui.PaneBox("Sessions", m.Focus == ui.PaneSessions, dims.LeftW, dims.SessionsH, sessions.View(m.SessionsPane.Items, m.SessionsPane.Offset, dims.SessionsH-2))
	tasksPane := ui.PaneBox("Tasks", m.Focus == ui.PaneTasks, dims.LeftW, dims.TasksH, tasks.View(m.TasksPane.Items, m.TasksPane.Offset, dims.TasksH-2))
	chatBody := chat.View(m.ChatPane, m.Mode, dims.ChatH-2)
	chatPane := ui.PaneBox("Chat", m.Focus == ui.PaneChat, dims.LeftW, dims.ChatH, chatBody)
	runStatusLine := chat.RunStatusLine(m.ChatPane, m.Mode, m.Conn.String(), m.SessionKey, m.LastRefresh, m.Errors)
	runStatusLine += " | " + terminal.StatusLine(m.TerminalPane)
	runStatusPane := ui.PaneBox("Run Status", false, dims.LeftW, dims.RunH, runStatusLine)
	left := lipgloss.JoinVertical(lipgloss.Left, statusPane, sessionsPane, tasksPane, chatPane, runStatusPane)

	// Right column: terminal (full height)
	terminalBody := terminal.View(m.TerminalPane, dims.TerminalH-2)
	right := ui.PaneBox("Terminal", m.Focus == ui.PaneTerminal, dims.RightW, dims.TerminalH, terminalBody)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	footer := muted.Render("hjkl focus | t terminal | i edit | Ctrl+n shell | A attach | Ctrl+Q detach | Ctrl+] back | J/K scroll | r refresh | q quit")

	parts := []string{header, body}
	if len(m.Errors) > 0 {
		parts = append(parts, errorStyle.Render("Errors: "+strings.Join(m.Errors, " | ")))
	}
	parts = append(parts, footer)
	return strings.Join(parts, "\n")
}

func (m Model) Init() tea.Cmd {
	return InitCmds(m.Transport, m.TerminalMgr)
}
