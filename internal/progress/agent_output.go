package progress

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
)

type agentTokenUsage struct {
	Out    int
	In     int
	Cached int
}

type agentLastAction struct {
	Elapsed   time.Duration
	EventType string
	Content   string
	Tokens    agentTokenUsage
	Italic    bool
}

type agentOutputSummary struct {
	Totals     agentTokenUsage
	LastAction *agentLastAction
}

func summarizeAgentStepOutput(step Step) (agentOutputSummary, bool) {
	stdoutPath := strings.TrimSpace(step.Artifacts.Stdout)
	if stdoutPath == "" {
		return agentOutputSummary{}, false
	}
	data, err := os.ReadFile(stdoutPath)
	if err != nil {
		return agentOutputSummary{}, false
	}

	switch step.Type {
	case "claude":
		return summarizeClaudeOutput(data)
	case "codex":
		return summarizeCodexOutput(data)
	default:
		return agentOutputSummary{}, false
	}
}

func summarizeClaudeOutput(data []byte) (agentOutputSummary, bool) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var summary agentOutputSummary
	var firstAt time.Time
	var hasFirstAt bool
	var sawTokens bool
	var sawAction bool

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		at, okAt := parseEventTimestamp(event)
		if okAt && !hasFirstAt {
			firstAt = at
			hasFirstAt = true
		}

		message, _ := mapField(event, "message")
		if message != nil {
			usage := agentTokenUsage{
				In:  intField(message, "input_tokens"),
				Out: intField(message, "output_tokens"),
				Cached: intField(message, "cache_creation_input_tokens") +
					intField(message, "cache_read_input_tokens"),
			}
			if usage.In > 0 || usage.Out > 0 || usage.Cached > 0 {
				sawTokens = true
			}
			summary.Totals.In += usage.In
			summary.Totals.Out += usage.Out
			summary.Totals.Cached += usage.Cached

			eventType, content, italic := claudeEventDescription(event)
			if eventType != "" || content != "" {
				elapsed := time.Duration(0)
				if hasFirstAt && okAt {
					elapsed = at.Sub(firstAt)
				}
				summary.LastAction = &agentLastAction{
					Elapsed:   elapsed,
					EventType: eventType,
					Content:   content,
					Tokens:    usage,
					Italic:    italic,
				}
				sawAction = true
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return agentOutputSummary{}, false
	}

	if !sawTokens && !sawAction {
		return agentOutputSummary{}, false
	}
	return summary, true
}

func summarizeCodexOutput(data []byte) (agentOutputSummary, bool) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var summary agentOutputSummary
	var firstAt time.Time
	var hasFirstAt bool
	var lastAction agentLastAction
	var hasAction bool
	var sawTokens bool

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		at, okAt := parseEventTimestamp(entry)
		if okAt && !hasFirstAt {
			firstAt = at
			hasFirstAt = true
		}

		entryType, _ := stringField(entry, "type")
		payload, _ := mapField(entry, "payload")

		if entryType == "event_msg" && payload != nil {
			payloadType, _ := stringField(payload, "type")
			if payloadType == "token_count" {
				if info, _ := mapField(payload, "info"); info != nil {
					if total, _ := mapField(info, "total_token_usage"); total != nil {
						summary.Totals.In = intField(total, "input_tokens")
						summary.Totals.Out = intField(total, "output_tokens")
						summary.Totals.Cached = intField(total, "cached_input_tokens")
						if summary.Totals.In > 0 || summary.Totals.Out > 0 || summary.Totals.Cached > 0 {
							sawTokens = true
						}
					}
					if last, _ := mapField(info, "last_token_usage"); last != nil {
						if summary.LastAction == nil {
							summary.LastAction = &agentLastAction{}
						}
						summary.LastAction.Tokens = agentTokenUsage{
							In:     intField(last, "input_tokens"),
							Out:    intField(last, "output_tokens"),
							Cached: intField(last, "cached_input_tokens"),
						}
					}
				}
				continue
			}

			if action, ok := codexActionFromEventMessage(payload, at, okAt, firstAt, hasFirstAt); ok {
				lastAction = action
				hasAction = true
			}
			continue
		}

		if entryType == "response_item" && payload != nil {
			if action, ok := codexActionFromResponseItem(payload, at, okAt, firstAt, hasFirstAt); ok {
				lastAction = action
				hasAction = true
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return agentOutputSummary{}, false
	}

	if hasAction {
		if summary.LastAction == nil {
			summary.LastAction = &agentLastAction{}
		}
		summary.LastAction.Elapsed = lastAction.Elapsed
		summary.LastAction.EventType = lastAction.EventType
		summary.LastAction.Content = lastAction.Content
		summary.LastAction.Italic = lastAction.Italic
	}

	if !sawTokens && summary.LastAction == nil {
		return agentOutputSummary{}, false
	}
	return summary, true
}

func codexActionFromEventMessage(payload map[string]any, at time.Time, okAt bool, firstAt time.Time, hasFirstAt bool) (agentLastAction, bool) {
	payloadType, _ := stringField(payload, "type")
	if payloadType != "exec_command_end" {
		return agentLastAction{}, false
	}

	eventType := "exec_command"
	content := ""
	if parsed, ok := sliceField(payload, "parsed_cmd"); ok && len(parsed) > 0 {
		if first, ok := parsed[0].(map[string]any); ok {
			if name, ok := stringField(first, "name"); ok && strings.TrimSpace(name) != "" {
				eventType = name
			}
			if cmd, ok := stringField(first, "cmd"); ok && strings.TrimSpace(cmd) != "" {
				content = cmd
			}
			if content == "" {
				if pattern, ok := stringField(first, "pattern"); ok {
					content = pattern
				}
			}
			if content == "" {
				if path, ok := stringField(first, "path"); ok {
					content = path
				}
			}
		}
	}
	if content == "" {
		if cmd, ok := sliceStringField(payload, "command"); ok {
			content = strings.Join(cmd, " ")
		}
	}

	elapsed := time.Duration(0)
	if hasFirstAt && okAt {
		elapsed = at.Sub(firstAt)
	}
	return agentLastAction{
		Elapsed:   elapsed,
		EventType: eventType,
		Content:   content,
	}, true
}

func codexActionFromResponseItem(payload map[string]any, at time.Time, okAt bool, firstAt time.Time, hasFirstAt bool) (agentLastAction, bool) {
	payloadType, _ := stringField(payload, "type")
	switch payloadType {
	case "function_call":
		name, _ := stringField(payload, "name")
		arguments, _ := stringField(payload, "arguments")
		eventType, content := toolEventTypeAndContent(name, arguments)
		elapsed := time.Duration(0)
		if hasFirstAt && okAt {
			elapsed = at.Sub(firstAt)
		}
		return agentLastAction{
			Elapsed:   elapsed,
			EventType: eventType,
			Content:   content,
		}, true
	case "custom_tool_call":
		name, _ := stringField(payload, "name")
		input, _ := stringField(payload, "input")
		eventType, content := toolEventTypeAndContent(name, input)
		elapsed := time.Duration(0)
		if hasFirstAt && okAt {
			elapsed = at.Sub(firstAt)
		}
		return agentLastAction{
			Elapsed:   elapsed,
			EventType: eventType,
			Content:   content,
		}, true
	case "reasoning":
		elapsed := time.Duration(0)
		if hasFirstAt && okAt {
			elapsed = at.Sub(firstAt)
		}
		return agentLastAction{
			Elapsed:   elapsed,
			EventType: "thinking",
			Content:   "thinking",
			Italic:    true,
		}, true
	default:
		return agentLastAction{}, false
	}
}

func claudeEventDescription(event map[string]any) (string, string, bool) {
	content, ok := sliceField(event, "content")
	if !ok || len(content) == 0 {
		return "", "", false
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		return "", "", false
	}

	contentType, _ := stringField(first, "type")
	switch contentType {
	case "tool_use":
		name, _ := stringField(first, "name")
		eventType := strings.TrimSpace(name)
		if eventType == "" {
			eventType = "tool_use"
		}
		input, _ := mapField(first, "input")
		return eventType, toolContent(eventType, input), false
	case "thinking":
		thinking, _ := stringField(first, "thinking")
		return "thinking", thinking, true
	default:
		text, _ := stringField(first, "text")
		if text == "" {
			text = contentType
		}
		return contentType, text, false
	}
}

func toolEventTypeAndContent(name string, rawInput string) (string, string) {
	eventType := strings.TrimSpace(name)
	if eventType == "" {
		eventType = "tool_use"
	}

	rawInput = strings.TrimSpace(rawInput)
	if rawInput == "" {
		return eventType, ""
	}

	var input map[string]any
	if err := json.Unmarshal([]byte(rawInput), &input); err != nil {
		return eventType, rawInput
	}
	return eventType, toolContent(eventType, input)
}

func toolContent(eventType string, input map[string]any) string {
	if input == nil {
		return ""
	}

	switch eventType {
	case "Glob", "Grep":
		if pattern, ok := stringField(input, "pattern"); ok {
			return pattern
		}
	case "Bash":
		if command, ok := stringField(input, "command"); ok {
			return command
		}
	case "StructuredOutput":
		if message, ok := stringField(input, "message"); ok {
			return message
		}
	}

	if path, ok := stringField(input, "file_path"); ok {
		return path
	}
	if command, ok := stringField(input, "command"); ok {
		return command
	}
	if pattern, ok := stringField(input, "pattern"); ok {
		return pattern
	}

	encoded, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func formatAgentTokenSummary(base string, totals agentTokenUsage) string {
	suffix := fmt.Sprintf(
		"Out: %s In: %s Cached: %s",
		formatTokenCount(totals.Out),
		formatTokenCount(totals.In),
		formatTokenCount(totals.Cached),
	)
	base = strings.TrimSpace(base)
	if base == "" {
		return suffix
	}
	return base + "  " + suffix
}

func formatTokenCount(value int) string {
	if value < 1000 {
		return fmt.Sprintf("%d", value)
	}

	abs := value
	sign := ""
	if value < 0 {
		abs = -value
		sign = "-"
	}

	if abs >= 1_000_000 {
		return sign + compactWithSuffix(float64(abs)/1_000_000, "M")
	}
	return sign + compactWithSuffix(float64(abs)/1_000, "k")
}

func compactWithSuffix(base float64, suffix string) string {
	rounded := math.Round(base*10) / 10
	if math.Mod(rounded, 1) == 0 {
		return fmt.Sprintf("%.0f%s", rounded, suffix)
	}
	return fmt.Sprintf("%.1f%s", rounded, suffix)
}

func formatAgentEventElapsed(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	minutes := int(elapsed / time.Minute)
	hours := minutes / 60
	remainingMinutes := minutes % 60
	return fmt.Sprintf("%02d:%02d", hours, remainingMinutes)
}

func parseEventTimestamp(event map[string]any) (time.Time, bool) {
	raw, ok := stringField(event, "timestamp")
	if !ok || strings.TrimSpace(raw) == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func intField(source map[string]any, key string) int {
	raw, ok := source[key]
	if !ok {
		return 0
	}
	switch typed := raw.(type) {
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case int32:
		return int(typed)
	case json.Number:
		if value, err := typed.Int64(); err == nil {
			return int(value)
		}
		if value, err := typed.Float64(); err == nil {
			return int(value)
		}
	}
	return 0
}

func mapField(source map[string]any, key string) (map[string]any, bool) {
	raw, ok := source[key]
	if !ok {
		return nil, false
	}
	typed, ok := raw.(map[string]any)
	return typed, ok
}

func sliceField(source map[string]any, key string) ([]any, bool) {
	raw, ok := source[key]
	if !ok {
		return nil, false
	}
	typed, ok := raw.([]any)
	return typed, ok
}

func stringField(source map[string]any, key string) (string, bool) {
	raw, ok := source[key]
	if !ok {
		return "", false
	}
	typed, ok := raw.(string)
	return typed, ok
}

func sliceStringField(source map[string]any, key string) ([]string, bool) {
	raw, ok := source[key]
	if !ok {
		return nil, false
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, false
		}
		values = append(values, text)
	}
	return values, true
}
