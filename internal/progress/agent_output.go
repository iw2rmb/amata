package progress

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
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
	Totals      agentTokenUsage
	LastAction  *agentLastAction
	LastEventAt time.Time
	Thinking    string
	Shell       string
}

func summarizeAgentStepOutput(step Step) (agentOutputSummary, bool) {
	stdout, stdoutMod, hasStdout := readArtifactFile(step.Artifacts.Stdout)
	stderr, stderrMod, hasStderr := readArtifactFile(step.Artifacts.Stderr)
	if !hasStdout && !hasStderr {
		return agentOutputSummary{}, false
	}

	switch step.Type {
	case "claude":
		summary, ok := summarizeClaudeOutput(stdout)
		if !ok {
			return summary, ok
		}
		if summary.LastEventAt.IsZero() {
			summary.LastEventAt = latestTime(stdoutMod, stderrMod)
		}
		return summary, ok
	case "codex":
		summary, ok := summarizeCodexOutput(stdout, stderr)
		if !ok {
			return summary, ok
		}
		if summary.LastEventAt.IsZero() {
			summary.LastEventAt = latestTime(stdoutMod, stderrMod)
		}
		return summary, ok
	default:
		return agentOutputSummary{}, false
	}
}

func readArtifactFile(path string) ([]byte, time.Time, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, time.Time{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, false
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		return data, time.Time{}, true
	}
	return data, info.ModTime().UTC(), true
}

func latestTime(values ...time.Time) time.Time {
	latest := time.Time{}
	for _, value := range values {
		if value.IsZero() {
			continue
		}
		if latest.IsZero() || value.After(latest) {
			latest = value
		}
	}
	return latest
}

func summarizeClaudeOutput(data []byte) (agentOutputSummary, bool) {
	if len(data) == 0 {
		return agentOutputSummary{}, false
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var summary agentOutputSummary
	var firstAt time.Time
	var hasFirstAt bool
	var sawTokens bool
	var sawAction bool
	var thinkingDelta string
	var hasThinkingDelta bool

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

		if usages := tokenUsagesFromTokensFields(event); len(usages) > 0 {
			for _, usage := range usages {
				summary.Totals.In += usage.In
				summary.Totals.Out += usage.Out
				summary.Totals.Cached += usage.Cached
				if usage.In > 0 || usage.Out > 0 || usage.Cached > 0 {
					sawTokens = true
				}
			}
		}

		usage, replaceTotals, hasUsage := claudeTokenUsageFromEvent(event)
		if !hasUsage {
			usage = agentTokenUsage{}
		}
		if hasUsage {
			if usage.In > 0 || usage.Out > 0 || usage.Cached > 0 {
				sawTokens = true
			}
			if replaceTotals {
				summary.Totals = usage
			} else {
				summary.Totals.In += usage.In
				summary.Totals.Out += usage.Out
				summary.Totals.Cached += usage.Cached
			}
		}

		if update, ok := claudeThinkingUpdateFromEvent(event); ok {
			switch update.Mode {
			case claudeThinkingModeDelta:
				thinkingDelta += update.Text
				hasThinkingDelta = true
				summary.Thinking = thinkingDelta
			case claudeThinkingModeFull:
				thinkingDelta = ""
				hasThinkingDelta = false
				summary.Thinking = update.Text
			}
		}

		eventType, content, italic := claudeEventDescription(event)
		if eventType == "" && content == "" {
			continue
		}
		if strings.EqualFold(eventType, "thinking") && hasThinkingDelta {
			// Keep action content aligned with the currently displayed accumulated delta.
			content = thinkingDelta
		}
		if strings.EqualFold(eventType, "Shell") {
			if text := strings.TrimSpace(content); text != "" {
				summary.Shell = text
			}
		}

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
		if okAt {
			summary.LastEventAt = at
		}
		sawAction = true
	}

	if err := scanner.Err(); err != nil {
		return agentOutputSummary{}, false
	}

	if !sawTokens && !sawAction {
		return agentOutputSummary{}, false
	}
	return summary, true
}

func summarizeCodexOutput(stdout []byte, stderr []byte) (agentOutputSummary, bool) {
	stdoutSummary, hasStdoutSummary := summarizeCodexJSONOutput(stdout)
	stderrSummary, hasStderrSummary := summarizeCodexStderrOutput(stderr)

	switch {
	case hasStdoutSummary && hasStderrSummary:
		if summaryHasNoTokens(stdoutSummary) {
			stdoutSummary.Totals = stderrSummary.Totals
		}
		if stdoutSummary.LastAction == nil {
			stdoutSummary.LastAction = stderrSummary.LastAction
		}
		return stdoutSummary, true
	case hasStdoutSummary:
		return stdoutSummary, true
	case hasStderrSummary:
		return stderrSummary, true
	default:
		return agentOutputSummary{}, false
	}
}

func summarizeCodexJSONOutput(data []byte) (agentOutputSummary, bool) {
	if len(data) == 0 {
		return agentOutputSummary{}, false
	}

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
		if usageMap, _ := mapField(entry, "usage"); usageMap != nil {
			if usage, ok := usageFromMap(usageMap); ok {
				summary.Totals = usage
				if usage.In > 0 || usage.Out > 0 || usage.Cached > 0 {
					sawTokens = true
				}
			}
		}
		if usages := tokenUsagesFromTokensFields(entry); len(usages) > 0 {
			for _, usage := range usages {
				summary.Totals.In += usage.In
				summary.Totals.Out += usage.Out
				summary.Totals.Cached += usage.Cached
				if usage.In > 0 || usage.Out > 0 || usage.Cached > 0 {
					sawTokens = true
				}
			}
		}
		if action, ok := codexActionFromItemEntry(entry, at, okAt, firstAt, hasFirstAt); ok {
			lastAction = action
			hasAction = true
			if okAt {
				summary.LastEventAt = at
			}
			if strings.EqualFold(strings.TrimSpace(action.EventType), "Shell") {
				summary.Shell = strings.TrimSpace(action.Content)
			}
		}
		if thinking, ok := codexThinkingFromItemEntry(entry); ok {
			summary.Thinking = thinking
		}

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
				if okAt {
					summary.LastEventAt = at
				}
				if strings.EqualFold(strings.TrimSpace(action.EventType), "Shell") {
					summary.Shell = strings.TrimSpace(action.Content)
				}
			}
			continue
		}

		if entryType == "response_item" && payload != nil {
			if action, ok := codexActionFromResponseItem(payload, at, okAt, firstAt, hasFirstAt); ok {
				lastAction = action
				hasAction = true
				if okAt {
					summary.LastEventAt = at
				}
				if strings.EqualFold(strings.TrimSpace(action.EventType), "Shell") {
					summary.Shell = strings.TrimSpace(action.Content)
				}
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

func summarizeCodexStderrOutput(data []byte) (agentOutputSummary, bool) {
	if len(data) == 0 {
		return agentOutputSummary{}, false
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var summary agentOutputSummary
	var pendingTool string
	expectTokens := false
	sawTokens := false
	sawAction := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if expectTokens {
			expectTokens = false
			if value, ok := parseCompactInt(line); ok {
				summary.Totals.In = value
				sawTokens = true
			}
			continue
		}

		if strings.EqualFold(line, "tokens used") {
			expectTokens = true
			continue
		}

		if pendingTool != "" {
			eventType := pendingTool
			content := line
			if pendingTool == "Shell" {
				if index := strings.Index(content, " in "); index > 0 {
					content = strings.TrimSpace(content[:index])
				}
			}
			summary.LastAction = &agentLastAction{
				EventType: eventType,
				Content:   content,
			}
			pendingTool = ""
			sawAction = true
			if strings.EqualFold(eventType, "Shell") {
				summary.Shell = strings.TrimSpace(content)
			}
			continue
		}

		switch strings.ToLower(line) {
		case "exec":
			pendingTool = "Shell"
		case "read":
			pendingTool = "Read"
		case "write":
			pendingTool = "Write"
		case "grep":
			pendingTool = "Grep"
		}
	}

	if err := scanner.Err(); err != nil {
		return agentOutputSummary{}, false
	}

	if sawTokens && summary.LastAction != nil && isZeroUsage(summary.LastAction.Tokens) {
		summary.LastAction.Tokens = summary.Totals
	}

	if !sawTokens && !sawAction {
		return agentOutputSummary{}, false
	}
	return summary, true
}

func summaryHasNoTokens(summary agentOutputSummary) bool {
	return isZeroUsage(summary.Totals)
}

func isZeroUsage(usage agentTokenUsage) bool {
	return usage.In == 0 && usage.Out == 0 && usage.Cached == 0
}

func parseCompactInt(raw string) (int, bool) {
	sanitized := strings.ReplaceAll(strings.TrimSpace(raw), ",", "")
	sanitized = strings.ReplaceAll(sanitized, "_", "")
	if sanitized == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(sanitized)
	if err != nil {
		return 0, false
	}
	return parsed, true
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
			if strings.EqualFold(eventType, "Bash") || strings.EqualFold(eventType, "exec_command") {
				eventType = "Shell"
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

func codexActionFromItemEntry(entry map[string]any, at time.Time, okAt bool, firstAt time.Time, hasFirstAt bool) (agentLastAction, bool) {
	entryType, _ := stringField(entry, "type")
	if entryType != "item.started" && entryType != "item.completed" {
		return agentLastAction{}, false
	}

	item, _ := mapField(entry, "item")
	if item == nil {
		return agentLastAction{}, false
	}
	itemType, _ := stringField(item, "type")
	if itemType != "command_execution" {
		return agentLastAction{}, false
	}

	command, _ := stringField(item, "command")
	command = strings.TrimSpace(command)
	if command == "" {
		return agentLastAction{}, false
	}

	elapsed := time.Duration(0)
	if hasFirstAt && okAt {
		elapsed = at.Sub(firstAt)
	}
	return agentLastAction{
		Elapsed:   elapsed,
		EventType: "Shell",
		Content:   command,
	}, true
}

func codexThinkingFromItemEntry(entry map[string]any) (string, bool) {
	item, _ := mapField(entry, "item")
	if item == nil {
		return "", false
	}
	itemType, _ := stringField(item, "type")
	if itemType != "agent_message" {
		return "", false
	}
	text, ok := mapOrStringJSONField(item, "text")
	if !ok || text == nil {
		return "", false
	}
	thinking, ok := stringField(text, "$thinking")
	if !ok {
		return "", false
	}
	thinking = strings.TrimSpace(thinking)
	if thinking == "" {
		return "", false
	}
	return thinking, true
}

func mapOrStringJSONField(source map[string]any, key string) (map[string]any, bool) {
	textMap, _ := mapField(source, key)
	if textMap != nil {
		return textMap, true
	}
	rawText, ok := stringField(source, key)
	if !ok {
		return nil, false
	}
	rawText = strings.TrimSpace(rawText)
	if rawText == "" {
		return nil, false
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(rawText), &decoded); err != nil {
		return nil, false
	}
	return decoded, true
}

type claudeThinkingMode string

const (
	claudeThinkingModeDelta claudeThinkingMode = "delta"
	claudeThinkingModeFull  claudeThinkingMode = "full"
)

type claudeThinkingUpdate struct {
	Mode claudeThinkingMode
	Text string
}

func claudeThinkingUpdateFromEvent(event map[string]any) (claudeThinkingUpdate, bool) {
	if event == nil {
		return claudeThinkingUpdate{}, false
	}

	eventType, _ := stringField(event, "type")
	if eventType == "stream_event" {
		nested, _ := mapField(event, "event")
		return claudeThinkingUpdateFromStreamEvent(nested)
	}

	content, ok := claudeContentBlocks(event)
	if !ok || len(content) == 0 {
		return claudeThinkingUpdate{}, false
	}
	for _, block := range content {
		contentBlock, ok := block.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := stringField(contentBlock, "type")
		if blockType != "thinking" {
			continue
		}
		text, ok := stringField(contentBlock, "thinking")
		if !ok {
			continue
		}
		return claudeThinkingUpdate{Mode: claudeThinkingModeFull, Text: text}, true
	}
	return claudeThinkingUpdate{}, false
}

func claudeThinkingUpdateFromStreamEvent(event map[string]any) (claudeThinkingUpdate, bool) {
	if event == nil {
		return claudeThinkingUpdate{}, false
	}

	eventType, _ := stringField(event, "type")
	switch eventType {
	case "content_block_delta":
		delta, _ := mapField(event, "delta")
		if delta == nil {
			return claudeThinkingUpdate{}, false
		}
		deltaType, _ := stringField(delta, "type")
		if deltaType != "thinking_delta" {
			return claudeThinkingUpdate{}, false
		}
		text, ok := stringField(delta, "thinking")
		if !ok {
			return claudeThinkingUpdate{}, false
		}
		return claudeThinkingUpdate{Mode: claudeThinkingModeDelta, Text: text}, true
	case "content_block_start":
		contentBlock, _ := mapField(event, "content_block")
		if contentBlock == nil {
			return claudeThinkingUpdate{}, false
		}
		blockType, _ := stringField(contentBlock, "type")
		if blockType != "thinking" {
			return claudeThinkingUpdate{}, false
		}
		text, ok := stringField(contentBlock, "thinking")
		if !ok {
			return claudeThinkingUpdate{}, false
		}
		return claudeThinkingUpdate{Mode: claudeThinkingModeFull, Text: text}, true
	default:
		return claudeThinkingUpdate{}, false
	}
}

func claudeEventDescription(event map[string]any) (string, string, bool) {
	eventType, _ := stringField(event, "type")
	if eventType == "stream_event" {
		nested, _ := mapField(event, "event")
		return claudeStreamEventDescription(nested)
	}

	content, ok := claudeContentBlocks(event)
	if !ok || len(content) == 0 {
		if eventType == "result" {
			if stopReason, ok := stringField(event, "stop_reason"); ok && strings.TrimSpace(stopReason) != "" {
				return "result", stopReason, false
			}
		}
		return "", "", false
	}
	var bestType string
	var bestContent string
	var bestItalic bool
	for _, block := range content {
		current, ok := block.(map[string]any)
		if !ok {
			continue
		}
		contentType, _ := stringField(current, "type")
		switch contentType {
		case "tool_use":
			name, _ := stringField(current, "name")
			eventType := normalizeAgentEventType(strings.TrimSpace(name))
			if eventType == "" {
				eventType = "tool_use"
			}
			input, _ := mapField(current, "input")
			bestType = eventType
			bestContent = toolContent(eventType, input)
			bestItalic = false
		case "thinking":
			thinking, _ := stringField(current, "thinking")
			if strings.TrimSpace(thinking) == "" {
				thinking = "thinking"
			}
			bestType = "thinking"
			bestContent = thinking
			bestItalic = true
		default:
			text, _ := stringField(current, "text")
			if text == "" {
				text = contentType
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
			bestType = contentType
			bestContent = text
			bestItalic = false
		}
	}
	return bestType, bestContent, bestItalic
}

func claudeStreamEventDescription(event map[string]any) (string, string, bool) {
	if event == nil {
		return "", "", false
	}

	eventType, _ := stringField(event, "type")
	switch eventType {
	case "content_block_start":
		contentBlock, _ := mapField(event, "content_block")
		if contentBlock == nil {
			return "", "", false
		}
		blockType, _ := stringField(contentBlock, "type")
		switch blockType {
		case "thinking":
			thinking, _ := stringField(contentBlock, "thinking")
			thinking = strings.TrimSpace(thinking)
			if thinking == "" {
				return "", "", false
			}
			return "thinking", thinking, true
		case "tool_use":
			name, _ := stringField(contentBlock, "name")
			normalized := normalizeAgentEventType(name)
			if !strings.EqualFold(normalized, "Shell") {
				return "", "", false
			}
			input, _ := mapField(contentBlock, "input")
			content := strings.TrimSpace(toolContent(normalized, input))
			if content == "" {
				return "", "", false
			}
			return normalized, content, false
		default:
			return "", "", false
		}
	case "content_block_delta":
		delta, _ := mapField(event, "delta")
		if delta == nil {
			return "", "", false
		}
		deltaType, _ := stringField(delta, "type")
		if deltaType != "thinking_delta" {
			return "", "", false
		}
		thinking, _ := stringField(delta, "thinking")
		thinking = strings.TrimSpace(thinking)
		if thinking == "" {
			return "", "", false
		}
		return "thinking", thinking, true
	default:
		return "", "", false
	}
}

func claudeContentBlocks(event map[string]any) ([]any, bool) {
	if content, ok := sliceField(event, "content"); ok && len(content) > 0 {
		return content, true
	}
	message, _ := mapField(event, "message")
	if message == nil {
		return nil, false
	}
	content, ok := sliceField(message, "content")
	if !ok || len(content) == 0 {
		return nil, false
	}
	return content, true
}

func claudeTokenUsageFromEvent(event map[string]any) (agentTokenUsage, bool, bool) {
	eventType, _ := stringField(event, "type")
	message, _ := mapField(event, "message")

	if message != nil {
		if usageMap, _ := mapField(message, "usage"); usageMap != nil {
			if usage, ok := usageFromMap(usageMap); ok {
				return usage, eventType == "result", true
			}
		}
		if usage, ok := usageFromMap(message); ok {
			return usage, eventType == "result", true
		}
	}

	if usageMap, _ := mapField(event, "usage"); usageMap != nil {
		if usage, ok := usageFromMap(usageMap); ok {
			return usage, eventType == "result", true
		}
	}

	if usage, ok := usageFromMap(event); ok {
		return usage, eventType == "result", true
	}
	return agentTokenUsage{}, false, false
}

func usageFromMap(source map[string]any) (agentTokenUsage, bool) {
	if source == nil {
		return agentTokenUsage{}, false
	}
	_, hasInput := source["input_tokens"]
	_, hasOutput := source["output_tokens"]
	_, hasCached := source["cached_input_tokens"]
	_, hasCacheCreation := source["cache_creation_input_tokens"]
	_, hasCacheRead := source["cache_read_input_tokens"]
	_, hasIn := source["in"]
	_, hasOut := source["out"]
	_, hasCachedShort := source["cached"]
	_, hasInputShort := source["input"]
	_, hasOutputShort := source["output"]

	if !hasInput && !hasOutput && !hasCached && !hasCacheCreation && !hasCacheRead &&
		!hasIn && !hasOut && !hasCachedShort && !hasInputShort && !hasOutputShort {
		return agentTokenUsage{}, false
	}

	cached := intField(source, "cached_input_tokens")
	if cached == 0 {
		cached = intField(source, "cache_creation_input_tokens") + intField(source, "cache_read_input_tokens")
	}
	if cached == 0 {
		cached = intField(source, "cached")
	}

	in := intField(source, "input_tokens")
	if in == 0 {
		in = intField(source, "in")
	}
	if in == 0 {
		in = intField(source, "input")
	}

	out := intField(source, "output_tokens")
	if out == 0 {
		out = intField(source, "out")
	}
	if out == 0 {
		out = intField(source, "output")
	}

	return agentTokenUsage{
		In:     in,
		Out:    out,
		Cached: cached,
	}, true
}

func tokenUsagesFromTokensFields(event map[string]any) []agentTokenUsage {
	if event == nil {
		return nil
	}
	raw := findAllNestedFields(event, "tokens")
	if len(raw) == 0 {
		return nil
	}
	usage := make([]agentTokenUsage, 0, len(raw))
	for _, item := range raw {
		switch typed := item.(type) {
		case map[string]any:
			if parsed, ok := usageFromMap(typed); ok {
				usage = append(usage, parsed)
			}
		case []any:
			for _, nested := range typed {
				nestedMap, ok := nested.(map[string]any)
				if !ok {
					continue
				}
				if parsed, ok := usageFromMap(nestedMap); ok {
					usage = append(usage, parsed)
				}
			}
		}
	}
	return usage
}

func findAllNestedFields(value any, key string) []any {
	found := []any{}
	switch typed := value.(type) {
	case map[string]any:
		if direct, ok := typed[key]; ok {
			found = append(found, direct)
		}
		for _, nested := range typed {
			found = append(found, findAllNestedFields(nested, key)...)
		}
	case []any:
		for _, nested := range typed {
			found = append(found, findAllNestedFields(nested, key)...)
		}
	}
	return found
}

func toolEventTypeAndContent(name string, rawInput string) (string, string) {
	eventType := normalizeAgentEventType(name)
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

func normalizeAgentEventType(eventType string) string {
	eventType = strings.TrimSpace(eventType)
	if strings.EqualFold(eventType, "Bash") || strings.EqualFold(eventType, "exec_command") {
		return "Shell"
	}
	return eventType
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
	case "Bash", "Shell":
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
	suffix := formatAgentTokenTriplet(totals)
	base = strings.TrimSpace(base)
	if base == "" {
		return suffix
	}
	return base + " | " + suffix
}

func formatAgentTokenTriplet(usage agentTokenUsage) string {
	return fmt.Sprintf(
		"🢁%s 🢃%s 🢃🢃%s",
		formatTokenCount(usage.Out),
		formatTokenCount(usage.In),
		formatTokenCount(usage.Cached),
	)
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
	totalSeconds := int(elapsed.Round(time.Second).Seconds())
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
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
