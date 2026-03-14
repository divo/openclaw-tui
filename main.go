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

type pane int

const (
	paneStatus pane = iota
	paneSessions
	paneTasks
	paneChat
)

type mode int

const (
	modeMove mode = iota
	modeEdit
)

// connState tracks whether we have an active session to send to.
type connState int

const (
	connConnecting connState = iota
	connConnected
	connDisconnected
)

func (c connState) String() string {
	switch c {
	case connConnecting:
		return "connecting"
	case connConnected:
		return "connected"
	default:
		return "disconnected"
	}
}

type taskItem struct {
	priority int
	text     string
}

type refreshResult struct {
	statusRaw   string
	sessionsRaw string
	taskItems   []taskItem
	errors      []string
	at          time.Time
}

type refreshMsg refreshResult

type sessionDiscoverMsg struct {
	sessionKey string
	err        error
}

type chatReplyMsg struct {
	reply      string
	err        error
	retryCount int
	prompt     string // original prompt, so we can retry
}

type retryConnectMsg struct {
	prompt     string
	retryCount int
}

type model struct {
	width       int
	height      int
	status      string
	lastRefresh time.Time

	// session / connection
	sessionKey string
	conn       connState

	projectItems    []taskItem
	sessionItems    []string
	connectionItems []string
	errors          []string

	chatLines   []string
	chatInput   string
	chatSending bool
	pendingMsg  string // queued while connecting

	focus pane
	mode  mode

	statusOffset   int
	sessionsOffset int
	tasksOffset    int
	chatOffset     int
}

func initialModel() model {
	return model{
		status:          "Booting",
		lastRefresh:     time.Now(),
		conn:            connConnecting,
		projectItems:    []taskItem{{priority: 2, text: "Loading tasks..."}},
		sessionItems:    []string{"Loading sessions..."},
		connectionItems: []string{"Loading channels..."},
		chatLines: []string{
			"Amerish: Ready.",
			"MOVE mode: h/j/k/l changes pane focus.",
			"EDIT mode: focus Chat + i, type, Enter sends, Esc returns to MOVE.",
			"Scroll: J/K line, Ctrl+d/Ctrl+u page.",
		},
		focus: paneChat,
		mode:  modeMove,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(discoverSessionCmd(), refreshCmd(), tickCmd())
}

// ── Session Discovery ────────────────────────────────────────────────────────

func discoverSessionCmd() tea.Cmd {
	return func() tea.Msg {
		out, err := runOpenClaw(10*time.Second, "sessions", "list")
		if err != nil {
			return sessionDiscoverMsg{err: fmt.Errorf("sessions list: %w", err)}
		}
		key := parseMainSessionKey(out)
		if key == "" {
			return sessionDiscoverMsg{err: fmt.Errorf("no main session found")}
		}
		return sessionDiscoverMsg{sessionKey: key}
	}
}

// parseMainSessionKey finds the primary (non-cron) direct session key.
// Expected line format:
//
//	direct agent:main:main   just now  ...
func parseMainSessionKey(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] != "direct" {
			continue
		}
		key := fields[1]
		// Skip cron sub-sessions; prefer the bare main session.
		if strings.Contains(key, ":cron:") {
			continue
		}
		return key
	}
	return ""
}

// scheduleReconnect queues a session re-discover after a short delay.
func scheduleReconnect(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return discoverSessionCmd()()
	})
}

// ── Tick / Refresh ───────────────────────────────────────────────────────────

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return runRefresh() })
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

	result.taskItems = readTaskItems(tasksPath, 12)
	if len(result.taskItems) == 0 {
		result.taskItems = []taskItem{{priority: 3, text: "No open tasks found"}}
	}

	return refreshMsg(result)
}

// ── Chat Send ────────────────────────────────────────────────────────────────

func sendChatCmd(sessionKey, prompt string, retryCount int) tea.Cmd {
	return func() tea.Msg {
		reply, err := runOpenClaw(45*time.Second, "agent", "--session-id", sessionKey, "--message", prompt)
		if err != nil {
			return chatReplyMsg{err: err, retryCount: retryCount, prompt: prompt}
		}
		return chatReplyMsg{reply: strings.TrimSpace(reply)}
	}
}

// ── OpenClaw binary ──────────────────────────────────────────────────────────

func openclawBinary() string {
	candidates := []string{"openclaw", "/home/divo/.npm-global/bin/openclaw"}
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

	cmd := exec.CommandContext(ctx, openclawBinary(), args...)
	cmd.Env = append(os.Environ(), "PATH=/home/divo/.npm-global/bin:/run/current-system/sw/bin:/usr/bin:/bin:"+os.Getenv("PATH"))

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

// ── Update ───────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	// ── Session discovery result ─────────────────────────────────────────────
	case sessionDiscoverMsg:
		if msg.err != nil {
			m.conn = connDisconnected
			m.errors = append(m.errors, "session: "+msg.err.Error())
			// Retry discovery in 3s.
			return m, scheduleReconnect(3 * time.Second)
		}
		if m.sessionKey != msg.sessionKey {
			// New or changed session — announce it.
			m.chatLines = append(m.chatLines, fmt.Sprintf("⚡ connected → %s", msg.sessionKey))
		}
		m.sessionKey = msg.sessionKey
		m.conn = connConnected
		// If we had a message queued while reconnecting, send it now.
		if m.pendingMsg != "" {
			pending := m.pendingMsg
			m.pendingMsg = ""
			m.chatSending = true
			return m, sendChatCmd(m.sessionKey, pending, 0)
		}
		return m, nil

	// ── Refresh result ───────────────────────────────────────────────────────
	case refreshMsg:
		m.lastRefresh = msg.at
		m.status = "Live"
		m.errors = msg.errors
		m.projectItems = msg.taskItems
		m.connectionItems = parseConnections(msg.statusRaw)
		m.sessionItems = parseSessionsCompact(msg.sessionsRaw, 6)
		if len(m.connectionItems) == 0 {
			m.connectionItems = []string{"No channel data"}
		}
		if len(m.sessionItems) == 0 {
			m.sessionItems = []string{"No sessions returned"}
		}
		// If session went missing in the latest refresh, trigger re-discover.
		if m.conn == connConnected && m.sessionKey != "" {
			if parseMainSessionKey(msg.sessionsRaw) == "" {
				m.conn = connDisconnected
				m.chatLines = append(m.chatLines, "⚠ session lost — reconnecting...")
				return m, tea.Batch(tickCmd(), discoverSessionCmd())
			}
		}
		return m, tickCmd()

	// ── Chat reply ───────────────────────────────────────────────────────────
	case chatReplyMsg:
		m.chatSending = false
		if msg.err != nil {
			if msg.retryCount < 3 {
				// Treat as a connection problem — re-discover and retry.
				m.conn = connDisconnected
				m.chatLines = append(m.chatLines, fmt.Sprintf("⚠ send failed (retry %d/3) — reconnecting...", msg.retryCount+1))
				m.pendingMsg = msg.prompt
				m.chatSending = true // keep UI in "sending" state
				return m, discoverSessionCmd()
			}
			m.chatLines = append(m.chatLines, "Amerish [error]: "+msg.err.Error())
			return m, nil
		}
		reply := strings.TrimSpace(msg.reply)
		if reply == "" {
			reply = "(no reply text)"
		}
		for _, line := range strings.Split(reply, "\n") {
			m.chatLines = append(m.chatLines, "Amerish: "+compactLine(line, 180))
		}
		if len(m.chatLines) > 120 {
			m.chatLines = m.chatLines[len(m.chatLines)-120:]
		}
		return m, nil

	// ── Window resize ────────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	// ── Keyboard ─────────────────────────────────────────────────────────────
	case tea.KeyMsg:
		if m.mode == modeEdit {
			switch msg.String() {
			case "esc":
				m.mode = modeMove
				return m, nil
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

				if m.conn != connConnected || m.sessionKey == "" {
					// Not connected yet — queue the message and kick off discovery.
					m.pendingMsg = prompt
					m.chatLines = append(m.chatLines, "⏳ not connected — queued, reconnecting...")
					return m, discoverSessionCmd()
				}
				return m, sendChatCmd(m.sessionKey, prompt, 0)

			case "backspace", "ctrl+h":
				m.chatInput = trimLastRune(m.chatInput)
				return m, nil
			default:
				if len(msg.Runes) > 0 {
					m.chatInput += string(msg.Runes)
				}
				return m, nil
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.status = "Refreshing..."
			return m, tea.Batch(refreshCmd(), discoverSessionCmd())
		case "i":
			if m.focus == paneChat {
				m.mode = modeEdit
			}
			return m, nil
		case "h":
			m.focus = focusLeft(m.focus)
			return m, nil
		case "l":
			m.focus = focusRight(m.focus)
			return m, nil
		case "j":
			m.focus = focusDown(m.focus)
			return m, nil
		case "k":
			m.focus = focusUp(m.focus)
			return m, nil
		case "J":
			m.scrollFocused(1)
			return m, nil
		case "K":
			m.scrollFocused(-1)
			return m, nil
		case "ctrl+d":
			m.scrollFocused(5)
			return m, nil
		case "ctrl+u":
			m.scrollFocused(-5)
			return m, nil
		}
		return m, nil
	}

	return m, nil
}

// ── Focus helpers ────────────────────────────────────────────────────────────

func focusLeft(p pane) pane {
	switch p {
	case paneTasks:
		return paneStatus
	case paneChat:
		return paneSessions
	default:
		return p
	}
}

func focusRight(p pane) pane {
	switch p {
	case paneStatus, paneSessions:
		return paneTasks
	default:
		return p
	}
}

func focusUp(p pane) pane {
	switch p {
	case paneSessions:
		return paneStatus
	case paneChat:
		return paneSessions
	default:
		return p
	}
}

func focusDown(p pane) pane {
	switch p {
	case paneStatus:
		return paneSessions
	case paneSessions, paneTasks:
		return paneChat
	default:
		return p
	}
}

func (m *model) scrollFocused(delta int) {
	switch m.focus {
	case paneStatus:
		m.statusOffset = max(0, m.statusOffset+delta)
	case paneSessions:
		m.sessionsOffset = max(0, m.sessionsOffset+delta)
	case paneTasks:
		m.tasksOffset = max(0, m.tasksOffset+delta)
	case paneChat:
		m.chatOffset = max(0, m.chatOffset+delta)
	}
}

// ── View ─────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Padding(0, 1)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	modeLabel := "MOVE"
	if m.mode == modeEdit {
		modeLabel = "EDIT"
	}

	connLabel := connStateLabel(m.conn)
	sessionLabel := m.sessionKey
	if sessionLabel == "" {
		sessionLabel = "—"
	}

	header := headerStyle.Render(fmt.Sprintf(
		"OpenClaw TUI | %s | %s | session=%s | refreshed=%s",
		modeLabel, connLabel, sessionLabel, m.lastRefresh.Format("15:04:05"),
	))

	bodyH := max(10, m.height-7)
	topH := max(6, bodyH/2)
	bottomH := bodyH - topH
	leftW := max(24, m.width/2)
	rightW := m.width - leftW
	statusH := max(3, topH/2)
	sessionsH := topH - statusH

	statusPane := paneBox("Status", m.focus == paneStatus, leftW, statusH, renderList(m.connectionItems, m.statusOffset, statusH-2))
	sessionsPane := paneBox("Sessions", m.focus == paneSessions, leftW, sessionsH, renderList(m.sessionItems, m.sessionsOffset, sessionsH-2))
	leftTop := lipgloss.JoinVertical(lipgloss.Left, statusPane, sessionsPane)

	tasksPane := paneBox("Tasks", m.focus == paneTasks, rightW, topH, renderTasks(m.projectItems, m.tasksOffset, topH-2))
	top := lipgloss.JoinHorizontal(lipgloss.Top, leftTop, tasksPane)

	chatBody := renderChat(m.chatLines, m.chatOffset, m.chatInput, m.chatSending, m.mode, bottomH-2)
	chatPane := paneBox("Chat", m.focus == paneChat, m.width, bottomH, chatBody)

	footer := muted.Render("MOVE: hjkl focus, J/K scroll, Ctrl+d/u page, r refresh, q quit | EDIT: i (in Chat), Enter send, Esc back")

	parts := []string{header, top, chatPane}
	if len(m.errors) > 0 {
		parts = append(parts, errorStyle.Render("Errors: "+strings.Join(m.errors, " | ")))
	}
	parts = append(parts, footer)
	return strings.Join(parts, "\n\n")
}

func connStateLabel(c connState) string {
	switch c {
	case connConnected:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Render("● connected")
	case connConnecting:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("◌ connecting")
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗ disconnected")
	}
}

// ── Render helpers ────────────────────────────────────────────────────────────

func paneBox(title string, focused bool, width, height int, content string) string {
	b := lipgloss.NormalBorder()
	st := lipgloss.NewStyle().Border(b).Padding(0, 1).Width(max(8, width-4)).Height(max(3, height-2))
	if focused {
		st = st.BorderForeground(lipgloss.Color("45"))
		title = "● " + title
	}
	return st.Render(title + "\n" + content)
}

func renderList(items []string, offset, height int) string {
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
	return strings.Join(out, "\n")
}

func renderTasks(items []taskItem, offset, height int) string {
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
		badgeText := fmt.Sprintf("P%d", it.priority)
		switch it.priority {
		case 1:
			badgeText = p1.Render(badgeText)
		case 2:
			badgeText = p2.Render(badgeText)
		default:
			badgeText = p3.Render(badgeText)
		}
		out = append(out, fmt.Sprintf("☐ %s %s", badgeText, compactLine(it.text, 95)))
	}

	return strings.Join(out, "\n")
}

func renderChat(lines []string, offset int, input string, sending bool, md mode, height int) string {
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
	if md == modeEdit {
		prefix = "I> "
	}
	if sending {
		prefix = "[sending] "
	}
	return strings.Join(append(visible, prefix+input), "\n")
}

// ── Parsers ───────────────────────────────────────────────────────────────────

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

func parseTaskLine(line string) taskItem {
	item := taskItem{priority: 3, text: compactLine(line, 100)}

	rest := strings.TrimSpace(strings.TrimPrefix(line, "- [ ]"))
	if strings.HasPrefix(rest, "[P") {
		closeIdx := strings.Index(rest, "]")
		if closeIdx > 2 {
			p := rest[2:closeIdx]
			if p == "1" || p == "2" || p == "3" {
				item.priority = int(p[0] - '0')
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
		item.text = strings.TrimSpace(rest)
	}
	return item
}

func readTaskItems(path string, limit int) []taskItem {
	b, err := os.ReadFile(path)
	if err != nil {
		return []taskItem{{priority: 1, text: "Unable to read TASKS.md"}}
	}

	var tasks []taskItem
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- [ ]") {
			continue
		}
		t := parseTaskLine(trimmed)
		tasks = append(tasks, t)
	}

	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].priority == tasks[j].priority {
			return tasks[i].text < tasks[j].text
		}
		return tasks[i].priority < tasks[j].priority
	})

	if len(tasks) > limit {
		tasks = tasks[:limit]
	}
	return tasks
}

// ── String utilities ──────────────────────────────────────────────────────────

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

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}
