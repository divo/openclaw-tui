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
	"openclaw-tui/internal/features/terminal"
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

	m.TerminalPane = terminal.Reduce(m.TerminalPane, incoming)

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
			m.ChatPane = chat.BeginPendingSend(m.ChatPane)
			return m, chat.SendChatCmd(
				m.Transport,
				m.SessionKey,
				m.ChatPane.ActivePrompt,
				m.ChatPane.ActiveMsgID,
				m.ChatPane.ActiveAttempt,
			)
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

	case msg.ChatRetryPendingMsg:
		if m.ChatPane.PendingMsg == "" {
			return m, nil
		}
		if m.Conn != ConnConnected || m.SessionKey == "" {
			m.ChatPane.Lines = append(m.ChatPane.Lines, "⏳ waiting for connection before retry...")
			return m, DiscoverSessionCmd(m.Transport)
		}
		m.ChatPane = chat.BeginPendingSend(m.ChatPane)
		return m, chat.SendChatCmd(
			m.Transport,
			m.SessionKey,
			m.ChatPane.ActivePrompt,
			m.ChatPane.ActiveMsgID,
			m.ChatPane.ActiveAttempt,
		)

	case terminal.EventMsg:
		return m, terminal.WaitEventCmd(m.TerminalMgr)

	case terminal.StartSessionResultMsg:
		return m, nil

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
		if m.Focus == ui.PaneChat {
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
					m.ChatPane = chat.QueueForReconnect(m.ChatPane, "waiting for connection")
					m.ChatPane.Lines = append(m.ChatPane.Lines, "⏳ reconnecting session...")
					return m, DiscoverSessionCmd(m.Transport)
				}
				m.ChatPane = chat.BeginSend(m.ChatPane)
				return m, chat.SendChatCmd(
					m.Transport,
					m.SessionKey,
					m.ChatPane.ActivePrompt,
					m.ChatPane.ActiveMsgID,
					m.ChatPane.ActiveAttempt,
				)
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

		if m.Focus == ui.PaneTerminal {
			active := m.TerminalPane.ActiveSession()
			switch k.String() {
			case "esc":
				m.Mode = ui.ModeMove
				m.TerminalPane.CommandMode = false
				m.TerminalPane.PendingCommand = ""
				return m, nil
			case "ctrl+n":
				m.TerminalPane.CommandMode = true
				m.TerminalPane.PendingCommand = ""
				m.TerminalPane.SetStatus("new session command: shell | claude | ssh <host>", false)
				return m, nil
			}

			if m.TerminalPane.CommandMode {
				switch k.String() {
				case "enter":
					spec, err := terminal.ParseCreateCommand(m.TerminalPane.PendingCommand)
					m.TerminalPane.CommandMode = false
					m.TerminalPane.PendingCommand = ""
					if err != nil {
						m.TerminalPane.SetStatus(err.Error(), true)
						return m, nil
					}
					return m, terminal.StartSessionCmd(m.TerminalMgr, spec)
				case "backspace", "ctrl+h":
					m.TerminalPane.PendingCommand = trimLastRune(m.TerminalPane.PendingCommand)
					return m, nil
				default:
					if len(k.Runes) > 0 {
						m.TerminalPane.PendingCommand += string(k.Runes)
					}
					return m, nil
				}
			}

			if active == nil {
				m.TerminalPane.SetStatus("no active session; press Ctrl+n and run shell/claude/ssh <host>", true)
				return m, nil
			}
			return m, forwardTerminalKey(active.ID, k, m.TerminalMgr)
		}
	}

	switch k.String() {
	case "q", "ctrl+c":
		return m, tea.Sequence(terminal.ShutdownCmd(m.TerminalMgr), tea.Quit)
	case "r":
		m.Status = "Refreshing..."
		return m, tea.Batch(RefreshCmd(m.Transport), DiscoverSessionCmd(m.Transport))
	case "i":
		if m.Focus == ui.PaneChat || m.Focus == ui.PaneTerminal {
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
	case "n":
		if m.Focus == ui.PaneTerminal {
			m.TerminalPane.NextSession()
		}
		return m, nil
	case "p":
		if m.Focus == ui.PaneTerminal {
			m.TerminalPane.PrevSession()
		}
		return m, nil
	case "x":
		if m.Focus == ui.PaneTerminal {
			active := m.TerminalPane.ActiveSession()
			if active != nil {
				return m, terminal.KillSessionCmd(m.TerminalMgr, active.ID)
			}
		}
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
	case ui.PaneTerminal:
		active := m.TerminalPane.ActiveSession()
		if active != nil {
			active.Scrollback = max(0, active.Scrollback+delta)
		}
	}
}

func forwardTerminalKey(sessionID string, k tea.KeyMsg, mgr *terminal.Manager) tea.Cmd {
	switch k.String() {
	case "enter":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\r"))
	case "tab":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\t"))
	case "backspace", "ctrl+h":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte{0x7f})
	case "up":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[A"))
	case "down":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[B"))
	case "left":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[D"))
	case "right":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[C"))
	}
	if b, ok := ctrlKeyByte(k.String()); ok {
		return terminal.WriteActiveCmd(mgr, sessionID, []byte{b})
	}
	if len(k.Runes) > 0 {
		return terminal.WriteActiveCmd(mgr, sessionID, []byte(string(k.Runes)))
	}
	return nil
}

func ctrlKeyByte(s string) (byte, bool) {
	if !strings.HasPrefix(s, "ctrl+") || len(s) != len("ctrl+")+1 {
		return 0, false
	}
	r := s[len(s)-1]
	if r >= 'a' && r <= 'z' {
		return r - 'a' + 1, true
	}
	return 0, false
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
