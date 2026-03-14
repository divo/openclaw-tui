package app

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"openclaw-tui/internal/features/chat"
	"openclaw-tui/internal/features/tasks"
	"openclaw-tui/internal/msg"
	"openclaw-tui/internal/transport"
)

const refreshInterval = 5 * time.Second

func InitCmds(t transport.Transport) tea.Cmd {
	return tea.Batch(DiscoverSessionCmd(t), RefreshCmd(t), TickCmd(t), chat.UITickCmd())
}

func TickCmd(t transport.Transport) tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return runRefresh(t) })
}

func RefreshCmd(t transport.Transport) tea.Cmd {
	return func() tea.Msg { return runRefresh(t) }
}

func DiscoverSessionCmd(t transport.Transport) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		key, err := t.DiscoverMainSession(ctx)
		if err != nil {
			return msg.SessionDiscoverMsg{Err: err}
		}
		return msg.SessionDiscoverMsg{SessionKey: key}
	}
}

func ScheduleReconnect(t transport.Transport, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return DiscoverSessionCmd(t)()
	})
}

func runRefresh(t transport.Transport) tea.Msg {
	result := msg.RefreshResult{At: time.Now()}

	ctxStatus, cancelStatus := context.WithTimeout(context.Background(), 5*time.Second)
	statusOut, err := t.StatusAll(ctxStatus)
	cancelStatus()
	if err != nil {
		result.Errors = append(result.Errors, "status: "+err.Error())
	}
	result.StatusRaw = statusOut

	ctxSessions, cancelSessions := context.WithTimeout(context.Background(), 5*time.Second)
	sessionsOut, err := t.SessionsList(ctxSessions)
	cancelSessions()
	if err != nil {
		result.Errors = append(result.Errors, "sessions: "+err.Error())
	}
	result.SessionsRaw = sessionsOut

	result.TaskItems = tasks.ReadTaskItems(tasksPath, 12)
	if len(result.TaskItems) == 0 {
		result.TaskItems = []msg.TaskItem{{Priority: 3, Text: "No open tasks found"}}
	}

	return msg.RefreshMsg(result)
}
