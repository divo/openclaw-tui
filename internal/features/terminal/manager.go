package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

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

type OutputEvent struct {
	SessionID string
	Chunk     string
}

type ExitEvent struct {
	SessionID string
	ExitCode  int
	Err       string
}

type ManagerErrorEvent struct{ Err string }

func (SessionEvent) isTerminalEvent()      {}
func (OutputEvent) isTerminalEvent()       {}
func (ExitEvent) isTerminalEvent()         {}
func (ManagerErrorEvent) isTerminalEvent() {}

type runtimeSession struct {
	id   string
	spec SessionSpec
	cmd  *exec.Cmd
	pty  *os.File
}

type Manager struct {
	mu       sync.Mutex
	sessions map[string]*runtimeSession
	nextID   int
	events   chan Event
}

func NewManager() *Manager {
	return &Manager{
		sessions: map[string]*runtimeSession{},
		events:   make(chan Event, 256),
	}
}

func (m *Manager) Events() <-chan Event { return m.events }

func (m *Manager) Start(spec SessionSpec) error {
	if spec.Cmd == "" {
		return fmt.Errorf("empty command")
	}

	m.mu.Lock()
	m.nextID++
	id := fmt.Sprintf("t%03d", m.nextID)
	meta := SessionMeta{ID: id, Name: spec.Name, Type: spec.Type, Status: SessionStatusStarting}
	m.mu.Unlock()
	m.emit(SessionEvent{Meta: meta})

	cmd := exec.Command(spec.Cmd, spec.Args...)
	cmd.Env = append(os.Environ(), spec.Env...)

	f, err := pty.Start(cmd)
	if err != nil {
		m.emit(ManagerErrorEvent{Err: fmt.Sprintf("start %s: %v", spec.Name, err)})
		m.emit(SessionEvent{Meta: SessionMeta{ID: id, Name: spec.Name, Type: spec.Type, Status: SessionStatusError, Err: err.Error()}})
		return err
	}

	r := &runtimeSession{id: id, spec: spec, cmd: cmd, pty: f}

	m.mu.Lock()
	m.sessions[id] = r
	m.mu.Unlock()

	m.emit(SessionEvent{Meta: SessionMeta{ID: id, Name: spec.Name, Type: spec.Type, Status: SessionStatusRunning}})
	go m.readLoop(r)
	go m.waitLoop(r)
	return nil
}

func (m *Manager) Write(sessionID string, data []byte) error {
	m.mu.Lock()
	rs, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	_, err := rs.pty.Write(data)
	return err
}

func (m *Manager) Kill(sessionID string) error {
	m.mu.Lock()
	rs, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	if rs.cmd.Process == nil {
		return nil
	}
	if err := rs.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}

	go func() {
		timer := time.NewTimer(800 * time.Millisecond)
		defer timer.Stop()
		<-timer.C
		_ = rs.cmd.Process.Kill()
	}()
	return nil
}

func (m *Manager) Shutdown() {
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

func (m *Manager) readLoop(rs *runtimeSession) {
	buf := make([]byte, 4096)
	for {
		n, err := rs.pty.Read(buf)
		if n > 0 {
			m.emit(OutputEvent{SessionID: rs.id, Chunk: string(buf[:n])})
		}
		if err != nil {
			return
		}
	}
}

func (m *Manager) waitLoop(rs *runtimeSession) {
	err := rs.cmd.Wait()
	exitCode := 0
	errStr := ""
	if err != nil {
		errStr = err.Error()
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
	}
	_ = rs.pty.Close()

	m.mu.Lock()
	delete(m.sessions, rs.id)
	m.mu.Unlock()

	m.emit(ExitEvent{SessionID: rs.id, ExitCode: exitCode, Err: errStr})
}

func (m *Manager) emit(evt Event) {
	select {
	case m.events <- evt:
	default:
		// avoid blocking UI; drop and report once
		select {
		case m.events <- ManagerErrorEvent{Err: "terminal event queue full"}:
		default:
		}
	}
}
