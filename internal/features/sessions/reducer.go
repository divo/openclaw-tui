package sessions

import (
	"strings"

	"openclaw-tui/internal/msg"
)

func Reduce(state State, m msg.RefreshMsg) State {
	state.Items = parseSessionsCompact(m.SessionsRaw, 6)
	if len(state.Items) == 0 {
		state.Items = []string{"No sessions returned"}
	}
	return state
}

func parseSessionsCompact(raw string, limit int) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, r := range strings.Split(raw, "\n") {
		r = strings.TrimSpace(r)
		if strings.HasPrefix(r, "- direct") || strings.HasPrefix(r, "- group") || strings.HasPrefix(r, "- cron") {
			out = append(out, compactLine(r, 90))
			if len(out) >= limit {
				break
			}
		}
	}
	if len(out) == 0 {
		for _, r := range strings.Split(strings.TrimSpace(raw), "\n") {
			r = strings.TrimSpace(r)
			if r == "" || strings.HasPrefix(strings.ToLower(r), "session store") || strings.HasPrefix(strings.ToLower(r), "sessions listed") || strings.HasPrefix(strings.ToLower(r), "kind") {
				continue
			}
			out = append(out, compactLine(r, 90))
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func compactLine(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if maxLen <= 3 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
