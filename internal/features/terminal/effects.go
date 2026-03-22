package terminal

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
)

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

type CaptureFullResultMsg struct {
	SessionID string
	Lines     []string
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

// AttachCmd bridges the existing detached PTY to stdin/stdout for fullscreen
// interactive use. Uses the same single-client approach as Claude Squad:
// no second tmux attach-session, just io.Copy + raw stdin forwarding.
func AttachCmd(mgr *Manager, sessionID string) tea.Cmd {
	if sessionID == "" {
		return nil
	}
	// We need to set up the attach (get the done channel) before suspending
	// Bubble Tea, but the actual bridging runs in goroutines that need raw
	// terminal access. So: set up raw mode + attach inside the ExecProcess
	// callback won't work. Instead, use tea.ExitAltScreen, then a blocking
	// cmd that waits on the attach channel, then tea.EnterAltScreen.
	return tea.Sequence(
		tea.ExitAltScreen,
		attachBlockingCmd(mgr, sessionID),
		tea.EnterAltScreen,
	)
}

func attachBlockingCmd(mgr *Manager, sessionID string) tea.Cmd {
	return func() tea.Msg {
		// Put terminal in raw mode so keystrokes go straight through.
		oldState, err := term.MakeRaw(os.Stdin.Fd())
		if err != nil {
			return AttachResultMsg{SessionID: sessionID, Err: err}
		}

		doneCh, err := mgr.Attach(sessionID)
		if err != nil {
			_ = term.Restore(os.Stdin.Fd(), oldState)
			return AttachResultMsg{SessionID: sessionID, Err: err}
		}

		// Block until detach (Ctrl+Q) or session end.
		<-doneCh

		_ = term.Restore(os.Stdin.Fd(), oldState)
		return AttachResultMsg{SessionID: sessionID, Err: nil}
	}
}

func CaptureFullCmd(mgr *Manager, sessionID string) tea.Cmd {
	return func() tea.Msg {
		if sessionID == "" {
			return CaptureFullResultMsg{SessionID: sessionID, Lines: nil, Err: nil}
		}
		lines, err := mgr.CaptureFull(sessionID)
		return CaptureFullResultMsg{SessionID: sessionID, Lines: lines, Err: err}
	}
}

func ResizeAllCmd(mgr *Manager, width, height int) tea.Cmd {
	return func() tea.Msg {
		mgr.ResizeAll(width, height)
		return nil
	}
}

func ShutdownCmd(mgr *Manager) tea.Cmd {
	return func() tea.Msg {
		mgr.Shutdown()
		return nil
	}
}
