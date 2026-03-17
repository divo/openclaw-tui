package terminal

import (
	"fmt"
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
	case AttachResultMsg:
		if m.Err != nil {
			s.SetStatus(fmt.Sprintf("attach failed [%s]: %v", m.SessionID, m.Err), true)
		} else {
			s.SetStatus(fmt.Sprintf("detached [%s]", m.SessionID), false)
		}
	case CaptureFullResultMsg:
		if m.Err != nil {
			s.SetStatus(fmt.Sprintf("scroll capture failed [%s]: %v", m.SessionID, m.Err), true)
		} else {
			s.EnterScrollMode(m.Lines)
			s.SetStatus("scroll mode: J/K move, Esc exits", false)
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
	case CaptureEvent:
		if !(s.IsScrolling && s.ActiveSessionID() == e.SessionID) {
			s.SetSnapshot(e.SessionID, e.Lines)
		}
	case ExitEvent:
		for i := range s.Sessions {
			if s.Sessions[i].ID == e.SessionID {
				s.SetStatus(fmt.Sprintf("%s [%s] exited (%d)", s.Sessions[i].Name, e.SessionID, e.ExitCode), e.ExitCode != 0)
				break
			}
		}
		s.Remove(e.SessionID)
	case ManagerErrorEvent:
		s.SetStatus(e.Err, true)
	}
	return s
}
