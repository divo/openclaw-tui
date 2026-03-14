package tasks

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"openclaw-tui/internal/msg"
)

func View(items []msg.TaskItem, offset, height int) string {
	if height < 1 {
		return ""
	}
	if len(items) == 0 {
		return "(empty)"
	}
	if offset > len(items)-1 {
		offset = max(0, len(items)-1)
	}
	end := min(len(items), offset+height)
	out := make([]string, 0, height)

	p1 := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	p2 := lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	p3 := lipgloss.NewStyle().Foreground(lipgloss.Color("45")).Bold(true)

	for _, it := range items[offset:end] {
		badgeText := fmt.Sprintf("P%d", it.Priority)
		switch it.Priority {
		case 1:
			badgeText = p1.Render(badgeText)
		case 2:
			badgeText = p2.Render(badgeText)
		default:
			badgeText = p3.Render(badgeText)
		}
		out = append(out, fmt.Sprintf("☐ %s %s", badgeText, compactLine(it.Text, 95)))
	}

	return strings.Join(out, "\n")
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
