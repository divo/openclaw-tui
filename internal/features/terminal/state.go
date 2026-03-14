package terminal

import (
	"fmt"
	"strings"
	"time"
)

const (
	SessionTypeShell  = "shell"
	SessionTypeSSH    = "ssh"
	SessionTypeClaude = "claude"

	SessionStatusStarting = "starting"
	SessionStatusRunning  = "running"
	SessionStatusExited   = "exited"
	SessionStatusError    = "error"
)

type Session struct {
	ID         string
	Name       string
	Type       string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ExitCode   int
	LastError  string
	Buffer     RingBuffer
	Scrollback int
}

type State struct {
	Sessions        []Session
	Active          int
	MaxBufferLines  int
	MaxBufferBytes  int
	PendingCommand  string
	CommandMode     bool
	LastStatusLine  string
	LastStatusIsErr bool
	LastErrorAt     time.Time
}

func InitialState() State {
	return State{
		Sessions:       nil,
		Active:         -1,
		MaxBufferLines: 600,
		MaxBufferBytes: 128 * 1024,
		LastStatusLine: "No terminal sessions. Press i then Ctrl+n to create one.",
	}
}

func (s *State) ActiveSession() *Session {
	if s.Active < 0 || s.Active >= len(s.Sessions) {
		return nil
	}
	return &s.Sessions[s.Active]
}

func (s *State) Upsert(meta SessionMeta) {
	for i := range s.Sessions {
		if s.Sessions[i].ID == meta.ID {
			s.Sessions[i].Name = meta.Name
			s.Sessions[i].Type = meta.Type
			s.Sessions[i].Status = meta.Status
			s.Sessions[i].UpdatedAt = time.Now()
			if meta.Err != "" {
				s.Sessions[i].LastError = meta.Err
			}
			if meta.ExitCode != 0 {
				s.Sessions[i].ExitCode = meta.ExitCode
			}
			return
		}
	}

	s.Sessions = append(s.Sessions, Session{
		ID:        meta.ID,
		Name:      meta.Name,
		Type:      meta.Type,
		Status:    meta.Status,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Buffer: RingBuffer{
			MaxLines: s.MaxBufferLines,
			MaxBytes: s.MaxBufferBytes,
		},
	})
	if s.Active == -1 {
		s.Active = 0
	}
}

func (s *State) Remove(id string) {
	idx := -1
	for i := range s.Sessions {
		if s.Sessions[i].ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return
	}
	s.Sessions = append(s.Sessions[:idx], s.Sessions[idx+1:]...)
	if len(s.Sessions) == 0 {
		s.Active = -1
		return
	}
	if s.Active >= len(s.Sessions) {
		s.Active = len(s.Sessions) - 1
	}
}

func (s *State) NextSession() {
	if len(s.Sessions) == 0 {
		return
	}
	s.Active = (s.Active + 1) % len(s.Sessions)
}

func (s *State) PrevSession() {
	if len(s.Sessions) == 0 {
		return
	}
	s.Active--
	if s.Active < 0 {
		s.Active = len(s.Sessions) - 1
	}
}

func (s *State) AppendOutput(sessionID string, chunk string) {
	for i := range s.Sessions {
		if s.Sessions[i].ID == sessionID {
			s.Sessions[i].Buffer.Append(chunk)
			s.Sessions[i].UpdatedAt = time.Now()
			return
		}
	}
}

func (s *State) SetStatus(line string, isErr bool) {
	s.LastStatusLine = line
	s.LastStatusIsErr = isErr
	if isErr {
		s.LastErrorAt = time.Now()
	}
}

func ParseCreateCommand(input string) (SessionSpec, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return SessionSpec{}, fmt.Errorf("empty command")
	}
	parts := strings.Fields(trimmed)
	switch parts[0] {
	case "shell":
		return ShellSpec(), nil
	case "claude":
		return ClaudeSpec(), nil
	case "ssh":
		if len(parts) < 2 {
			return SessionSpec{}, fmt.Errorf("usage: ssh <host>")
		}
		host := strings.Join(parts[1:], " ")
		return SSHSpec(host), nil
	default:
		return SessionSpec{}, fmt.Errorf("unknown command %q", parts[0])
	}
}
