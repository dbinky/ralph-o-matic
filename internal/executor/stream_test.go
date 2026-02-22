package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatStreamEvent_AssistantText(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"Let me analyze the codebase."}]}}`
	result := FormatStreamEvent(line)
	assert.Equal(t, "Let me analyze the codebase.", result)
}

func TestFormatStreamEvent_AssistantTextTruncated(t *testing.T) {
	longText := make([]byte, 400)
	for i := range longText {
		longText[i] = 'a'
	}
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"` + string(longText) + `"}]}}`
	result := FormatStreamEvent(line)
	assert.Len(t, result, 303) // 300 chars + "..."
	assert.True(t, len(result) <= 303)
}

func TestFormatStreamEvent_ToolUse_Read(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/src/main.go"}}]}}`
	result := FormatStreamEvent(line)
	assert.Equal(t, ">> Read /src/main.go", result)
}

func TestFormatStreamEvent_ToolUse_Edit(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/src/main.go","old_string":"foo","new_string":"bar"}}]}}`
	result := FormatStreamEvent(line)
	assert.Equal(t, ">> Edit /src/main.go", result)
}

func TestFormatStreamEvent_ToolUse_Bash(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}`
	result := FormatStreamEvent(line)
	assert.Equal(t, ">> Bash: go test ./...", result)
}

func TestFormatStreamEvent_ToolUse_Grep(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Grep","input":{"pattern":"func main"}}]}}`
	result := FormatStreamEvent(line)
	assert.Equal(t, `>> Grep: func main`, result)
}

func TestFormatStreamEvent_ToolUse_Unknown(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"WebSearch","input":{"query":"go testing"}}]}}`
	result := FormatStreamEvent(line)
	assert.Equal(t, ">> WebSearch", result)
}

func TestFormatStreamEvent_MixedContent(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"Reading the file."},{"type":"tool_use","name":"Read","input":{"file_path":"/src/main.go"}}]}}`
	result := FormatStreamEvent(line)
	assert.Equal(t, "Reading the file.\n>> Read /src/main.go", result)
}

func TestFormatStreamEvent_ResultSkipped(t *testing.T) {
	line := `{"type":"result","subtype":"success","is_error":false,"result":"Done."}`
	result := FormatStreamEvent(line)
	assert.Empty(t, result)
}

func TestFormatStreamEvent_SystemSkipped(t *testing.T) {
	line := `{"type":"system","subtype":"init","session_id":"abc-123"}`
	result := FormatStreamEvent(line)
	assert.Empty(t, result)
}

func TestFormatStreamEvent_EmptyLine(t *testing.T) {
	assert.Empty(t, FormatStreamEvent(""))
}

func TestFormatStreamEvent_NonJSON(t *testing.T) {
	assert.Empty(t, FormatStreamEvent("not json"))
}

func TestFormatStreamEvent_EmptyText(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"  "}]}}`
	result := FormatStreamEvent(line)
	assert.Empty(t, result)
}
