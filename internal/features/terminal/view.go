package terminal

import (
	"fmt"
	"strings"
)

func View(state State, height int) string {
	if len(state.Sessions) == 0 {
		body := []string{
			"No terminal sessions.",
			"",
			"Create tmux sessions:",
			"  MOVE: S (or Ctrl+n on Terminal) starts a shell",
			"  EDIT on Terminal: Ctrl+t opens custom command mode (shell|claude|ssh <host>)",
			"",
			"When a session exists:",
			"  MOVE mode + Enter (or a) => in-pane input mode",
			"  Press A for optional fullscreen attach",
			"  Detach fullscreen with Ctrl+Q",
		}
		if state.CommandMode {
			body = append(body, "", "> "+state.PendingCommand)
		}
		return strings.Join(body, "\n")
	}

	header := make([]string, 0, len(state.Sessions))
	for i, sess := range state.Sessions {
		label := fmt.Sprintf("%s:%s(%s)", sess.ID, sess.Name, sess.Status)
		if i == state.Active {
			label = "[" + label + "]"
		}
		header = append(header, label)
	}

	active := state.ActiveSession()
	lines := []string{strings.Join(header, "  ")}
	lines = append(lines, strings.Repeat("─", 20))
	if active != nil {
		content := active.Snapshot
		available := max(1, height-3)
		start := 0
		if len(content) > available {
			start = len(content) - available - active.Scrollback
			if start < 0 {
				start = 0
			}
		}
		end := min(len(content), start+available)
		for i := start; i < end; i++ {
			lines = append(lines, content[i])
		}
		if len(content) == 0 {
			lines = append(lines, "(no output yet)")
		}
	}

	if state.IsScrolling {
		lines = append(lines, "", "-- scroll mode (J/K, Ctrl+d/u, Esc to exit) --")
	}
	if state.CommandMode {
		lines = append(lines, "> "+state.PendingCommand)
	}
	return strings.Join(lines, "\n")
}

func StatusLine(state State) string {
	if state.LastStatusLine == "" {
		return "terminal: idle"
	}
	return "terminal: " + state.LastStatusLine
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
