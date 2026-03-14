package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	refreshInterval = 5 * time.Second
	tasksPath       = "/home/divo/code/obsidian/Amerish/TASKS.md"
)

type refreshResult struct {
	statusRaw   string
	sessionsRaw string
	taskItems   []string
	errors      []string
	at          time.Time
}

type refreshMsg refreshResult

type chatReplyMsg struct {
	prompt string
	reply  string
	err    error
}

type model struct {
	width       int
	height      int
	status      string
	lastRefresh time.Time

	projectItems    []string
	sessionItems    []string
	connectionItems []string
	errors          []string

	chatLines   []string
	chatInput   string
	chatSending bool
}

func initialModel() model {
	return model{
		status:          "Booting",
		lastRefresh:     time.Now(),
		projectItems:    []string{"Loading tasks..."},
		sessionItems:    []string{"Loading sessions..."},
		connectionItems: []string{"Loading channels..."},
		chatLines: []string{
			"Amerish: Ready. Type and press Enter to chat.",
			"Tip: r refreshes status panes.",
		},
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

	statusOut, err := runOpenClaw(5*time.Second, "status", "--all")
	if err != nil {
		result.errors = append(result.errors, "status: "+err.Error())
	}
	result.statusRaw = statusOut

	sessionsOut, err := runOpenClaw(5*time.Second, "sessions", "list")
	if err != nil {
		result.errors = append(result.errors, "sessions: "+err.Error())
	}
	result.sessionsRaw = sessionsOut

	result.taskItems = readTaskItems(tasksPath, 8)
	if len(result.taskItems) == 0 {
		result.taskItems = []string{"No open tasks found"}
	}

	return refreshMsg(result)
}

func sendChatCmd(prompt string) tea.Cmd {
	return func() tea.Msg {
		reply, err := runOpenClaw(45*time.Second, "agent", "--session-id", "main", "--message", prompt)
		if err != nil {
			return chatReplyMsg{prompt: prompt, err: err}
		}
		return chatReplyMsg{prompt: prompt, reply: strings.TrimSpace(reply)}
	}
}

func openclawBinary() string {
	candidates := []string{
		"openclaw",
		"/home/divo/.npm-global/bin/openclaw",
	}
	for _, c := range candidates {
		if strings.Contains(c, "/") {
			if _, err := os.Stat(c); err == nil {
				return c
			}
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return "openclaw"
}

func runOpenClaw(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	bin := openclawBinary()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(),
		"PATH=/home/divo/.npm-global/bin:/run/current-system/sw/bin:/usr/bin:/bin:"+os.Getenv("PATH"),
	)

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
		m.sessionItems = parseSessionsCompact(msg.sessionsRaw, 4)

		if len(m.connectionItems) == 0 {
			m.connectionItems = []string{"No channel data"}
		}
		if len(m.sessionItems) == 0 {
			m.sessionItems = []string{"No sessions returned"}
		}

		return m, tickCmd()

	case chatReplyMsg:
		m.chatSending = false
		if msg.err != nil {
			m.chatLines = append(m.chatLines, "Amerish [error]: "+msg.err.Error())
			return m, nil
		}
		reply := strings.TrimSpace(msg.reply)
		if reply == "" {
			reply = "(no reply text)"
		}
		m.chatLines = append(m.chatLines, "Amerish: "+compactLine(reply, 220))
		if len(m.chatLines) > 40 {
			m.chatLines = m.chatLines[len(m.chatLines)-40:]
		}
		return m, nil

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
		case "enter":
			if m.chatSending {
				return m, nil
			}
			prompt := strings.TrimSpace(m.chatInput)
			if prompt == "" {
				return m, nil
			}
			m.chatLines = append(m.chatLines, "You: "+prompt)
			m.chatInput = ""
			m.chatSending = true
			if len(m.chatLines) > 40 {
				m.chatLines = m.chatLines[len(m.chatLines)-40:]
			}
			return m, sendChatCmd(prompt)
		case "backspace", "ctrl+h":
			m.chatInput = trimLastRune(m.chatInput)
			return m, nil
		}

		if len(msg.Runes) > 0 {
			m.chatInput += string(msg.Runes)
		}
		return m, nil
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
		"OpenClaw Ops TUI | status=%s | refreshed=%s | chat=%s",
		m.status,
		m.lastRefresh.Format("15:04:05"),
		map[bool]string{true: "sending", false: "idle"}[m.chatSending],
	))

	bodyHeight := max(10, m.height-9)
	tasksWidth := max(30, int(float64(m.width)*0.36))
	chatWidth := max(35, int(float64(m.width)*0.44))
	sideWidth := max(20, m.width-tasksWidth-chatWidth-2)

	tasksPane := panelStyle.Width(tasksWidth - 4).Height(bodyHeight).Render(renderTasksPane(m.projectItems))
	chatPane := panelStyle.Width(chatWidth - 4).Height(bodyHeight).Render(renderChatPane(m.chatLines, m.chatInput, m.chatSending, bodyHeight))
	sidePane := panelStyle.Width(sideWidth - 4).Height(bodyHeight).Render(renderSidePane(m.connectionItems, m.sessionItems))

	row := lipgloss.JoinHorizontal(lipgloss.Top, tasksPane, chatPane, sidePane)
	footer := muted.Render("Keys: type + Enter to chat | r refresh | q quit")

	parts := []string{header, row}
	if len(m.errors) > 0 {
		parts = append(parts, errorStyle.Render("Errors: "+strings.Join(m.errors, " | ")))
	}
	parts = append(parts, footer)
	return strings.Join(parts, "\n\n")
}

func renderTasksPane(tasks []string) string {
	lines := []string{"Tasks", "", "Top open:"}
	for _, t := range tasks {
		lines = append(lines, "- "+compactLine(t, 90))
	}
	return strings.Join(lines, "\n")
}

func renderChatPane(chatLines []string, input string, sending bool, height int) string {
	maxLines := max(6, height-6)
	start := 0
	if len(chatLines) > maxLines {
		start = len(chatLines) - maxLines
	}
	visible := chatLines[start:]

	lines := []string{"Chat", ""}
	for _, l := range visible {
		lines = append(lines, compactLine(l, 110))
	}
	lines = append(lines, "")
	if sending {
		lines = append(lines, "[sending...] "+input)
	} else {
		lines = append(lines, "> "+input)
	}
	return strings.Join(lines, "\n")
}

func renderSidePane(connections, sessions []string) string {
	good := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	bad := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("220"))

	lines := []string{"Status", "", "Connections:"}
	for _, c := range connections {
		lc := strings.ToLower(c)
		switch {
		case strings.Contains(lc, "connected"):
			lines = append(lines, good.Render("- "+compactLine(c, 60)))
		case strings.Contains(lc, "disconnected") || strings.Contains(lc, "error"):
			lines = append(lines, bad.Render("- "+compactLine(c, 60)))
		default:
			lines = append(lines, warn.Render("- "+compactLine(c, 60)))
		}
	}
	lines = append(lines, "", "Sessions:")
	for _, s := range sessions {
		lines = append(lines, "- "+compactLine(s, 60))
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
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func parseSessionsCompact(raw string, limit int) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, r := range strings.Split(raw, "\n") {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if strings.HasPrefix(r, "- direct") || strings.HasPrefix(r, "- group") || strings.HasPrefix(r, "- cron") {
			out = append(out, r)
			if len(out) >= limit {
				break
			}
		}
	}
	if len(out) == 0 {
		rows := strings.Split(strings.TrimSpace(raw), "\n")
		for _, r := range rows {
			r = strings.TrimSpace(r)
			if r == "" || strings.HasPrefix(r, "Session store") || strings.HasPrefix(r, "Sessions listed") || strings.HasPrefix(r, "Kind") {
				continue
			}
			out = append(out, r)
			if len(out) >= limit {
				break
			}
		}
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

func trimLastRune(s string) string {
	if s == "" {
		return s
	}
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}

func compactLine(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if maxLen <= 3 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
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
