package executor

import (
	"encoding/json"
	"fmt"
	"strings"
)

// streamEvent represents a stream-json event from Claude Code.
// Only the fields we need for formatting are included.
type streamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Message struct {
		Content []streamContent `json:"content"`
	} `json:"message"`
}

type streamContent struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// FormatStreamEvent parses a stream-json event line and returns a
// human-readable summary for display in the terminal. Returns empty
// string for events that should be skipped (tool results, heartbeats, etc).
func FormatStreamEvent(line string) string {
	if len(line) == 0 || line[0] != '{' {
		return ""
	}

	var ev streamEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return ""
	}

	switch ev.Type {
	case "assistant":
		return formatAssistant(ev.Message.Content)
	case "result":
		return "" // worker logs completion separately
	default:
		return ""
	}
}

func formatAssistant(content []streamContent) string {
	var parts []string
	for _, c := range content {
		switch c.Type {
		case "text":
			text := strings.TrimSpace(c.Text)
			if text == "" {
				continue
			}
			if len(text) > 300 {
				text = text[:300] + "..."
			}
			parts = append(parts, text)
		case "tool_use":
			parts = append(parts, formatToolUse(c.Name, c.Input))
		}
	}
	return strings.Join(parts, "\n")
}

func formatToolUse(name string, input json.RawMessage) string {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(input, &fields)

	switch name {
	case "Read":
		return fmt.Sprintf(">> Read %s", extractString(fields["file_path"]))
	case "Edit":
		return fmt.Sprintf(">> Edit %s", extractString(fields["file_path"]))
	case "Write":
		return fmt.Sprintf(">> Write %s", extractString(fields["file_path"]))
	case "Bash":
		cmd := extractString(fields["command"])
		if len(cmd) > 120 {
			cmd = cmd[:120] + "..."
		}
		return fmt.Sprintf(">> Bash: %s", cmd)
	case "Grep":
		return fmt.Sprintf(">> Grep: %s", extractString(fields["pattern"]))
	case "Glob":
		return fmt.Sprintf(">> Glob: %s", extractString(fields["pattern"]))
	default:
		return fmt.Sprintf(">> %s", name)
	}
}

func extractString(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return string(raw)
	}
	return s
}
