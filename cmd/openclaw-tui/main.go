package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"openclaw-tui/internal/app"
	"openclaw-tui/internal/transport"
)

func main() {
	m := app.NewModel(transport.NewCLITransport())
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}
