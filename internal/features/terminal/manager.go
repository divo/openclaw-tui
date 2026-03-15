package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
)

type SessionSpec struct {
	Name string
	Type string
	Cmd  string
	Args []string
	Env  []string
}

func ShellSpec() SessionSpec {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "bash"
	}
	// Force interactive mode so prompts render in the detached capture pane.
	// Without -i some shells behave non-interactively (no prompt), which looks
	// like a dead "(no output yet)" pane.
	return SessionSpec{Name: "shell", Type: SessionTypeShell, Cmd: shell, Args: []string{"-i"}}
}

func ClaudeSpec() SessionSpec {
	return SessionSpec{Name: "claude", Type: SessionTypeClaude, Cmd: "claude"}
}

func SSHSpec(host string) SessionSpec {
	return SessionSpec{Name: "ssh " + host, Type: SessionTypeSSH, Cmd: "ssh", Args: []string{host}}
}

type SessionMeta struct {
	ID       string
	Name     string
	Type     string
	Status   string
	Err      string
	ExitCode int
}

type Event interface{ isTerminalEvent() }

type SessionEvent struct{ Meta SessionMeta }

type CaptureEvent struct {
	SessionID string
	Lines     []string
}

type ExitEvent struct {
	SessionID string
	ExitCode  int
	Err       string
}

type ManagerErrorEvent struct{ Err string }

func (SessionEvent) isTerminalEvent()      {}
func (CaptureEvent) isTerminalEvent()      {}
func (ExitEvent) isTerminalEvent()         {}
func (ManagerErrorEvent) isTerminalEvent() {}

type runtimeSession struct {
	id           string
	spec         SessionSpec
	tmuxSession  *tmuxSession
	lastSnapshot string
	attaching    bool
}

type Manager struct {
	mu          sync.Mutex
	sessions    map[string]*runtimeSession
	nextID      int
	events      chan Event
	stopCh      chan struct{}
	desiredCols int
	desiredRows int
}

func NewManager() *Manager {
	m := &Manager{
		sessions: map[string]*runtimeSession{},
		events:   make(chan Event, 256),
		stopCh:   make(chan struct{}),
	}
	go m.pollLoop()
	return m
}

func (m *Manager) Events() <-chan Event { return m.events }

func (m *Manager) Start(spec SessionSpec) error {
	if spec.Cmd == "" {
		return fmt.Errorf("empty command")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		err = fmt.Errorf("tmux not found in PATH")
		m.emit(ManagerErrorEvent{Err: err.Error()})
		return err
	}

	m.mu.Lock()
	m.nextID++
	idNum := m.nextID
	id := fmt.Sprintf("t%03d", idNum)
	tmuxName := fmt.Sprintf("octui_%s_%03d_%d", sanitizeName(spec.Type), idNum, time.Now().UnixNano()%1_000_000)
	m.mu.Unlock()

	m.emit(SessionEvent{Meta: SessionMeta{ID: id, Name: spec.Name, Type: spec.Type, Status: SessionStatusStarting}})

	t := newTmuxSession(tmuxName)
	if err := t.Start(spec); err != nil {
		msg := err.Error()
		m.emit(ManagerErrorEvent{Err: fmt.Sprintf("start %s: %s", spec.Name, msg)})
		m.emit(SessionEvent{Meta: SessionMeta{ID: id, Name: spec.Name, Type: spec.Type, Status: SessionStatusError, Err: msg}})
		return err
	}

	rs := &runtimeSession{id: id, spec: spec, tmuxSession: t}
	m.mu.Lock()
	m.sessions[id] = rs
	cols, rows := m.desiredCols, m.desiredRows
	m.mu.Unlock()

	if cols > 0 && rows > 0 {
		_ = t.SetDetachedSize(cols, rows)
	}

	m.emit(SessionEvent{Meta: SessionMeta{ID: id, Name: spec.Name, Type: spec.Type, Status: SessionStatusRunning}})

	// Nudge interactive shells so the first prompt is guaranteed to render into
	// capture-pane (avoids confusing "(no output yet)" on startup).
	if spec.Type == SessionTypeShell {
		_ = t.SendKeys([]byte("\r"))
	}

	if lines, snap, err := t.Capture(300); err == nil {
		m.mu.Lock()
		if s, ok := m.sessions[id]; ok {
			s.lastSnapshot = snap
		}
		m.mu.Unlock()
		m.emit(CaptureEvent{SessionID: id, Lines: lines})
	}
	return nil
}

// Write sends bytes to the detached tmux PTY so terminal interaction can
// happen directly in the embedded pane (capture updates rendered in-pane).
func (m *Manager) Write(sessionID string, data []byte) error {
	rs, err := m.getSession(sessionID)
	if err != nil {
		return err
	}
	return rs.tmuxSession.SendKeys(data)
}

func (m *Manager) Kill(sessionID string) error {
	rs, err := m.getSession(sessionID)
	if err != nil {
		return err
	}
	if err := rs.tmuxSession.Close(); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	m.emit(ExitEvent{SessionID: sessionID, ExitCode: 0})
	return nil
}

// PrepareAttach transitions a session from detached-capture mode to attached
// interactive mode by releasing the detached PTY first.
func (m *Manager) PrepareAttach(sessionID string) (*exec.Cmd, error) {
	rs, err := m.getSession(sessionID)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	if rs.attaching {
		m.mu.Unlock()
		return nil, fmt.Errorf("session %s already attaching", sessionID)
	}
	rs.attaching = true
	m.mu.Unlock()

	if err := rs.tmuxSession.ReleaseDetached(); err != nil {
		m.mu.Lock()
		rs.attaching = false
		m.mu.Unlock()
		return nil, err
	}

	if w, h, e := term.GetSize(os.Stdout.Fd()); e == nil {
		_ = rs.tmuxSession.SetDetachedSize(w, h)
	}
	_, _ = runTmux("bind-key", "-n", "C-q", "detach-client")

	cmd := exec.Command("tmux", "attach-session", "-t", "="+rs.tmuxSession.name)
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd, nil
}

// FinishAttach restores detached capture mode after interactive attach exits.
func (m *Manager) FinishAttach(sessionID string) error {
	rs, err := m.getSession(sessionID)
	if err != nil {
		return err
	}
	defer func() {
		m.mu.Lock()
		rs.attaching = false
		m.mu.Unlock()
	}()
	if err := rs.tmuxSession.Restore(); err != nil {
		return err
	}
	m.mu.Lock()
	cols, rows := m.desiredCols, m.desiredRows
	m.mu.Unlock()
	if cols > 0 && rows > 0 {
		_ = rs.tmuxSession.SetDetachedSize(cols, rows)
	}
	return nil
}

func (m *Manager) CaptureFull(sessionID string) ([]string, error) {
	rs, err := m.getSession(sessionID)
	if err != nil {
		return nil, err
	}
	lines, _, err := rs.tmuxSession.CaptureRange("-", "-")
	if err != nil {
		return nil, err
	}
	return lines, nil
}

func (m *Manager) ResizeAll(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	m.mu.Lock()
	m.desiredCols, m.desiredRows = width, height
	sessions := make([]*runtimeSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()

	for _, s := range sessions {
		if err := s.tmuxSession.SetDetachedSize(width, height); err != nil {
			m.emit(ManagerErrorEvent{Err: fmt.Sprintf("resize %s: %v", s.tmuxSession.name, err)})
		}
	}
}

func (m *Manager) Shutdown() {
	select {
	case <-m.stopCh:
	default:
		close(m.stopCh)
	}

	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Kill(id)
	}
}

func (m *Manager) pollLoop() {
	ticker := time.NewTicker(60 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.pollOnce()
		case <-m.stopCh:
			return
		}
	}
}

func (m *Manager) pollOnce() {
	type item struct {
		id   string
		sess *tmuxSession
		last string
	}
	m.mu.Lock()
	items := make([]item, 0, len(m.sessions))
	for _, s := range m.sessions {
		items = append(items, item{id: s.id, sess: s.tmuxSession, last: s.lastSnapshot})
	}
	m.mu.Unlock()

	for _, it := range items {
		has, err := it.sess.Exists()
		if err != nil {
			m.emit(ManagerErrorEvent{Err: "tmux has-session: " + err.Error()})
			continue
		}
		if !has {
			m.mu.Lock()
			delete(m.sessions, it.id)
			m.mu.Unlock()
			m.emit(ExitEvent{SessionID: it.id, ExitCode: 0})
			continue
		}

		lines, snap, err := it.sess.Capture(300)
		if err != nil {
			m.emit(ManagerErrorEvent{Err: "tmux capture: " + err.Error()})
			continue
		}
		if snap == it.last {
			continue
		}
		m.mu.Lock()
		if s, ok := m.sessions[it.id]; ok {
			s.lastSnapshot = snap
		}
		m.mu.Unlock()
		m.emit(CaptureEvent{SessionID: it.id, Lines: lines})
	}
}

func (m *Manager) getSession(sessionID string) (*runtimeSession, error) {
	m.mu.Lock()
	rs, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return rs, nil
}

func (m *Manager) emit(evt Event) {
	select {
	case m.events <- evt:
	default:
		select {
		case m.events <- ManagerErrorEvent{Err: "terminal event queue full"}:
		default:
		}
	}
}

type tmuxSession struct {
	name string
	ptmx *os.File
}

func newTmuxSession(name string) *tmuxSession {
	return &tmuxSession{name: name}
}

func (t *tmuxSession) Start(spec SessionSpec) error {
	args := []string{"new-session", "-d", "-s", t.name, spec.Cmd}
	args = append(args, spec.Args...)
	cmd := exec.Command("tmux", args...)
	cmd.Env = append(os.Environ(), spec.Env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}

	_, _ = runTmux("set-option", "-t", t.name, "history-limit", "10000")
	_, _ = runTmux("set-option", "-t", t.name, "mouse", "on")

	return t.Restore()
}

func (t *tmuxSession) Restore() error {
	if t.ptmx != nil {
		return nil
	}
	ptmx, err := pty.Start(exec.Command("tmux", "attach-session", "-t", "="+t.name))
	if err != nil {
		return fmt.Errorf("open detached tmux pty: %w", err)
	}
	t.ptmx = ptmx
	return nil
}

func (t *tmuxSession) ReleaseDetached() error {
	if t.ptmx == nil {
		return nil
	}
	err := t.ptmx.Close()
	t.ptmx = nil
	if err != nil {
		return fmt.Errorf("close detached tmux pty: %w", err)
	}
	return nil
}

func (t *tmuxSession) Close() error {
	_ = t.ReleaseDetached()
	_, err := runTmux("kill-session", "-t", "="+t.name)
	if err != nil {
		return err
	}
	return nil
}

func (t *tmuxSession) Exists() (bool, error) {
	_, err := runTmux("has-session", "-t", "="+t.name)
	if err == nil {
		return true, nil
	}
	raw := strings.ToLower(err.Error())
	if strings.Contains(raw, "can't find session") || strings.Contains(raw, "exit status 1") {
		return false, nil
	}
	return false, err
}

func (t *tmuxSession) Capture(historyLines int) ([]string, string, error) {
	start := fmt.Sprintf("-%d", historyLines)
	return t.CaptureRange(start, "")
}

func (t *tmuxSession) CaptureRange(start, end string) ([]string, string, error) {
	args := []string{"capture-pane", "-p", "-e", "-J", "-t", t.paneTarget()}
	if start != "" {
		args = append(args, "-S", start)
	}
	if end != "" {
		args = append(args, "-E", end)
	}
	out, err := runTmux(args...)
	if err != nil {
		return nil, "", err
	}
	snap := strings.ReplaceAll(out, "\r\n", "\n")
	snap = strings.ReplaceAll(snap, "\r", "\n")
	snap = strings.TrimRight(snap, "\n")
	if snap == "" {
		return nil, "", nil
	}
	return strings.Split(snap, "\n"), snap, nil
}

func (t *tmuxSession) SetDetachedSize(width, height int) error {
	if width <= 0 || height <= 0 || t.ptmx == nil {
		return nil
	}
	return pty.Setsize(t.ptmx, &pty.Winsize{Rows: uint16(height), Cols: uint16(width)})
}

func (t *tmuxSession) SendKeys(data []byte) error {
	if t.ptmx == nil {
		return fmt.Errorf("tmux detached pty not ready")
	}
	_, err := t.ptmx.Write(data)
	return err
}

func runTmux(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			return "", err
		}
		return "", fmt.Errorf("%w | %s", err, text)
	}
	return text, nil
}

func sendBytesToTmux(tmuxSession string, data []byte) error {
	target := paneTarget(tmuxSession)
	for len(data) > 0 {
		if len(data) >= 3 && data[0] == 0x1b && data[1] == '[' {
			switch data[2] {
			case 'A':
				if _, err := runTmux("send-keys", "-t", target, "Up"); err != nil {
					return err
				}
				data = data[3:]
				continue
			case 'B':
				if _, err := runTmux("send-keys", "-t", target, "Down"); err != nil {
					return err
				}
				data = data[3:]
				continue
			case 'C':
				if _, err := runTmux("send-keys", "-t", target, "Right"); err != nil {
					return err
				}
				data = data[3:]
				continue
			case 'D':
				if _, err := runTmux("send-keys", "-t", target, "Left"); err != nil {
					return err
				}
				data = data[3:]
				continue
			}
		}

		b := data[0]
		switch {
		case b == '\r' || b == '\n':
			if _, err := runTmux("send-keys", "-t", target, "Enter"); err != nil {
				return err
			}
			data = data[1:]
		case b == '\t':
			if _, err := runTmux("send-keys", "-t", target, "Tab"); err != nil {
				return err
			}
			data = data[1:]
		case b == 0x7f:
			if _, err := runTmux("send-keys", "-t", target, "BSpace"); err != nil {
				return err
			}
			data = data[1:]
		case b >= 1 && b <= 26:
			ctrl := "C-" + string('a'+(b-1))
			if _, err := runTmux("send-keys", "-t", target, ctrl); err != nil {
				return err
			}
			data = data[1:]
		default:
			i := 0
			for i < len(data) {
				c := data[i]
				if c == 0x1b || c == '\r' || c == '\n' || c == '\t' || c == 0x7f || (c >= 1 && c <= 26) {
					break
				}
				i++
			}
			if i == 0 {
				data = data[1:]
				continue
			}
			lit := string(data[:i])
			if _, err := runTmux("send-keys", "-t", target, "-l", lit); err != nil {
				return err
			}
			data = data[i:]
		}
	}
	return nil
}

func (t *tmuxSession) paneTarget() string {
	return paneTarget(t.name)
}

func paneTarget(session string) string {
	return session + ":0.0"
}

func sanitizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "term"
	}
	repl := strings.NewReplacer(" ", "_", "/", "_", ":", "_", ".", "_")
	return repl.Replace(s)
}
