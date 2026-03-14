package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type model struct {
	width  int
	height int
	status string
}

func initialModel() model {
	return model{status: "Ready"}
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
			return m, nil
		}
	}

	return m, nil
}

func (m model) View() string {
	header := "OpenClaw TUI (Bubble Tea)"
	line := strings.Repeat("=", max(20, len(header)))

	body := fmt.Sprintf("Status: %s\nSize: %dx%d\n\nKeys: r=refresh  q=quit", m.status, m.width, m.height)

	return fmt.Sprintf("%s\n%s\n\n%s\n", header, line, body)
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
