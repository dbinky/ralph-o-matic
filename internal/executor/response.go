package executor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ResponseMetadata holds parsed data from Claude's JSON output
type ResponseMetadata struct {
	SessionID     string   `json:"session_id"`
	FilesModified int      `json:"files_modified"`
	HasErrors     bool     `json:"has_errors"`
	IsError       bool     `json:"is_error"`
	IsTestOnly    bool     `json:"is_test_only"`
	ExitSignal    bool     `json:"exit_signal"`
	Completed     bool     `json:"completed"`
	WorkSummary   string   `json:"work_summary"`
	ErrorMessages []string `json:"error_messages,omitempty"`
}

// claudeResult represents the JSON output from `claude --print --output-format json`
type claudeResult struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	IsError   bool   `json:"is_error"`
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
}

// ParseResponse parses Claude CLI JSON output into ResponseMetadata.
// Handles the standard result object format. If the input contains non-JSON
// text before the JSON (e.g. stderr), it extracts the JSON portion.
func ParseResponse(jsonOutput []byte) (*ResponseMetadata, error) {
	if len(jsonOutput) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	// Try to find JSON in the output (may have stderr prefix)
	data := extractJSON(jsonOutput)
	if data == nil {
		return nil, fmt.Errorf("no valid JSON found in response")
	}

	var cr claudeResult
	if err := json.Unmarshal(data, &cr); err != nil {
		return nil, fmt.Errorf("failed to parse claude response: %w", err)
	}

	meta := &ResponseMetadata{
		SessionID: cr.SessionID,
		IsError:   cr.IsError,
	}

	// Parse RALPH_STATUS block if present
	parseRalphStatus(cr.Result, meta)

	// Note: We intentionally do NOT use keyword-based completion detection
	// (e.g., "all tasks complete", "ready for review") because these phrases
	// can appear in natural language without indicating actual completion.
	// Completion must be signaled via RALPH_STATUS block or <promise> tags.

	// Detect errors in result text
	detectErrors(cr.Result, cr.IsError, meta)

	// Detect test-only loops
	meta.IsTestOnly = detectTestOnly(cr.Result)

	// Extract work summary (first meaningful line)
	meta.WorkSummary = extractSummary(cr.Result)

	return meta, nil
}

// extractJSON finds the first JSON object in the output
func extractJSON(data []byte) []byte {
	// Try parsing as-is first
	if json.Valid(data) {
		return data
	}

	// Look for first '{' character (skip stderr lines)
	idx := bytes.IndexByte(data, '{')
	if idx >= 0 {
		candidate := data[idx:]
		if json.Valid(candidate) {
			return candidate
		}
	}

	return nil
}

var ralphStatusRe = regexp.MustCompile(`(?s)---RALPH_STATUS---(.*?)---END_RALPH_STATUS---`)

func parseRalphStatus(result string, meta *ResponseMetadata) {
	matches := ralphStatusRe.FindStringSubmatch(result)
	if len(matches) < 2 {
		return
	}

	block := matches[1]
	exitSignalExplicit := false

	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch strings.ToUpper(key) {
		case "STATUS":
			if strings.EqualFold(val, "COMPLETE") {
				meta.Completed = true
			}
		case "EXIT_SIGNAL":
			exitSignalExplicit = true
			meta.ExitSignal = strings.EqualFold(val, "true")
		case "FILES_MODIFIED":
			_, _ = fmt.Sscanf(val, "%d", &meta.FilesModified)
		case "WORK_TYPE":
			// informational only
		}
	}

	// If STATUS: COMPLETE but no explicit EXIT_SIGNAL, infer exit
	if meta.Completed && !exitSignalExplicit {
		meta.ExitSignal = true
	}
}

var errorLineRe = regexp.MustCompile(`(?m)^(?:Error:|ERROR:|error:)\s*(.+)`)

func detectErrors(result string, isError bool, meta *ResponseMetadata) {
	if isError {
		meta.HasErrors = true
	}

	matches := errorLineRe.FindAllStringSubmatch(result, -1)
	for _, m := range matches {
		if len(m) >= 2 {
			meta.ErrorMessages = append(meta.ErrorMessages, strings.TrimSpace(m[1]))
			meta.HasErrors = true
		}
	}
}

var (
	testPatterns = regexp.MustCompile(`(?i)\b(?:running tests?|npm test|jest|pytest|bats|go test)\b`)
	implPatterns = regexp.MustCompile(`(?i)\b(?:implement|creating|writing|adding|function|class|feature|fix|refactor)\b`)
)

func detectTestOnly(result string) bool {
	testCount := len(testPatterns.FindAllString(result, -1))
	implCount := len(implPatterns.FindAllString(result, -1))
	return testCount > 0 && implCount == 0
}

func extractSummary(result string) string {
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "---") {
			if len(line) > 200 {
				return line[:200]
			}
			return line
		}
	}
	return ""
}
