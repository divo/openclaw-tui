package terminal

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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
	// Bash/sh need -i to force interactive mode in a detached PTY.
	// Zsh and fish auto-detect PTY interactivity; -i can cause .zshrc
	// issues (compinit hangs, powerline probes, etc.).
	var args []string
	base := filepath.Base(shell)
	if base == "bash" || base == "sh" {
		args = []string{"-i"}
	}
	return SessionSpec{Name: "shell", Type: SessionTypeShell, Cmd: shell, Args: args}
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
	id          string
	spec        SessionSpec
	tmuxSession *tmuxSession
	lastHash    uint64
	attaching   bool
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
			s.lastHash = hashSnap(snap)
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

// Attach bridges the existing detached PTY directly to stdin/stdout for
// fullscreen interactive use (like Claude Squad). Only one tmux client
// exists at any time, avoiding dual-client resize fights.
// Returns a channel that closes when the user detaches (Ctrl+Q) or the
// session ends. The caller should block on this channel.
func (m *Manager) Attach(sessionID string) (<-chan struct{}, error) {
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

	ptmx := rs.tmuxSession.ptmx
	if ptmx == nil {
		m.mu.Lock()
		rs.attaching = false
		m.mu.Unlock()
		return nil, fmt.Errorf("session %s has no PTY", sessionID)
	}

	// Resize the PTY to the real terminal dimensions before bridging.
	if w, h, e := term.GetSize(os.Stdout.Fd()); e == nil && w > 0 && h > 0 {
		_ = pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
	}

	doneCh := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// Goroutine 1: PTY output → stdout
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(os.Stdout, ptmx)
	}()

	// Goroutine 2: stdin → PTY (with Ctrl+Q detection)
	go func() {
		// Nuke first 50ms of stdin (terminal control sequence noise on attach).
		timeoutCh := make(chan struct{})
		go func() {
			time.Sleep(50 * time.Millisecond)
			close(timeoutCh)
		}()

		buf := make([]byte, 32)
		for {
			nr, err := os.Stdin.Read(buf)
			if err != nil {
				break
			}
			// Discard initial control sequences.
			select {
			case <-timeoutCh:
			default:
				continue
			}
			// Ctrl+Q (ASCII 17) = detach.
			if nr == 1 && buf[0] == 17 {
				m.detachSession(rs, ctx, cancel, &wg, doneCh)
				return
			}
			_, _ = ptmx.Write(buf[:nr])
		}
		// stdin EOF — detach.
		m.detachSession(rs, ctx, cancel, &wg, doneCh)
	}()

	// Goroutine 3+4: SIGWINCH monitor with debouncing.
	wg.Add(2)
	winchChan := make(chan os.Signal, 1)
	signal.Notify(winchChan, syscall.SIGWINCH)
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGWINCH) // trigger initial resize
	debouncedWinch := make(chan os.Signal, 1)
	go func() {
		defer wg.Done()
		var timer *time.Timer
		for {
			select {
			case <-ctx.Done():
				return
			case <-winchChan:
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(50*time.Millisecond, func() {
					select {
					case debouncedWinch <- syscall.SIGWINCH:
					case <-ctx.Done():
					}
				})
			}
		}
	}()
	go func() {
		defer wg.Done()
		defer signal.Stop(winchChan)
		for {
			select {
			case <-ctx.Done():
				return
			case <-debouncedWinch:
				if w, h, e := term.GetSize(os.Stdout.Fd()); e == nil && w > 0 && h > 0 {
					_ = pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
				}
			}
		}
	}()

	return doneCh, nil
}

// detachSession closes the attached PTY, restores a fresh detached PTY,
// cancels attach goroutines, and signals completion.
func (m *Manager) detachSession(rs *runtimeSession, ctx context.Context, cancel context.CancelFunc, wg *sync.WaitGroup, doneCh chan struct{}) {
	// Close the attached PTY — this causes the io.Copy goroutine to return.
	if rs.tmuxSession.ptmx != nil {
		_ = rs.tmuxSession.ptmx.Close()
		rs.tmuxSession.ptmx = nil
		rs.tmuxSession.ptmxCmd = nil
	}

	// Restore a fresh detached PTY for capture mode.
	_ = rs.tmuxSession.Restore()

	// Resize back to TUI pane dimensions.
	m.mu.Lock()
	cols, rows := m.desiredCols, m.desiredRows
	rs.attaching = false
	m.mu.Unlock()
	if cols > 0 && rows > 0 {
		_ = rs.tmuxSession.SetDetachedSize(cols, rows)
	}

	cancel()
	wg.Wait()
	close(doneCh)
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
	if m.desiredCols == width && m.desiredRows == height {
		m.mu.Unlock()
		return
	}
	m.desiredCols, m.desiredRows = width, height
	sessions := make([]*runtimeSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		if !s.attaching {
			sessions = append(sessions, s)
		}
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
	// Safety net: remove any lingering global C-q binding.
	_, _ = runTmux("unbind-key", "-n", "C-q")

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
		id       string
		sess     *tmuxSession
		lastHash uint64
	}
	m.mu.Lock()
	items := make([]item, 0, len(m.sessions))
	for _, s := range m.sessions {
		if s.attaching {
			continue // skip sessions in fullscreen attach
		}
		items = append(items, item{id: s.id, sess: s.tmuxSession, lastHash: s.lastHash})
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
		h := hashSnap(snap)
		if h == it.lastHash {
			continue
		}
		m.mu.Lock()
		if s, ok := m.sessions[it.id]; ok {
			s.lastHash = h
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
	name    string
	ptmx    *os.File
	ptmxCmd *exec.Cmd // the background "tmux attach-session" process backing ptmx
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
		// Verify the existing PTY is still valid (tmux server may have crashed).
		if _, err := t.ptmx.Stat(); err != nil {
			t.killDetached()
		} else {
			return nil
		}
	}
	cmd := exec.Command("tmux", "attach-session", "-t", "="+t.name)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("open detached tmux pty: %w", err)
	}
	t.ptmx = ptmx
	t.ptmxCmd = cmd
	return nil
}

func (t *tmuxSession) ReleaseDetached() error {
	if t.ptmx == nil {
		return nil
	}
	t.killDetached()
	return nil
}

// killDetached kills the background "tmux attach-session" process and closes
// the PTY fd so no stale tmux client remains (which would fight over session
// size with any new attach client).
func (t *tmuxSession) killDetached() {
	if t.ptmxCmd != nil && t.ptmxCmd.Process != nil {
		_ = t.ptmxCmd.Process.Kill()
		_ = t.ptmxCmd.Wait()
	}
	if t.ptmx != nil {
		_ = t.ptmx.Close()
	}
	t.ptmx = nil
	t.ptmxCmd = nil
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

func (t *tmuxSession) paneTarget() string {
	return t.name + ":0.0"
}

func hashSnap(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

func sanitizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "term"
	}
	repl := strings.NewReplacer(" ", "_", "/", "_", ":", "_", ".", "_")
	return repl.Replace(s)
}
