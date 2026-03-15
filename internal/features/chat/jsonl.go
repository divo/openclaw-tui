package chat

import (
	"encoding/json"
	"strings"
)

// ParsedJSONLLine represents what we extracted from a single session JSONL line.
type ParsedJSONLLine struct {
	Role       string // "user" | "assistant"
	Text       string // non-empty for text content blocks
	StopReason string // "stop" | "toolUse" | "aborted" | "error" | ""
	ToolNames  []string
	IsFinal    bool // stopReason == "stop" with actual text
	IsError    bool // stopReason == "error" or "aborted"
}

type jsonlMessage struct {
	Type    string `json:"type"`
	Message struct {
		Role       string `json:"role"`
		StopReason string `json:"stopReason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Name string `json:"name"` // for toolCall blocks
		} `json:"content"`
	} `json:"message"`
}

// ParseJSONLLine parses a single raw JSONL line from a session file.
// Returns nil if the line is not a message type or not relevant for display.
func ParseJSONLLine(raw string) *ParsedJSONLLine {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var m jsonlMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	if m.Type != "message" {
		return nil
	}
	role := m.Message.Role
	if role != "user" && role != "assistant" {
		return nil
	}

	p := &ParsedJSONLLine{
		Role:       role,
		StopReason: m.Message.StopReason,
	}

	for _, c := range m.Message.Content {
		switch c.Type {
		case "text":
			if t := strings.TrimSpace(c.Text); t != "" {
				if p.Text == "" {
					p.Text = t
				} else {
					p.Text += "\n" + t
				}
			}
		case "toolCall":
			if c.Name != "" {
				p.ToolNames = append(p.ToolNames, c.Name)
			}
		}
	}

	p.IsFinal = (m.Message.StopReason == "stop" && p.Text != "")
	p.IsError = (m.Message.StopReason == "error" || m.Message.StopReason == "aborted")
	return p
}
