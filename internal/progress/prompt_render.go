package progress

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	glamour "charm.land/glamour/v2"
	glamourstyles "charm.land/glamour/v2/styles"
	lipgloss "charm.land/lipgloss/v2"
	charmansi "github.com/charmbracelet/x/ansi"
)

const (
	agentPromptMaxWidth    = 80
	agentPromptLeftPadding = 1
	codexRowMaxWidth       = 80
)

func renderAgentPromptDetails(step Step, descriptor StepDescriptor, options renderStepOptions) []string {
	data := cloneDescriptorData(step.Descriptor)
	showLastAction := step.Status == StepStatusRunning
	wrap := agentPromptWordWrap(options)
	if data == nil || len(data.DetailText) == 0 {
		return appendAgentLastActionDetails(
			step,
			options.now,
			descriptor.DetailLines,
			options.agentOutput,
			showLastAction,
			wrap,
			options.styles.colorize,
		)
	}

	rendered, err := renderAgentPromptMarkdown(data.DetailText[0], wrap)
	if err != nil {
		return descriptor.DetailLines
	}
	if options.styles.colorize {
		return appendAgentLastActionDetails(
			step,
			options.now,
			rendered,
			options.agentOutput,
			showLastAction,
			wrap,
			true,
		)
	}

	plain := make([]string, 0, len(rendered))
	for _, line := range rendered {
		plain = append(plain, strings.TrimRight(charmansi.Strip(line), " "))
	}
	return appendAgentLastActionDetails(
		step,
		options.now,
		plain,
		options.agentOutput,
		showLastAction,
		wrap,
		false,
	)
}

func renderRunningCodexDetails(step Step, options renderStepOptions) []string {
	summary := options.agentOutput
	wrap := agentPromptWordWrap(options)

	promptPath := ""
	if step.Artifacts.Files != nil {
		promptPath = strings.TrimSpace(step.Artifacts.Files["prompt"])
	}
	if promptPath == "" {
		promptPath = "prompt.md"
	}
	displayPromptPath := promptPathDisplay(promptPath, workspaceRootForStep(step))

	promptCollapsed := " [P]rompt " + renderAgentInlineText(displayPromptPath, wrap, options.styles.colorize)
	thinkingText := "(none yet)"
	shellText := "(none yet)"
	if summary != nil {
		if text := strings.TrimSpace(summary.Thinking); text != "" {
			thinkingText = text
		}
		if text := strings.TrimSpace(summary.Shell); text != "" {
			shellText = text
		}
	}

	thinkingPrefix := " [T]hinking "
	shellPrefix := " [S]hell "
	thinkingCollapsed := thinkingPrefix + renderAgentInlineText(
		truncateWithEllipsis(thinkingText, codexRowMaxWidth-lipgloss.Width(thinkingPrefix)),
		wrap,
		options.styles.colorize,
	)
	shellCollapsed := shellPrefix + renderAgentInlineText(
		truncateWithEllipsis(shellText, codexRowMaxWidth-lipgloss.Width(shellPrefix)),
		wrap,
		options.styles.colorize,
	)

	lines := []string{""}
	if options.promptExpanded {
		lines = append(lines, " [P]rompt")
		lines = append(lines, "")
		renderedPrompt := renderAgentEventContentMarkdown(promptPathContent(step, promptPath), wrap, options.styles.colorize)
		if len(renderedPrompt) == 0 {
			lines = append(lines, "  "+promptPathContent(step, promptPath))
		} else {
			for _, line := range renderedPrompt {
				lines = append(lines, "  "+line)
			}
		}
		lines = append(lines, "")
	} else {
		lines = append(lines, promptCollapsed)
	}

	if options.thinkingExpanded {
		lines = append(lines, " [T]hinking")
		lines = append(lines, "")
		expanded := renderAgentEventContentMarkdown("> "+thinkingText, wrap, options.styles.colorize)
		for _, line := range expanded {
			lines = append(lines, "  "+line)
		}
		lines = append(lines, "")
	} else {
		lines = append(lines, thinkingCollapsed)
	}

	if options.shellExpanded {
		lines = append(lines, " [S]hell")
		lines = append(lines, "")
		expanded := renderAgentEventContentMarkdown(shellText, wrap, options.styles.colorize)
		for _, line := range expanded {
			lines = append(lines, "  "+line)
		}
		lines = append(lines, "")
	} else {
		lines = append(lines, shellCollapsed)
	}
	return lines
}

func appendAgentLastActionDetails(
	step Step,
	now time.Time,
	lines []string,
	summary *agentOutputSummary,
	enabled bool,
	wrap int,
	colorize bool,
) []string {
	if !enabled || summary == nil || summary.LastAction == nil {
		return lines
	}

	last := summary.LastAction
	eventType := strings.TrimSpace(last.EventType)
	if eventType == "" {
		eventType = "event"
	}
	action := eventType
	if !isZeroUsage(last.Tokens) {
		action = fmt.Sprintf("%s | %s", eventType, formatAgentTokenTriplet(last.Tokens))
	}

	content := strings.TrimSpace(last.Content)
	if content == "" {
		content = "(no content)"
	}
	if last.Italic {
		content = "*" + content + "*"
	}
	renderedContent := renderAgentEventContentMarkdown(content, wrap, colorize)

	details := append([]string{}, lines...)
	details = append(details, "")
	details = append(details, action)
	details = append(details, "")
	details = append(details, renderedContent...)
	return details
}

func renderAgentEventContentMarkdown(content string, wrap int, colorize bool) []string {
	rendered, err := renderAgentPromptMarkdown(content, wrap)
	if err != nil {
		return []string{content}
	}
	rendered = trimEmptyMarkdownEdges(rendered)
	if colorize {
		return rendered
	}
	plain := make([]string, 0, len(rendered))
	for _, line := range rendered {
		plain = append(plain, strings.TrimRight(charmansi.Strip(line), " "))
	}
	return plain
}

func trimEmptyMarkdownEdges(lines []string) []string {
	start := 0
	for start < len(lines) && isVisiblyEmpty(lines[start]) {
		start++
	}
	end := len(lines)
	for end > start && isVisiblyEmpty(lines[end-1]) {
		end--
	}
	if start >= end {
		return []string{}
	}
	return lines[start:end]
}

func isVisiblyEmpty(line string) bool {
	return strings.TrimSpace(charmansi.Strip(line)) == ""
}

func renderAgentInlineText(content string, wrap int, colorize bool) string {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStylePath(glamourstyles.DraculaStyle),
		glamour.WithWordWrap(max(wrap, 1)),
		glamour.WithPreservedNewLines(),
	)
	if err != nil {
		return strings.TrimSpace(content)
	}
	rendered, err := renderer.Render(content)
	if err != nil {
		return strings.TrimSpace(content)
	}
	rendered = strings.TrimRight(rendered, "\n")
	lines := strings.Split(rendered, "\n")
	for _, line := range lines {
		plain := strings.TrimSpace(charmansi.Strip(line))
		if plain == "" {
			continue
		}
		if colorize {
			return strings.TrimSpace(line)
		}
		return plain
	}
	return strings.TrimSpace(content)
}

func truncateWithEllipsis(value string, maxWidth int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= maxWidth {
		return value
	}
	if maxWidth <= 3 {
		return strings.Repeat(".", maxWidth)
	}
	limit := maxWidth - 3
	runes := []rune(value)
	end := 0
	for index := range runes {
		candidate := string(runes[:index+1])
		if lipgloss.Width(candidate) > limit {
			break
		}
		end = index + 1
	}
	if end == 0 {
		return "..."
	}
	return string(runes[:end]) + "..."
}

func promptPathContent(step Step, fallback string) string {
	path := fallback
	if step.Artifacts.Files != nil {
		if current := strings.TrimSpace(step.Artifacts.Files["prompt"]); current != "" {
			path = current
		}
	}
	data, _, ok := readArtifactFile(path)
	if !ok {
		return path
	}
	text := strings.TrimRight(string(data), "\n")
	if strings.TrimSpace(text) == "" {
		return path
	}
	return text
}

func workspaceRootForStep(step Step) string {
	if step.Descriptor == nil {
		return ""
	}
	return strings.TrimSpace(step.Descriptor.WorkspaceRoot)
}

func promptPathDisplay(promptPath string, workspaceRoot string) string {
	promptPath = strings.TrimSpace(promptPath)
	if promptPath == "" {
		return promptPath
	}
	if !filepath.IsAbs(promptPath) {
		return filepath.ToSlash(filepath.Clean(promptPath))
	}
	candidates := []string{}
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot != "" {
		if relative, ok := relativeToWorkspace(promptPath, workspaceRoot); ok {
			candidates = append(candidates, relative)
		}
	}
	if homeVariant, ok := homeTildeVariant(promptPath); ok {
		candidates = append(candidates, homeVariant)
	}
	if len(candidates) == 0 {
		return promptPath
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if lipgloss.Width(candidate) < lipgloss.Width(best) {
			best = candidate
		}
	}
	return best
}

func relativeToWorkspace(promptPath string, workspaceRoot string) (string, bool) {
	absPrompt, err := filepath.Abs(promptPath)
	if err != nil {
		return "", false
	}
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(absRoot, absPrompt)
	if err != nil || relative == "." {
		return "", false
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.Clean(relative), true
}

func homeTildeVariant(promptPath string) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", false
	}
	absPrompt, err := filepath.Abs(promptPath)
	if err != nil {
		return "", false
	}
	absHome, err := filepath.Abs(home)
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(absHome, absPrompt)
	if err != nil {
		return "", false
	}
	if relative == "." {
		return "~", true
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(filepath.Join("~", relative)), true
}

func renderAgentPromptMarkdown(markdown string, wrap int) ([]string, error) {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStylePath(glamourstyles.DraculaStyle),
		glamour.WithWordWrap(max(wrap, 1)),
		glamour.WithPreservedNewLines(),
	)
	if err != nil {
		return nil, err
	}

	rendered, err := renderer.Render(markdown)
	if err != nil {
		return nil, err
	}
	return padAgentPromptLines(rendered), nil
}

func agentPromptWordWrap(options renderStepOptions) int {
	return max(1, min(detailWidth(options.width, options.styles), agentPromptMaxWidth)-agentPromptLeftPadding)
}

func padAgentPromptLines(rendered string) []string {
	rendered = strings.Trim(rendered, "\n")
	if rendered == "" {
		return nil
	}

	source := strings.Split(rendered, "\n")
	lines := make([]string, 0, len(source)+1)
	lines = append(lines, "")
	for _, line := range source {
		lines = append(lines, " "+line)
	}
	return lines
}
