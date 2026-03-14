package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	width        int
	height       int
	status       string
	lastRefresh  time.Time
	currentView  string
	connections  []string
	subagents    []string
	projectItems []string
}

func initialModel() model {
	return model{
		status:      "Ready",
		lastRefresh: time.Now(),
		currentView: "main",
		connections: []string{
			"WhatsApp: connected",
			"Telegram: connected",
			"Webchat: connected",
		},
		subagents: []string{
			"(none running)",
		},
		projectItems: []string{
			"[P1] Bug refactor",
			"[P1] Home gym building",
			"[P1] Career fit reset",
		},
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.status = "Refresh requested"
			m.lastRefresh = time.Now()
			return m, nil
		case "1":
			m.currentView = "main"
			return m, nil
		}
	}

	return m, nil
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		Padding(0, 1)

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1)

	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	header := headerStyle.Render(fmt.Sprintf(
		"OpenClaw Ops TUI | view=%s | status=%s | refreshed=%s",
		m.currentView,
		m.status,
		m.lastRefresh.Format("15:04:05"),
	))

	leftWidth := max(30, int(float64(m.width)*0.58))
	rightWidth := max(20, m.width-leftWidth-1)
	bodyHeight := max(10, m.height-7)

	left := panelStyle.
		Width(leftWidth - 4).
		Height(bodyHeight).
		Render(renderLeftPane(m.projectItems))

	right := panelStyle.
		Width(rightWidth - 4).
		Height(bodyHeight).
		Render(renderRightPane(m.subagents, m.connections))

	row := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	footer := muted.Render("Keys: r=refresh  q=quit")

	return strings.Join([]string{header, row, footer}, "\n\n")
}

func renderLeftPane(items []string) string {
	lines := []string{"Project / Task Focus", "", "Top open items:"}
	for _, item := range items {
		lines = append(lines, "- "+item)
	}
	lines = append(lines, "", "Next: wire real TASKS.md + memory parser")
	return strings.Join(lines, "\n")
}

func renderRightPane(subagents, connections []string) string {
	lines := []string{"Sessions / Subagents / Connections", "", "Subagents:"}
	for _, s := range subagents {
		lines = append(lines, "- "+s)
	}
	lines = append(lines, "", "Connections:")
	for _, c := range connections {
		lines = append(lines, "- "+c)
	}
	lines = append(lines, "", "Next: wire openclaw status/sessions/subagents")
	return strings.Join(lines, "\n")
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
