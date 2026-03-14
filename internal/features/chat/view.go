package chat

import (
	"fmt"
	"strings"
	"time"

	"openclaw-tui/internal/ui"
)

func View(lines []string, offset int, input string, sending bool, mode ui.Mode, height int) string {
	if height < 2 {
		return "> " + input
	}
	available := height - 1
	if available < 1 {
		available = 1
	}
	if len(lines) == 0 {
		lines = []string{"(no messages yet)"}
	}
	if offset > len(lines)-1 {
		offset = max(0, len(lines)-1)
	}
	end := min(len(lines), offset+available)
	visible := lines[offset:end]
	for i := range visible {
		visible[i] = compactLine(visible[i], 140)
	}
	prefix := "> "
	if mode == ui.ModeEdit {
		prefix = "I> "
	}
	if sending {
		prefix = "[sending] "
	}
	return strings.Join(append(visible, prefix+input), "\n")
}

func RunStatusLine(sending bool, startedAt time.Time, spinnerIndex int, mode ui.Mode, connState, sessionKey string, lastRefresh time.Time, errs []string) string {
	modeLabel := "MOVE"
	if mode == ui.ModeEdit {
		modeLabel = "EDIT"
	}
	runLabel := "idle"
	if sending {
		elapsed := 0
		if !startedAt.IsZero() {
			elapsed = int(time.Since(startedAt).Seconds())
			if elapsed < 0 {
				elapsed = 0
			}
		}
		spin := SpinnerFrames[spinnerIndex%len(SpinnerFrames)]
		runLabel = fmt.Sprintf("%s running • %ds", spin, elapsed)
	}
	if strings.TrimSpace(connState) == "" {
		connState = "disconnected"
	}
	if sessionKey == "" {
		sessionKey = "—"
	}
	errLabel := "none"
	if len(errs) > 0 {
		errLabel = compactLine(firstLine(errs[len(errs)-1]), 34)
	}
	line := fmt.Sprintf("%s | %s | mode:%s | sess:%s | ref:%s | err:%s", runLabel, connState, modeLabel, compactLine(sessionKey, 20), lastRefresh.Format("15:04:05"), errLabel)
	return compactLine(line, 160)
}

func firstLine(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(s), "\n")
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
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
