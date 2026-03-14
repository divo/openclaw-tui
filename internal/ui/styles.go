package ui

import "github.com/charmbracelet/lipgloss"

func HeaderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Padding(0, 1)
}

func MutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
}

func ErrorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
}
