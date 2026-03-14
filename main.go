package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	refreshInterval = 5 * time.Second
	tasksPath       = "/home/divo/code/obsidian/Amerish/TASKS.md"
)

type refreshResult struct {
	statusRaw    string
	sessionsRaw  string
	subagentsRaw string
	taskItems    []string
	errors       []string
	at           time.Time
}

type refreshMsg refreshResult

type model struct {
	width       int
	height      int
	status      string
	lastRefresh time.Time

	projectItems []string
	sessionItems []string
	subagentItems []string
	connectionItems []string
	errors []string
}

func initialModel() model {
	return model{
		status:      "Booting",
		lastRefresh: time.Now(),
		projectItems: []string{"Loading tasks..."},
		sessionItems: []string{"Loading sessions..."},
		subagentItems: []string{"Loading sub-agents..."},
		connectionItems: []string{"Loading channels..."},
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(refreshCmd(), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg {
		return runRefresh()
	})
}

func refreshCmd() tea.Cmd {
	return func() tea.Msg { return runRefresh() }
}

func runRefresh() tea.Msg {
	result := refreshResult{at: time.Now()}

	statusOut, err := runCommand(4*time.Second, "openclaw", "status", "--all")
	if err != nil {
		result.errors = append(result.errors, "status: "+err.Error())
	}
	result.statusRaw = statusOut

	sessionsOut, err := runCommand(4*time.Second, "openclaw", "sessions", "list")
	if err != nil {
		result.errors = append(result.errors, "sessions: "+err.Error())
	}
	result.sessionsRaw = sessionsOut

	result.subagentsRaw = sessionsOut

	result.taskItems = readTaskItems(tasksPath, 8)
	if len(result.taskItems) == 0 {
		result.taskItems = []string{"No open tasks found"}
	}

	return refreshMsg(result)
}

func runCommand(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			return "", err
		}
		return text, fmt.Errorf("%w | %s", err, firstLine(text))
	}
	return text, nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case refreshMsg:
		m.lastRefresh = msg.at
		m.status = "Live"
		m.errors = msg.errors
		m.projectItems = msg.taskItems
		m.connectionItems = parseConnections(msg.statusRaw)
		m.sessionItems = parseRows(msg.sessionsRaw, 8)
		m.subagentItems = parseSubagentRows(msg.subagentsRaw, 8)

		if len(m.connectionItems) == 0 {
			m.connectionItems = []string{"No channel data"}
		}
		if len(m.sessionItems) == 0 {
			m.sessionItems = []string{"No sessions returned"}
		}
		if len(m.subagentItems) == 0 {
			m.subagentItems = []string{"No sub-agents returned"}
		}

		return m, tickCmd()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.status = "Refreshing..."
			return m, refreshCmd()
		}
	}

	return m, nil
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Padding(0, 1)
	panelStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	header := headerStyle.Render(fmt.Sprintf(
		"OpenClaw Ops TUI | status=%s | refreshed=%s",
		m.status,
		m.lastRefresh.Format("15:04:05"),
	))

	leftWidth := max(30, int(float64(m.width)*0.56))
	rightWidth := max(20, m.width-leftWidth-1)
	bodyHeight := max(10, m.height-9)

	left := panelStyle.Width(leftWidth - 4).Height(bodyHeight).Render(renderLeftPane(m.projectItems, m.sessionItems, m.subagentItems))
	right := panelStyle.Width(rightWidth - 4).Height(bodyHeight).Render(renderRightPane(m.connectionItems, m.errors))
	row := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	footer := muted.Render("Keys: r=refresh  q=quit")

	var parts []string
	parts = append(parts, header, row)
	if len(m.errors) > 0 {
		parts = append(parts, errorStyle.Render("Errors: "+strings.Join(m.errors, " | ")))
	}
	parts = append(parts, footer)

	return strings.Join(parts, "\n\n")
}

func renderLeftPane(tasks, sessions, subagents []string) string {
	lines := []string{"Project / Sessions", "", "Top open tasks:"}
	for _, t := range tasks {
		lines = append(lines, "- "+t)
	}

	lines = append(lines, "", "Sessions:")
	for _, s := range sessions {
		lines = append(lines, "- "+s)
	}

	lines = append(lines, "", "Sub-agents:")
	for _, s := range subagents {
		lines = append(lines, "- "+s)
	}

	return strings.Join(lines, "\n")
}

func renderRightPane(connections, errs []string) string {
	good := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	bad := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("220"))

	lines := []string{"Connections", ""}
	for _, c := range connections {
		lc := strings.ToLower(c)
		switch {
		case strings.Contains(lc, "connected"):
			lines = append(lines, good.Render("- "+c))
		case strings.Contains(lc, "disconnected") || strings.Contains(lc, "error"):
			lines = append(lines, bad.Render("- "+c))
		default:
			lines = append(lines, warn.Render("- "+c))
		}
	}

	if len(errs) > 0 {
		lines = append(lines, "", "Command errors:")
		for _, e := range errs {
			lines = append(lines, bad.Render("- "+e))
		}
	}

	return strings.Join(lines, "\n")
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
				if _, ok := seen[trimmed]; !ok {
					out = append(out, trimmed)
					seen[trimmed] = struct{}{}
				}
				break
			}
		}
	}

	sort.Strings(out)
	return out
}

func parseRows(raw string, limit int) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	rows := strings.Split(raw, "\n")
	out := make([]string, 0, limit)
	for _, r := range rows {
		r = strings.TrimSpace(r)
		if r == "" || strings.HasPrefix(r, "#") || strings.HasPrefix(r, "---") {
			continue
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func parseSubagentRows(raw string, limit int) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, r := range strings.Split(raw, "\n") {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		lower := strings.ToLower(r)
		if strings.Contains(lower, "sub-agent") || strings.Contains(lower, "subagent") {
			out = append(out, r)
			if len(out) >= limit {
				break
			}
		}
	}
	if len(out) == 0 {
		return []string{"No sub-agents found"}
	}
	return out
}

func readTaskItems(path string, limit int) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return []string{"Unable to read TASKS.md"}
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [ ]") {
			out = append(out, trimmed)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}
