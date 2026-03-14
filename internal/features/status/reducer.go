package status

import (
	"sort"
	"strings"

	"openclaw-tui/internal/msg"
)

func Reduce(state State, m msg.RefreshMsg) State {
	state.ConnectionItems = parseConnections(m.StatusRaw)
	if len(state.ConnectionItems) == 0 {
		state.ConnectionItems = []string{"No channel data"}
	}
	return state
}

func parseConnections(statusRaw string) []string {
	if strings.TrimSpace(statusRaw) == "" {
		return nil
	}
	keys := []string{"whatsapp", "telegram", "discord", "slack", "signal", "webchat", "imessage", "googlechat"}
	var out []string
	seen := map[string]struct{}{}
	for _, line := range strings.Split(statusRaw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		for _, key := range keys {
			if strings.Contains(lower, key) {
				clean := compactLine(trimmed, 80)
				if _, ok := seen[clean]; !ok {
					out = append(out, clean)
					seen[clean] = struct{}{}
				}
				break
			}
		}
	}
	sort.Strings(out)
	if len(out) > 6 {
		out = out[:6]
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
