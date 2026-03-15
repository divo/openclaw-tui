package chat

import (
	"strings"
	"testing"

	"openclaw-tui/internal/ui"
)

// --- History navigation ---

func TestHistoryPrev_cyclesBackThroughHistory(t *testing.T) {
	s := InitialState()
	s = StartSend(s, "first")
	s = StartSend(s, "second")
	s = StartSend(s, "third")
	// HistoryIndex starts at -1 after sends
	s = HistoryPrev(s)
	if s.Input != "third" {
		t.Fatalf("expected 'third', got %q", s.Input)
	}
	s = HistoryPrev(s)
	if s.Input != "second" {
		t.Fatalf("expected 'second', got %q", s.Input)
	}
	s = HistoryPrev(s)
	if s.Input != "first" {
		t.Fatalf("expected 'first', got %q", s.Input)
	}
	// already at oldest — stays at first
	s = HistoryPrev(s)
	if s.Input != "first" {
		t.Fatalf("expected to stay at 'first', got %q", s.Input)
	}
}

func TestHistoryNext_forwardsAndClearsAtEnd(t *testing.T) {
	s := InitialState()
	s = StartSend(s, "one")
	s = StartSend(s, "two")
	s = HistoryPrev(s) // → "two"
	s = HistoryPrev(s) // → "one"
	s = HistoryNext(s) // → "two"
	if s.Input != "two" {
		t.Fatalf("expected 'two', got %q", s.Input)
	}
	s = HistoryNext(s) // past end → clears input
	if s.Input != "" {
		t.Fatalf("expected empty input after end of history, got %q", s.Input)
	}
	if s.HistoryIndex != -1 {
		t.Fatalf("expected HistoryIndex=-1 after clearing, got %d", s.HistoryIndex)
	}
}

func TestHistoryNext_noopWhenNotNavigating(t *testing.T) {
	s := InitialState()
	s = StartSend(s, "one")
	s.Input = "typing..."
	before := s
	s = HistoryNext(s)
	if s.Input != before.Input {
		t.Fatalf("HistoryNext should noop when not navigating, got %q", s.Input)
	}
}

// --- Follow-tail view rendering ---

func TestView_followTailShowsLastLines(t *testing.T) {
	s := InitialState()
	s.Lines = []string{"a", "b", "c", "d", "e"}
	s.FollowTail = true
	s.Offset = 0 // offset is ignored when FollowTail=true

	// height=4: available=3 body lines + 1 input row
	out := View(s, ui.ModeMove, 4)
	lines := strings.Split(out, "\n")
	// should show the last 3 lines: c, d, e
	if lines[0] != "c" || lines[1] != "d" || lines[2] != "e" {
		t.Fatalf("expected last 3 lines, got: %v", lines)
	}
}

func TestView_noFollowTailRespectsOffset(t *testing.T) {
	s := InitialState()
	s.Lines = []string{"a", "b", "c", "d", "e"}
	s.FollowTail = false
	s.Offset = 1

	out := View(s, ui.ModeMove, 4)
	lines := strings.Split(out, "\n")
	if lines[0] != "b" {
		t.Fatalf("expected offset=1 to show 'b' first, got %q", lines[0])
	}
}

func TestScroll_clearFollowTail(t *testing.T) {
	s := InitialState()
	s.Lines = []string{"a", "b", "c"}
	s.FollowTail = true
	s = Scroll(s, -1)
	if s.FollowTail {
		t.Fatal("manual scroll should clear FollowTail")
	}
}

func TestFollowLatest_setsFollowTailAndResetsOffset(t *testing.T) {
	s := InitialState()
	s.Offset = 42
	s.FollowTail = false
	s = FollowLatest(s)
	if !s.FollowTail {
		t.Fatal("FollowLatest should set FollowTail=true")
	}
	if s.Offset != 0 {
		t.Fatalf("FollowLatest should reset Offset to 0, got %d", s.Offset)
	}
}
