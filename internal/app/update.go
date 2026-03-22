package app

import (
	"fmt"
	"os"
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
		if x.SessionFilePath != "" {
			m.SessionFilePath = x.SessionFilePath
		}
		m.Conn = ConnConnected
		if m.ChatPane.PendingMsg != "" {
			m.ChatPane = chat.BeginPendingSend(m.ChatPane)
			if m.SessionFilePath != "" {
				if info, err := os.Stat(m.SessionFilePath); err == nil {
					m.ChatPane.TailOffset = info.Size()
				}
				m.ChatPane.TailFilePath = m.SessionFilePath
				return m, chat.SendAgentFireCmd(
					m.Transport,
					m.SessionKey,
					m.ChatPane.ActivePrompt,
					m.ChatPane.ActiveMsgID,
				)
			}
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

	case msg.ChatAgentFiredMsg:
		// Agent turn started in background — begin tailing the JSONL file.
		m.ChatPane.Lines = append(m.ChatPane.Lines,
			fmt.Sprintf("↳ [%03d] queued — watching for reply...", x.MessageID),
		)
		m.ChatPane.FollowTail = true
		m.ChatPane.Tailing = true
		return m, chat.TailCmd(m.Transport, m.ChatPane.TailFilePath, m.ChatPane.TailOffset, x.Done)

	case msg.ChatTailMsg:
		m.ChatPane.TailOffset = x.NewOffset
		newState, done := chat.ProcessTailLines(m.ChatPane, x)
		m.ChatPane = newState
		if done {
			m.ChatPane.Tailing = false
			return m, nil
		}
		// Not done yet — schedule next poll, passing nil for agentDone since we
		// already checked it above and don't have it anymore.
		return m, chat.TailCmd(m.Transport, m.ChatPane.TailFilePath, m.ChatPane.TailOffset, nil)

	case terminal.EventMsg:
		return m, terminal.WaitEventCmd(m.TerminalMgr)

	case terminal.StartSessionResultMsg:
		return m, nil

	case terminal.AttachResultMsg:
		// After attach returns, keep focus in MOVE mode.
		m.Mode = ui.ModeMove
		return m, nil

	case terminal.CaptureFullResultMsg:
		return m, nil

	case tea.WindowSizeMsg:
		m.Width = x.Width
		m.Height = x.Height
		dims := ui.ComputeDimensions(m.Width, m.Height)
		// Detached in-pane terminal should match pane dimensions and only update
		// when effective cols/rows actually change.
		termW := max(20, dims.RightW-2)
		termH := max(6, dims.TerminalH-2)
		if m.TerminalPane.Cols == termW && m.TerminalPane.Rows == termH {
			return m, nil
		}
		m.TerminalPane.RecordResize(termW, termH, "window")
		return m, terminal.ResizeAllCmd(m.TerminalMgr, termW, termH)

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
			case "enter", "ctrl+m":
				if m.ChatPane.Sending || m.ChatPane.Tailing {
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
				// Async path: fire agent in background, tail JSONL for reply.
				if m.SessionFilePath != "" {
					if info, err := os.Stat(m.SessionFilePath); err == nil {
						m.ChatPane.TailOffset = info.Size()
					}
					m.ChatPane.TailFilePath = m.SessionFilePath
					m.ChatPane = chat.BeginSendAsync(m.ChatPane)
					return m, chat.SendAgentFireCmd(
						m.Transport,
						m.SessionKey,
						m.ChatPane.ActivePrompt,
						m.ChatPane.ActiveMsgID,
					)
				}
				// Fallback: sync path (no session file available).
				m.ChatPane = chat.BeginSend(m.ChatPane)
				return m, chat.SendChatCmd(
					m.Transport,
					m.SessionKey,
					m.ChatPane.ActivePrompt,
					m.ChatPane.ActiveMsgID,
					m.ChatPane.ActiveAttempt,
				)
			case "up":
				m.ChatPane = chat.HistoryPrev(m.ChatPane)
				return m, nil
			case "down":
				m.ChatPane = chat.HistoryNext(m.ChatPane)
				return m, nil
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

			// Terminal command entry mode (Ctrl+t) has app-owned keys.
			if m.TerminalPane.CommandMode {
				switch k.String() {
				case "esc":
					m.TerminalPane.CommandMode = false
					m.TerminalPane.PendingCommand = ""
					m.TerminalPane.SetStatus("terminal input mode", false)
					return m, nil
				case "enter", "ctrl+m":
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

			// No active terminal session yet: allow session creation controls.
			if active == nil {
				switch k.String() {
				case "ctrl+n":
					m.TerminalPane.SetStatus("starting shell tmux session...", false)
					return m, terminal.StartSessionCmd(m.TerminalMgr, terminal.ShellSpec())
				case "ctrl+t":
					m.TerminalPane.CommandMode = true
					m.TerminalPane.PendingCommand = ""
					m.TerminalPane.SetStatus("new tmux session: shell | claude | ssh <host>", false)
					return m, nil
				case "esc":
					m.Mode = ui.ModeMove
					return m, nil
				default:
					m.TerminalPane.SetStatus("no active session; press Ctrl+n for shell (or Ctrl+t for custom)", true)
					return m, nil
				}
			}

			// Terminal input mode: forward everything except Ctrl+] app escape hatch.
			if k.String() == "ctrl+]" {
				m.Mode = ui.ModeMove
				m.TerminalPane.SetStatus("terminal MOVE mode", false)
				return m, nil
			}
			return m, forwardTerminalKey(active.ID, k, m.TerminalMgr)
		}
	}

	switch k.String() {
	case "esc":
		if m.Focus == ui.PaneTerminal && m.TerminalPane.IsScrolling {
			m.TerminalPane.ExitScrollMode()
			m.TerminalPane.SetStatus("exited scroll mode", false)
		}
		return m, nil
	case "q", "ctrl+c":
		return m, tea.Sequence(terminal.ShutdownCmd(m.TerminalMgr), tea.Quit)
	case "r":
		m.Status = "Refreshing..."
		return m, tea.Batch(RefreshCmd(m.Transport), DiscoverSessionCmd(m.Transport))
	case "ctrl+n":
		if m.Focus == ui.PaneTerminal {
			m.Mode = ui.ModeEdit
			m.TerminalPane.CommandMode = false
			m.TerminalPane.PendingCommand = ""
			m.TerminalPane.SetStatus("starting shell tmux session...", false)
			return m, terminal.StartSessionCmd(m.TerminalMgr, terminal.ShellSpec())
		}
		return m, nil
	case "ctrl+t":
		if m.Focus == ui.PaneTerminal {
			m.Mode = ui.ModeEdit
			m.TerminalPane.CommandMode = true
			m.TerminalPane.PendingCommand = ""
			m.TerminalPane.SetStatus("new tmux session: shell | claude | ssh <host>", false)
		}
		return m, nil
	case "i":
		if m.Focus == ui.PaneChat || m.Focus == ui.PaneTerminal {
			m.Mode = ui.ModeEdit
		}
		return m, nil
	case "t":
		m.Focus = ui.PaneTerminal
		m.Mode = ui.ModeMove
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
	case "enter", "ctrl+m", "a":
		if m.Focus == ui.PaneTerminal {
			active := m.TerminalPane.ActiveSession()
			if active != nil {
				m.Mode = ui.ModeEdit
				m.TerminalPane.SetStatus("terminal input mode (in-pane). Ctrl+] returns to MOVE", false)
				return m, nil
			}
			m.TerminalPane.SetStatus("no active session", true)
		}
		return m, nil
	case "A":
		if m.Focus == ui.PaneTerminal {
			active := m.TerminalPane.ActiveSession()
			if active != nil {
				m.TerminalPane.SetStatus("fullscreen attach... (detach with Ctrl+Q)", false)
				return m, terminal.AttachCmd(m.TerminalMgr, active.ID)
			}
			m.TerminalPane.SetStatus("no active session to attach", true)
		}
		return m, nil
	case "J":
		if m.Focus == ui.PaneTerminal && !m.TerminalPane.IsScrolling {
			active := m.TerminalPane.ActiveSession()
			if active != nil {
				return m, terminal.CaptureFullCmd(m.TerminalMgr, active.ID)
			}
		}
		scrollFocused(&m, 1)
		return m, nil
	case "K":
		if m.Focus == ui.PaneTerminal && !m.TerminalPane.IsScrolling {
			active := m.TerminalPane.ActiveSession()
			if active != nil {
				return m, terminal.CaptureFullCmd(m.TerminalMgr, active.ID)
			}
		}
		scrollFocused(&m, -1)
		return m, nil
	case "ctrl+d":
		if m.Focus == ui.PaneTerminal && !m.TerminalPane.IsScrolling {
			active := m.TerminalPane.ActiveSession()
			if active != nil {
				return m, terminal.CaptureFullCmd(m.TerminalMgr, active.ID)
			}
		}
		scrollFocused(&m, 5)
		return m, nil
	case "ctrl+u":
		if m.Focus == ui.PaneTerminal && !m.TerminalPane.IsScrolling {
			active := m.TerminalPane.ActiveSession()
			if active != nil {
				return m, terminal.CaptureFullCmd(m.TerminalMgr, active.ID)
			}
		}
		scrollFocused(&m, -5)
		return m, nil
	case "G":
		if m.Focus == ui.PaneChat {
			m.ChatPane = chat.FollowLatest(m.ChatPane)
		}
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
		m.ChatPane = chat.Scroll(m.ChatPane, delta)
	case ui.PaneTerminal:
		active := m.TerminalPane.ActiveSession()
		if active != nil {
			active.Scrollback = max(0, active.Scrollback+delta)
		}
	}
}

func forwardTerminalKey(sessionID string, k tea.KeyMsg, mgr *terminal.Manager) tea.Cmd {
	switch k.String() {
	case "esc":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte{0x1b})
	case "enter", "ctrl+m":
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
	case "home":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[H"))
	case "end":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[F"))
	case "insert":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[2~"))
	case "delete":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[3~"))
	case "pgup":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[5~"))
	case "pgdown":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[6~"))
	case "shift+tab":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[Z"))
	case "f1":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1bOP"))
	case "f2":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1bOQ"))
	case "f3":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1bOR"))
	case "f4":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1bOS"))
	case "f5":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[15~"))
	case "f6":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[17~"))
	case "f7":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[18~"))
	case "f8":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[19~"))
	case "f9":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[20~"))
	case "f10":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[21~"))
	case "f11":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[23~"))
	case "f12":
		return terminal.WriteActiveCmd(mgr, sessionID, []byte("\x1b[24~"))
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
