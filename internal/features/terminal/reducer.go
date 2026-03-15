package terminal

import (
	"fmt"
	"time"
)

func Reduce(state State, incoming any) State {
	s := state
	switch m := incoming.(type) {
	case EventMsg:
		s = reduceEvent(s, m.Event)
	case StartSessionResultMsg:
		if m.Err != nil {
			s.SetStatus(fmt.Sprintf("failed to start %s: %v", m.Spec.Name, m.Err), true)
		} else {
			s.SetStatus("starting "+m.Spec.Name+"...", false)
		}
	}
	return s
}

func reduceEvent(state State, evt Event) State {
	s := state
	switch e := evt.(type) {
	case SessionEvent:
		s.Upsert(e.Meta)
		s.SetStatus(fmt.Sprintf("%s [%s] %s", e.Meta.Name, e.Meta.ID, e.Meta.Status), e.Meta.Status == SessionStatusError)
		if e.Meta.Status == SessionStatusRunning {
			for i := range s.Sessions {
				if s.Sessions[i].ID == e.Meta.ID {
					s.Active = i
					break
				}
			}
		}
	case OutputEvent:
		s.AppendOutput(e.SessionID, e.Chunk)
	case ExitEvent:
		for i := range s.Sessions {
			if s.Sessions[i].ID == e.SessionID {
				s.Sessions[i].Buffer.Flush() // commit any partial line
				s.Sessions[i].Status = SessionStatusExited
				s.Sessions[i].ExitCode = e.ExitCode
				s.Sessions[i].LastError = e.Err
				s.Sessions[i].UpdatedAt = time.Now()
				s.SetStatus(fmt.Sprintf("%s [%s] exited (%d)", s.Sessions[i].Name, e.SessionID, e.ExitCode), e.ExitCode != 0)
				break
			}
		}
	case ManagerErrorEvent:
		s.SetStatus(e.Err, true)
	}
	return s
}
