package terminal

import tea "github.com/charmbracelet/bubbletea"

type EventMsg struct {
	Event Event
}

type StartSessionMsg struct {
	Spec SessionSpec
}

type StartSessionResultMsg struct {
	Spec SessionSpec
	Err  error
}

type AttachResultMsg struct {
	SessionID string
	Err       error
}

func WaitEventCmd(mgr *Manager) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-mgr.Events()
		if !ok {
			return EventMsg{Event: ManagerErrorEvent{Err: "terminal manager closed"}}
		}
		return EventMsg{Event: evt}
	}
}

func StartSessionCmd(mgr *Manager, spec SessionSpec) tea.Cmd {
	return func() tea.Msg {
		err := mgr.Start(spec)
		return StartSessionResultMsg{Spec: spec, Err: err}
	}
}

func WriteActiveCmd(mgr *Manager, sessionID string, data []byte) tea.Cmd {
	return func() tea.Msg {
		if sessionID == "" || len(data) == 0 {
			return nil
		}
		if err := mgr.Write(sessionID, data); err != nil {
			return EventMsg{Event: ManagerErrorEvent{Err: err.Error()}}
		}
		return nil
	}
}

func KillSessionCmd(mgr *Manager, sessionID string) tea.Cmd {
	return func() tea.Msg {
		if sessionID == "" {
			return nil
		}
		if err := mgr.Kill(sessionID); err != nil {
			return EventMsg{Event: ManagerErrorEvent{Err: err.Error()}}
		}
		return nil
	}
}

func AttachCmd(mgr *Manager, sessionID string) tea.Cmd {
	if sessionID == "" {
		return nil
	}
	cmd, err := mgr.AttachCommand(sessionID)
	if err != nil {
		return func() tea.Msg {
			return AttachResultMsg{SessionID: sessionID, Err: err}
		}
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return AttachResultMsg{SessionID: sessionID, Err: err}
	})
}

func ShutdownCmd(mgr *Manager) tea.Cmd {
	return func() tea.Msg {
		mgr.Shutdown()
		return nil
	}
}
