package status

import "github.com/charmbracelet/lipgloss"

func ViewList(items []string, offset, height int) string {
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
	for _, it := range items[offset:end] {
		out = append(out, "- "+compactLine(it, 100))
	}
	return joinLines(out)
}

func ConnStateLabel(state string) string {
	switch state {
	case "connected":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Render("● connected")
	case "connecting":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("◌ connecting")
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗ disconnected")
	}
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	out := lines[0]
	for i := 1; i < len(lines); i++ {
		out += "\n" + lines[i]
	}
	return out
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
