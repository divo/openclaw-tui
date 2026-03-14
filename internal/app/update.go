package app

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"openclaw-tui/internal/features/chat"
	"openclaw-tui/internal/features/sessions"
	"openclaw-tui/internal/features/status"
	"openclaw-tui/internal/features/tasks"
	"openclaw-tui/internal/msg"
	"openclaw-tui/internal/transport"
	"openclaw-tui/internal/ui"
)

func (m Model) Update(incoming tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := Reduce(m, incoming)
	return next, cmd
}

func Reduce(m Model, incoming tea.Msg) (Model, tea.Cmd) {
	nextChat, rr := chat.Reduce(m.ChatPane, incoming)
	m.ChatPane = nextChat
	if rr.NeedSessionDiscover {
		m.Conn = ConnDisconnected
		return m, DiscoverSessionCmd(m.Transport)
	}
	if rr.Cmd != nil {
		return m, rr.Cmd
	}

	switch x := incoming.(type) {
	case msg.SessionDiscoverMsg:
		if x.Err != nil {
			m.Conn = ConnDisconnected
			m.Errors = append(m.Errors, "session: "+x.Err.Error())
			return m, ScheduleReconnect(m.Transport, 3*time.Second)
		}
		if m.SessionKey != x.SessionKey {
			m.ChatPane.Lines = append(m.ChatPane.Lines, "⚡ connected → "+x.SessionKey)
		}
		m.SessionKey = x.SessionKey
		m.Conn = ConnConnected
		if m.ChatPane.PendingMsg != "" {
			pending := m.ChatPane.PendingMsg
			m.ChatPane.PendingMsg = ""
			m.ChatPane.Sending = true
			if m.ChatPane.StartedAt.IsZero() {
				m.ChatPane.StartedAt = time.Now()
			}
			return m, chat.SendChatCmd(m.Transport, m.SessionKey, pending, 0)
		}
		return m, nil

	case msg.RefreshMsg:
		m.LastRefresh = x.At
		m.Status = "Live"
		m.Errors = x.Errors
		m.StatusPane = status.Reduce(m.StatusPane, x)
		m.SessionsPane = sessions.Reduce(m.SessionsPane, x)
		m.TasksPane = tasks.Reduce(m.TasksPane, x)

		if m.Conn == ConnConnected && m.SessionKey != "" {
			if transport.ParseMainSessionKey(x.SessionsRaw) == "" {
				m.Conn = ConnDisconnected
				m.ChatPane.Lines = append(m.ChatPane.Lines, "⚠ session lost — reconnecting...")
				return m, tea.Batch(TickCmd(m.Transport), DiscoverSessionCmd(m.Transport))
			}
		}
		return m, TickCmd(m.Transport)

	case tea.WindowSizeMsg:
		m.Width = x.Width
		m.Height = x.Height
		return m, nil

	case tea.KeyMsg:
		return reduceKey(m, x)
	}

	return m, nil
}

func reduceKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if m.Mode == ui.ModeEdit {
		switch k.String() {
		case "esc":
			m.Mode = ui.ModeMove
			return m, nil
		case "enter":
			if m.ChatPane.Sending {
				return m, nil
			}
			prompt := strings.TrimSpace(m.ChatPane.Input)
			if prompt == "" {
				return m, nil
			}
			m.ChatPane = chat.StartSend(m.ChatPane, prompt)
			if m.Conn != ConnConnected || m.SessionKey == "" {
				m.ChatPane.PendingMsg = prompt
				m.ChatPane.Lines = append(m.ChatPane.Lines, "⏳ not connected — queued, reconnecting...")
				return m, DiscoverSessionCmd(m.Transport)
			}
			return m, chat.SendChatCmd(m.Transport, m.SessionKey, prompt, 0)
		case "backspace", "ctrl+h":
			m.ChatPane.Input = trimLastRune(m.ChatPane.Input)
			return m, nil
		default:
			if len(k.Runes) > 0 {
				m.ChatPane.Input += string(k.Runes)
			}
			return m, nil
		}
	}

	switch k.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "r":
		m.Status = "Refreshing..."
		return m, tea.Batch(RefreshCmd(m.Transport), DiscoverSessionCmd(m.Transport))
	case "i":
		if m.Focus == ui.PaneChat {
			m.Mode = ui.ModeEdit
		}
		return m, nil
	case "h":
		m.Focus = ui.FocusLeft(m.Focus)
		return m, nil
	case "l":
		m.Focus = ui.FocusRight(m.Focus)
		return m, nil
	case "j":
		m.Focus = ui.FocusDown(m.Focus)
		return m, nil
	case "k":
		m.Focus = ui.FocusUp(m.Focus)
		return m, nil
	case "J":
		scrollFocused(&m, 1)
		return m, nil
	case "K":
		scrollFocused(&m, -1)
		return m, nil
	case "ctrl+d":
		scrollFocused(&m, 5)
		return m, nil
	case "ctrl+u":
		scrollFocused(&m, -5)
		return m, nil
	}
	return m, nil
}

func scrollFocused(m *Model, delta int) {
	switch m.Focus {
	case ui.PaneStatus:
		m.StatusPane.Offset = max(0, m.StatusPane.Offset+delta)
	case ui.PaneSessions:
		m.SessionsPane.Offset = max(0, m.SessionsPane.Offset+delta)
	case ui.PaneTasks:
		m.TasksPane.Offset = max(0, m.TasksPane.Offset+delta)
	case ui.PaneChat:
		m.ChatPane.Offset = max(0, m.ChatPane.Offset+delta)
	}
}

func trimLastRune(s string) string {
	if s == "" {
		return s
	}
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
