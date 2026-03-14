package tasks

import (
	"os"
	"sort"
	"strings"

	"openclaw-tui/internal/msg"
)

func Reduce(state State, m msg.RefreshMsg) State {
	state.Items = m.TaskItems
	if len(state.Items) == 0 {
		state.Items = []msg.TaskItem{{Priority: 3, Text: "No open tasks found"}}
	}
	return state
}

func ReadTaskItems(path string, limit int) []msg.TaskItem {
	b, err := os.ReadFile(path)
	if err != nil {
		return []msg.TaskItem{{Priority: 1, Text: "Unable to read TASKS.md"}}
	}

	var items []msg.TaskItem
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- [ ]") {
			continue
		}
		items = append(items, parseTaskLine(trimmed))
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Priority == items[j].Priority {
			return items[i].Text < items[j].Text
		}
		return items[i].Priority < items[j].Priority
	})

	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func parseTaskLine(line string) msg.TaskItem {
	item := msg.TaskItem{Priority: 3, Text: compactLine(line, 100)}

	rest := strings.TrimSpace(strings.TrimPrefix(line, "- [ ]"))
	if strings.HasPrefix(rest, "[P") {
		closeIdx := strings.Index(rest, "]")
		if closeIdx > 2 {
			p := rest[2:closeIdx]
			if p == "1" || p == "2" || p == "3" {
				item.Priority = int(p[0] - '0')
			}
			rest = strings.TrimSpace(rest[closeIdx+1:])
		}
	}

	if i := strings.Index(rest, " -- "); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.Index(rest, " | "); i >= 0 {
		rest = rest[:i]
	}

	if strings.TrimSpace(rest) != "" {
		item.Text = strings.TrimSpace(rest)
	}
	return item
}

func compactLine(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if maxLen <= 3 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
