package progress

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/executor/agent"
	"github.com/iw2rmb/amata/internal/state"
)

type StatusSymbolKind string

const (
	StatusSymbolRunning   StatusSymbolKind = "running"
	StatusSymbolSucceeded StatusSymbolKind = "succeeded"
	StatusSymbolFailed    StatusSymbolKind = "failed"
	StatusSymbolSkipped   StatusSymbolKind = "skipped"
)

type DescriptorData struct {
	PrimaryText         string   `json:"primary_text,omitempty"`
	DetailText          []string `json:"detail_text,omitempty"`
	FinalSummaryDetails []string `json:"final_summary_details,omitempty"`
}

type StepDescriptor struct {
	StatusSymbolKind    StatusSymbolKind
	Elapsed             time.Duration
	StepType            string
	PrimaryText         string
	DetailLines         []string
	FinalSummaryDetails []string
}

type DescriptorOptions struct {
	Now         time.Time
	DetailWidth int
}

func BuildStepDescriptor(step Step, options DescriptorOptions) StepDescriptor {
	data := cloneDescriptorData(step.Descriptor)
	if data == nil {
		data = &DescriptorData{}
	}
	if step.Error != nil && len(data.DetailText) == 0 {
		data.DetailText = []string{step.Error.Message}
	}

	detailLines := []string{}
	for _, text := range data.DetailText {
		detailLines = append(detailLines, wrapDescriptorText(text, options.DetailWidth)...)
	}

	return StepDescriptor{
		StatusSymbolKind:    symbolKindForStatus(step.Status),
		Elapsed:             elapsedForStep(step, options.Now),
		StepType:            step.Type,
		PrimaryText:         data.PrimaryText,
		DetailLines:         detailLines,
		FinalSummaryDetails: append([]string(nil), data.FinalSummaryDetails...),
	}
}

func StepFromContext(stepCtx executor.StepContext) (Step, error) {
	step := Step{
		Flow:   stepCtx.FlowName,
		Index:  stepCtx.StepIndex,
		ID:     stepCtx.Step.ID,
		Type:   stepCtx.Step.ExecutorType(),
		Status: StepStatusRunning,
	}

	data, err := descriptorDataFromContext(stepCtx)
	if err != nil {
		return Step{}, err
	}
	step.Descriptor = data
	return step, nil
}

func StepFromResultWithContext(flowName string, stepCtx executor.StepContext, result state.StepResult) (Step, error) {
	step := StepFromResult(flowName, result)
	data, err := descriptorDataFromResult(stepCtx, result)
	if err != nil {
		return Step{}, err
	}
	step.Descriptor = data
	return step, nil
}

func cloneDescriptorData(data *DescriptorData) *DescriptorData {
	if data == nil {
		return nil
	}

	cloned := &DescriptorData{
		PrimaryText: data.PrimaryText,
	}
	if len(data.DetailText) > 0 {
		cloned.DetailText = append([]string(nil), data.DetailText...)
	} else {
		cloned.DetailText = []string{}
	}
	if len(data.FinalSummaryDetails) > 0 {
		cloned.FinalSummaryDetails = append([]string(nil), data.FinalSummaryDetails...)
	} else {
		cloned.FinalSummaryDetails = []string{}
	}
	return cloned
}

func descriptorDataFromContext(stepCtx executor.StepContext) (*DescriptorData, error) {
	switch stepCtx.Step.ExecutorType() {
	case "call":
		flow, err := callFlow(stepCtx)
		if err != nil {
			return nil, err
		}
		return &DescriptorData{
			PrimaryText:         flow,
			FinalSummaryDetails: []string{flow},
		}, nil
	case "switch":
		caseCount, err := switchCaseCount(stepCtx)
		if err != nil {
			return nil, err
		}
		if caseCount == 0 {
			return &DescriptorData{}, nil
		}
		return &DescriptorData{
			PrimaryText:         fmt.Sprintf("%d cases", caseCount),
			FinalSummaryDetails: []string{fmt.Sprintf("%d cases", caseCount)},
		}, nil
	case "for_each":
		itemCount, err := forEachItemCount(stepCtx)
		if err != nil {
			return nil, err
		}
		return &DescriptorData{
			PrimaryText:         fmt.Sprintf("%d items", itemCount),
			FinalSummaryDetails: []string{fmt.Sprintf("%d items", itemCount)},
		}, nil
	case "codex", "claude":
		return agentDescriptor(stepCtx, stepCtx.Step.ExecutorType())
	case "shell":
		command, err := shellCommand(stepCtx)
		if err != nil {
			return nil, err
		}
		return &DescriptorData{PrimaryText: command}, nil
	case "assert":
		primary, err := resolvedDescriptorValue(stepCtx, stepCtx.Step.Fields["assert"])
		if err != nil {
			return nil, err
		}
		data := &DescriptorData{PrimaryText: primary}
		if raw, ok := stepCtx.Step.Fields["message"]; ok {
			message, err := stepCtx.Runtime.ResolveString(raw)
			if err != nil {
				return nil, err
			}
			data.DetailText = append(data.DetailText, message)
		}
		return data, nil
	case "git.inspect":
		cwd, err := resolvedCWD(stepCtx)
		if err != nil {
			return nil, err
		}
		return &DescriptorData{PrimaryText: cwd}, nil
	case "git.commit":
		message, err := stepCtx.Runtime.ResolveString(stepCtx.Step.Fields["message"])
		if err != nil {
			return nil, err
		}
		return &DescriptorData{DetailText: []string{message}}, nil
	default:
		return nil, nil
	}
}

func descriptorDataFromResult(stepCtx executor.StepContext, result state.StepResult) (*DescriptorData, error) {
	data, err := descriptorDataFromContext(stepCtx)
	if err != nil {
		return nil, err
	}
	if data == nil {
		data = &DescriptorData{}
	}

	switch stepCtx.Step.ExecutorType() {
	case "call":
		if flow, ok := stringField(result.Value, "flow"); ok && flow != "" {
			data.PrimaryText = flow
			data.FinalSummaryDetails = []string{flow}
		}
	case "switch":
		matched, _ := boolField(result.Value, "matched")
		caseIndex, hasCaseIndex := intField(result.Value, "case")
		switch {
		case matched && hasCaseIndex:
			summary := fmt.Sprintf("case %d", caseIndex)
			data.PrimaryText = summary
			data.FinalSummaryDetails = []string{summary}
		case !matched:
			data.PrimaryText = "no match"
			data.FinalSummaryDetails = []string{"no match"}
		}
	case "for_each":
		if count, ok := intField(result.Value, "count"); ok {
			summary := fmt.Sprintf("%d items", count)
			data.PrimaryText = summary
			data.FinalSummaryDetails = []string{summary}
		}
	case "shell":
		if exitCode, ok := intField(result.Value, "exitCode"); ok {
			data.FinalSummaryDetails = []string{fmt.Sprintf("exit %d", exitCode)}
		}
	case "assert":
		if result.Status == state.StepStatusSucceeded {
			data.FinalSummaryDetails = []string{"passed"}
		} else {
			data.FinalSummaryDetails = []string{"failed"}
		}
	case "git.inspect":
		data = gitInspectDescriptorFromResult(data, result.Value)
	case "git.commit":
		data = gitCommitDescriptorFromResult(data, result.Value)
	}

	return data, nil
}

func symbolKindForStatus(status StepStatus) StatusSymbolKind {
	switch status {
	case StepStatusSucceeded:
		return StatusSymbolSucceeded
	case StepStatusFailed:
		return StatusSymbolFailed
	case StepStatusSkipped:
		return StatusSymbolSkipped
	default:
		return StatusSymbolRunning
	}
}

func elapsedForStep(step Step, now time.Time) time.Duration {
	if step.StartedAt.IsZero() {
		return 0
	}
	finish := step.FinishedAt
	if finish.IsZero() {
		finish = now
	}
	if finish.IsZero() || finish.Before(step.StartedAt) {
		return 0
	}
	return finish.Sub(step.StartedAt)
}

func agentDescriptor(stepCtx executor.StepContext, providerName string) (*DescriptorData, error) {
	resolved, err := agent.ResolveStep(stepCtx, providerName)
	if err != nil {
		return nil, fmt.Errorf("%s step: %s", providerName, err.Message)
	}

	primary := formatAgentModelReasoning(resolved.Model, resolved.Reasoning)
	data := &DescriptorData{
		PrimaryText: primary,
		DetailText:  []string{resolved.Prompt},
	}
	data.FinalSummaryDetails = nonEmptyStrings(resolved.Model, resolved.Reasoning)
	return data, nil
}

func formatAgentModelReasoning(model string, reasoning string) string {
	model = strings.TrimSpace(model)
	reasoning = strings.TrimSpace(reasoning)
	switch {
	case model == "":
		return reasoning
	case reasoning == "":
		return model
	default:
		return model + ":" + reasoning
	}
}

func callFlow(stepCtx executor.StepContext) (string, error) {
	value, ok := stepCtx.Step.Fields["flow"]
	if !ok {
		return "", fmt.Errorf("flow is required")
	}
	return stepCtx.Runtime.ResolveString(value)
}

func switchCaseCount(stepCtx executor.StepContext) (int, error) {
	value, ok := stepCtx.Step.Fields["cases"]
	if !ok {
		return 0, fmt.Errorf("cases is required")
	}
	switch typed := value.(type) {
	case []any:
		return len(typed), nil
	case []map[string]any:
		return len(typed), nil
	default:
		return 0, fmt.Errorf("cases must be an array")
	}
}

func forEachItemCount(stepCtx executor.StepContext) (int, error) {
	value, ok := stepCtx.Step.Fields["items"]
	if !ok {
		return 0, fmt.Errorf("for_each step is missing items")
	}

	resolved, err := stepCtx.Runtime.Resolve(value)
	if err != nil {
		return 0, err
	}

	switch typed := resolved.(type) {
	case []any:
		return len(typed), nil
	case []string:
		return len(typed), nil
	default:
		return 0, fmt.Errorf("for_each items must resolve to an array")
	}
}

func shellCommand(stepCtx executor.StepContext) (string, error) {
	value, ok := stepCtx.Step.Fields["command"]
	if !ok {
		return "", fmt.Errorf("command is required")
	}

	switch command := value.(type) {
	case string:
		resolved, err := stepCtx.Runtime.ResolveString(command)
		if err != nil {
			return "", err
		}
		return renderCommand([]string{"sh", "-lc", resolved}), nil
	case []any:
		parts := make([]string, 0, len(command))
		for index, part := range command {
			text, err := stepCtx.Runtime.ResolveString(part)
			if err != nil {
				return "", fmt.Errorf("command[%d]: %w", index, err)
			}
			parts = append(parts, text)
		}
		return renderCommand(parts), nil
	default:
		return "", fmt.Errorf("command must be a string or string array")
	}
}

func resolvedDescriptorValue(stepCtx executor.StepContext, value any) (string, error) {
	resolved, err := stepCtx.Runtime.Resolve(value)
	if err != nil {
		return "", err
	}
	if text, ok := resolved.(string); ok {
		return text, nil
	}
	encoded, err := json.Marshal(resolved)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func resolvedCWD(stepCtx executor.StepContext) (string, error) {
	value, ok := stepCtx.Step.Fields["cwd"]
	if !ok {
		return stepCtx.Workspace.Root, nil
	}

	text, err := stepCtx.Runtime.ResolveString(value)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(text) {
		return filepath.Clean(text), nil
	}
	return filepath.Clean(filepath.Join(stepCtx.Workspace.Root, text)), nil
}

func gitInspectDescriptorFromResult(data *DescriptorData, value any) *DescriptorData {
	if data.PrimaryText != "" {
		data.DetailText = append([]string{data.PrimaryText}, data.DetailText...)
	}

	isRepo, _ := boolField(value, "isRepo")
	hasDiff, _ := boolField(value, "hasDiff")
	files := stringSliceField(value, "files")

	switch {
	case !isRepo:
		data.PrimaryText = "not a repository"
		data.FinalSummaryDetails = []string{"not repo"}
	case hasDiff:
		data.PrimaryText = fmt.Sprintf("dirty %d files", len(files))
		data.FinalSummaryDetails = []string{fmt.Sprintf("%d files", len(files))}
	default:
		data.PrimaryText = "clean"
		data.FinalSummaryDetails = []string{"clean"}
	}

	if len(files) > 0 {
		data.DetailText = append(data.DetailText, strings.Join(files, "\n"))
	}

	return data
}

func gitCommitDescriptorFromResult(data *DescriptorData, value any) *DescriptorData {
	committed, _ := boolField(value, "committed")
	metadataValue, ok := mapField(value, "metadata")
	if !committed || !ok {
		data.PrimaryText = "no changes"
		data.FinalSummaryDetails = []string{"no changes"}
		return data
	}

	shortCommit, _ := stringField(metadataValue, "shortCommit")
	changedFiles, _ := intField(metadataValue, "changedFileCount")
	insertions, _ := intField(metadataValue, "insertions")
	deletions, _ := intField(metadataValue, "deletions")

	data.PrimaryText = fmt.Sprintf("%s files %d +%d -%d", shortCommit, changedFiles, insertions, deletions)
	data.FinalSummaryDetails = []string{
		shortCommit,
		fmt.Sprintf("files %d +%d -%d", changedFiles, insertions, deletions),
	}

	for _, file := range fileStats(metadataValue) {
		data.DetailText = append(data.DetailText, fmt.Sprintf("+%d -%d %s", file.Insertions, file.Deletions, file.Path))
	}

	return data
}

type commitFileDescriptor struct {
	Path       string
	Insertions int
	Deletions  int
}

func fileStats(value any) []commitFileDescriptor {
	rawFiles, ok := sliceField(value, "files")
	if !ok {
		return nil
	}

	files := make([]commitFileDescriptor, 0, len(rawFiles))
	for _, rawFile := range rawFiles {
		path, ok := stringField(rawFile, "path")
		if !ok || path == "" {
			continue
		}
		insertions, _ := intField(rawFile, "insertions")
		deletions, _ := intField(rawFile, "deletions")
		files = append(files, commitFileDescriptor{
			Path:       path,
			Insertions: insertions,
			Deletions:  deletions,
		})
	}
	return files
}

func wrapDescriptorText(text string, width int) []string {
	if text == "" {
		return []string{}
	}

	lines := []string{}
	for _, rawLine := range strings.Split(text, "\n") {
		if rawLine == "" {
			lines = append(lines, "")
			continue
		}
		if width <= 0 {
			lines = append(lines, rawLine)
			continue
		}

		words := strings.Fields(rawLine)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}

		current := words[0]
		for _, word := range words[1:] {
			if len(current)+1+len(word) <= width {
				current += " " + word
				continue
			}
			lines = append(lines, current)
			current = word
		}
		lines = append(lines, current)
	}
	return lines
}

func renderCommand(parts []string) string {
	if len(parts) == 0 {
		return ""
	}

	rendered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || strings.ContainsAny(part, " \t\n\"'\\") {
			rendered = append(rendered, strconv.Quote(part))
			continue
		}
		rendered = append(rendered, part)
	}
	return strings.Join(rendered, " ")
}

func nonEmptyStrings(values ...string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}

func mapField(value any, key string) (map[string]any, bool) {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	raw, ok := fields[key]
	if !ok {
		return nil, false
	}
	current, ok := raw.(map[string]any)
	return current, ok
}

func sliceField(value any, key string) ([]any, bool) {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	raw, ok := fields[key]
	if !ok {
		return nil, false
	}
	current, ok := raw.([]any)
	return current, ok
}

func stringField(value any, key string) (string, bool) {
	fields, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	raw, ok := fields[key]
	if !ok {
		return "", false
	}
	text, ok := raw.(string)
	return text, ok
}

func boolField(value any, key string) (bool, bool) {
	fields, ok := value.(map[string]any)
	if !ok {
		return false, false
	}
	raw, ok := fields[key]
	if !ok {
		return false, false
	}
	current, ok := raw.(bool)
	return current, ok
}

func intField(value any, key string) (int, bool) {
	fields, ok := value.(map[string]any)
	if !ok {
		return 0, false
	}
	raw, ok := fields[key]
	if !ok {
		return 0, false
	}

	switch current := raw.(type) {
	case int:
		return current, true
	case int32:
		return int(current), true
	case int64:
		return int(current), true
	case float64:
		return int(current), true
	default:
		return 0, false
	}
}

func stringSliceField(value any, key string) []string {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := fields[key]
	if !ok {
		return nil
	}

	switch current := raw.(type) {
	case []string:
		return append([]string(nil), current...)
	case []any:
		values := make([]string, 0, len(current))
		for _, item := range current {
			text, ok := item.(string)
			if !ok {
				continue
			}
			values = append(values, text)
		}
		return values
	default:
		return nil
	}
}
