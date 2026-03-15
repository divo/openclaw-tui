package terminal

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
	return SessionSpec{Name: "shell", Type: SessionTypeShell, Cmd: shell}
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
	tmuxSession  string
	lastSnapshot string
}

type Manager struct {
	mu       sync.Mutex
	sessions map[string]*runtimeSession
	nextID   int
	events   chan Event
	stopCh   chan struct{}
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

	args := []string{"new-session", "-d", "-s", tmuxName, spec.Cmd}
	args = append(args, spec.Args...)
	cmd := exec.Command("tmux", args...)
	cmd.Env = append(os.Environ(), spec.Env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		m.emit(ManagerErrorEvent{Err: fmt.Sprintf("start %s: %s", spec.Name, msg)})
		m.emit(SessionEvent{Meta: SessionMeta{ID: id, Name: spec.Name, Type: spec.Type, Status: SessionStatusError, Err: msg}})
		return err
	}

	rs := &runtimeSession{id: id, spec: spec, tmuxSession: tmuxName}
	m.mu.Lock()
	m.sessions[id] = rs
	m.mu.Unlock()

	m.emit(SessionEvent{Meta: SessionMeta{ID: id, Name: spec.Name, Type: spec.Type, Status: SessionStatusRunning}})

	if lines, snap, err := m.capture(tmuxName, 300); err == nil {
		m.mu.Lock()
		if s, ok := m.sessions[id]; ok {
			s.lastSnapshot = snap
		}
		m.mu.Unlock()
		m.emit(CaptureEvent{SessionID: id, Lines: lines})
	}
	return nil
}

func (m *Manager) Write(sessionID string, data []byte) error {
	rs, err := m.getSession(sessionID)
	if err != nil {
		return err
	}
	return sendBytesToTmux(rs.tmuxSession, data)
}

func (m *Manager) Kill(sessionID string) error {
	rs, err := m.getSession(sessionID)
	if err != nil {
		return err
	}
	if _, err := runTmux("kill-session", "-t", rs.tmuxSession); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	m.emit(ExitEvent{SessionID: sessionID, ExitCode: 0})
	return nil
}

// Attach enters full-screen attach mode for a tmux session and returns a done
// channel that closes when detached/finished. Detach chord: Ctrl+Q.
func (m *Manager) Attach(sessionID string) (<-chan struct{}, error) {
	rs, err := m.getSession(sessionID)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command("tmux", "attach-session", "-t", rs.tmuxSession)
	cmd.Env = os.Environ()
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	done := make(chan struct{})
	go func(tmuxSession string, c *exec.Cmd, f *os.File) {
		defer close(done)
		defer func() { _ = f.Close() }()

		// Raw mode removes cooked-input lag while attached.
		var oldState *term.State
		if term.IsTerminal(os.Stdin.Fd()) {
			if st, e := term.MakeRaw(os.Stdin.Fd()); e == nil {
				oldState = st
			}
		}
		defer func() {
			if oldState != nil {
				_ = term.Restore(os.Stdin.Fd(), oldState)
			}
		}()

		// Initial size sync + SIGWINCH resize forwarding to avoid clipped views.
		_ = pty.InheritSize(os.Stdin, f)
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGWINCH)
		defer signal.Stop(sigCh)
		resizeDone := make(chan struct{})
		stopResize := make(chan struct{})
		go func() {
			defer close(resizeDone)
			for {
				select {
				case <-sigCh:
					_ = pty.InheritSize(os.Stdin, f)
				case <-stopResize:
					return
				}
			}
		}()

		// tmux -> stdout
		copyDone := make(chan struct{}, 1)
		go func() {
			_, _ = io.Copy(os.Stdout, f)
			copyDone <- struct{}{}
		}()

		// stdin -> tmux with Ctrl+Q detach
		inDone := make(chan struct{}, 1)
		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := os.Stdin.Read(buf)
				if n > 0 {
					for i := 0; i < n; i++ {
						if buf[i] == 0x11 { // Ctrl+Q
							_, _ = runTmux("detach-client", "-s", tmuxSession)
							inDone <- struct{}{}
							return
						}
					}
					_, _ = f.Write(buf[:n])
				}
				if err != nil {
					inDone <- struct{}{}
					return
				}
			}
		}()

		waitDone := make(chan struct{}, 1)
		go func() {
			_ = c.Wait()
			waitDone <- struct{}{}
		}()

		select {
		case <-waitDone:
		case <-inDone:
			// give tmux a moment to settle detach path
			select {
			case <-waitDone:
			case <-time.After(300 * time.Millisecond):
			}
		}

		select {
		case <-copyDone:
		case <-time.After(300 * time.Millisecond):
		}

		close(stopResize)
		<-resizeDone
	}(rs.tmuxSession, cmd, ptmx)

	return done, nil
}

func (m *Manager) AttachCommand(sessionID string) (*exec.Cmd, error) {
	rs, err := m.getSession(sessionID)
	if err != nil {
		return nil, err
	}

	// Ensure detached window has current terminal size before attach.
	if w, h, e := term.GetSize(os.Stdout.Fd()); e == nil {
		_, _ = runTmux("resize-window", "-t", rs.tmuxSession, "-x", strconv.Itoa(w), "-y", strconv.Itoa(h))
	}
	// Mirror Claude-Squad detach chord for attach mode.
	_, _ = runTmux("bind-key", "-n", "C-q", "detach-client")

	cmd := exec.Command("tmux", "attach-session", "-t", rs.tmuxSession)
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd, nil
}

func (m *Manager) CaptureFull(sessionID string) ([]string, error) {
	rs, err := m.getSession(sessionID)
	if err != nil {
		return nil, err
	}
	out, err := runTmux("capture-pane", "-p", "-e", "-J", "-t", rs.tmuxSession, "-S", "-", "-E", "-")
	if err != nil {
		return nil, err
	}
	out = strings.ReplaceAll(out, "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// ResizeAll applies a detached pane size hint to all tmux sessions.
// This keeps capture-pane output from being clipped to stale/default sizes.
func (m *Manager) ResizeAll(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	m.mu.Lock()
	names := make([]string, 0, len(m.sessions))
	for _, s := range m.sessions {
		names = append(names, s.tmuxSession)
	}
	m.mu.Unlock()

	w := strconv.Itoa(width)
	h := strconv.Itoa(height)
	for _, name := range names {
		_, err := runTmux("resize-window", "-t", name, "-x", w, "-y", h)
		if err != nil {
			m.emit(ManagerErrorEvent{Err: fmt.Sprintf("resize %s: %v", name, err)})
		}
	}
}

func (m *Manager) Shutdown() {
	select {
	case <-m.stopCh:
		// already closed
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
	ticker := time.NewTicker(350 * time.Millisecond)
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
		name string
		last string
	}
	m.mu.Lock()
	items := make([]item, 0, len(m.sessions))
	for _, s := range m.sessions {
		items = append(items, item{id: s.id, name: s.tmuxSession, last: s.lastSnapshot})
	}
	m.mu.Unlock()

	for _, it := range items {
		has, err := tmuxHasSession(it.name)
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

		lines, snap, err := m.capture(it.name, 300)
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

func (m *Manager) capture(tmuxSession string, historyLines int) ([]string, string, error) {
	start := fmt.Sprintf("-%d", historyLines)
	out, err := runTmux("capture-pane", "-p", "-e", "-J", "-t", tmuxSession, "-S", start)
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

func tmuxHasSession(name string) (bool, error) {
	_, err := runTmux("has-session", "-t", name)
	if err == nil {
		return true, nil
	}
	// tmux uses exit code 1 for missing session; stderr usually includes this text.
	raw := strings.ToLower(err.Error())
	if strings.Contains(raw, "can't find session") || strings.Contains(raw, "exit status 1") {
		return false, nil
	}
	return false, err
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
	for len(data) > 0 {
		// Arrow keys
		if len(data) >= 3 && data[0] == 0x1b && data[1] == '[' {
			switch data[2] {
			case 'A':
				if _, err := runTmux("send-keys", "-t", tmuxSession, "Up"); err != nil {
					return err
				}
				data = data[3:]
				continue
			case 'B':
				if _, err := runTmux("send-keys", "-t", tmuxSession, "Down"); err != nil {
					return err
				}
				data = data[3:]
				continue
			case 'C':
				if _, err := runTmux("send-keys", "-t", tmuxSession, "Right"); err != nil {
					return err
				}
				data = data[3:]
				continue
			case 'D':
				if _, err := runTmux("send-keys", "-t", tmuxSession, "Left"); err != nil {
					return err
				}
				data = data[3:]
				continue
			}
		}

		b := data[0]
		switch {
		case b == '\r' || b == '\n':
			if _, err := runTmux("send-keys", "-t", tmuxSession, "Enter"); err != nil {
				return err
			}
			data = data[1:]
		case b == '\t':
			if _, err := runTmux("send-keys", "-t", tmuxSession, "Tab"); err != nil {
				return err
			}
			data = data[1:]
		case b == 0x7f:
			if _, err := runTmux("send-keys", "-t", tmuxSession, "BSpace"); err != nil {
				return err
			}
			data = data[1:]
		case b >= 1 && b <= 26:
			ctrl := "C-" + string('a'+(b-1))
			if _, err := runTmux("send-keys", "-t", tmuxSession, ctrl); err != nil {
				return err
			}
			data = data[1:]
		default:
			// Accumulate literal runes until next control code.
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
			if _, err := runTmux("send-keys", "-t", tmuxSession, "-l", lit); err != nil {
				return err
			}
			data = data[i:]
		}
	}
	return nil
}

func sanitizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "term"
	}
	repl := strings.NewReplacer(" ", "_", "/", "_", ":", "_", ".", "_")
	return repl.Replace(s)
}
