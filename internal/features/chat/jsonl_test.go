package chat

import (
	"encoding/json"
	"strings"
	"testing"

	"openclaw-tui/internal/msg"
)

// --- ParseJSONLLine ---

func makeJSONLLine(role, stopReason string, textContent string, toolNames []string) string {
	type contentBlock struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
		Name string `json:"name,omitempty"`
	}
	var content []contentBlock
	if textContent != "" {
		content = append(content, contentBlock{Type: "text", Text: textContent})
	}
	for _, n := range toolNames {
		content = append(content, contentBlock{Type: "toolCall", Name: n})
	}
	line := map[string]any{
		"type": "message",
		"message": map[string]any{
			"role":       role,
			"stopReason": stopReason,
			"content":    content,
		},
	}
	b, _ := json.Marshal(line)
	return string(b)
}

func TestParseJSONLLine_finalAssistantMessage(t *testing.T) {
	raw := makeJSONLLine("assistant", "stop", "Hello there!", nil)
	p := ParseJSONLLine(raw)
	if p == nil {
		t.Fatal("expected non-nil ParsedJSONLLine")
	}
	if !p.IsFinal {
		t.Fatal("expected IsFinal=true for stopReason=stop with text")
	}
	if p.Text != "Hello there!" {
		t.Fatalf("expected text 'Hello there!', got %q", p.Text)
	}
}

func TestParseJSONLLine_toolCallAssistant(t *testing.T) {
	raw := makeJSONLLine("assistant", "toolUse", "", []string{"exec", "Read"})
	p := ParseJSONLLine(raw)
	if p == nil {
		t.Fatal("expected non-nil")
	}
	if p.IsFinal {
		t.Fatal("toolUse should not be IsFinal")
	}
	if len(p.ToolNames) != 2 || p.ToolNames[0] != "exec" {
		t.Fatalf("unexpected tool names: %v", p.ToolNames)
	}
}

func TestParseJSONLLine_errorStopReason(t *testing.T) {
	raw := makeJSONLLine("assistant", "error", "", nil)
	p := ParseJSONLLine(raw)
	if p == nil || !p.IsError {
		t.Fatal("expected IsError=true for stopReason=error")
	}
}

func TestParseJSONLLine_nonMessageType(t *testing.T) {
	raw := `{"type":"model_change","data":{}}`
	p := ParseJSONLLine(raw)
	if p != nil {
		t.Fatal("non-message types should return nil")
	}
}

func TestParseJSONLLine_malformed(t *testing.T) {
	if ParseJSONLLine("not json at all") != nil {
		t.Fatal("malformed JSON should return nil")
	}
	if ParseJSONLLine("") != nil {
		t.Fatal("empty string should return nil")
	}
}

func TestParseJSONLLine_stopWithNoText_notFinal(t *testing.T) {
	// stopReason=stop but no text content — not final for display purposes.
	raw := makeJSONLLine("assistant", "stop", "", nil)
	p := ParseJSONLLine(raw)
	if p == nil {
		t.Fatal("should parse ok")
	}
	if p.IsFinal {
		t.Fatal("stop with no text should not be IsFinal")
	}
}

// --- ProcessTailLines ---

func TestProcessTailLines_showsFinalReply(t *testing.T) {
	state := InitialState()
	state.ActiveMsgID = 1

	raw := makeJSONLLine("assistant", "stop", "The answer is 42.", nil)
	m := msg.ChatTailMsg{Lines: []string{raw}, Done: false}

	newState, done := ProcessTailLines(state, m)
	if !done {
		t.Fatal("expected done=true when IsFinal line seen")
	}
	found := false
	for _, l := range newState.Lines {
		if strings.Contains(l, "42") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected reply text in lines, got: %v", newState.Lines)
	}
}

func TestProcessTailLines_showsToolCalls(t *testing.T) {
	state := InitialState()
	state.ActiveMsgID = 2

	raw := makeJSONLLine("assistant", "toolUse", "", []string{"exec"})
	m := msg.ChatTailMsg{Lines: []string{raw}, Done: false}

	newState, done := ProcessTailLines(state, m)
	if done {
		t.Fatal("tool call alone should not mark done")
	}
	found := false
	for _, l := range newState.Lines {
		if strings.Contains(l, "exec") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected tool name in lines, got: %v", newState.Lines)
	}
}

func TestProcessTailLines_doneOnAgentExit(t *testing.T) {
	state := InitialState()
	state.ActiveMsgID = 3

	m := msg.ChatTailMsg{Lines: nil, Done: true}
	_, done := ProcessTailLines(state, m)
	if !done {
		t.Fatal("expected done=true when m.Done is set")
	}
}

func TestProcessTailLines_errorBailsOut(t *testing.T) {
	state := InitialState()
	state.ActiveMsgID = 4

	raw := makeJSONLLine("assistant", "error", "", nil)
	m := msg.ChatTailMsg{Lines: []string{raw}, Done: false}
	_, done := ProcessTailLines(state, m)
	if !done {
		t.Fatal("error stopReason should mark done")
	}
}
