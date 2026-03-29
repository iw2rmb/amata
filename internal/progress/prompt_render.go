package progress

import (
	"fmt"
	"strings"
	"time"

	glamour "charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	glamourstyles "charm.land/glamour/v2/styles"
	charmansi "github.com/charmbracelet/x/ansi"
)

const (
	agentPromptMaxWidth    = 80
	agentPromptLeftPadding = 1
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
	// renderAgentPromptMarkdown prepends an empty line so prompt blocks start
	// with vertical spacing; event content is already spaced by caller.
	if len(rendered) > 0 && strings.TrimSpace(rendered[0]) == "" {
		rendered = rendered[1:]
	}
	if colorize {
		return rendered
	}
	plain := make([]string, 0, len(rendered))
	for _, line := range rendered {
		plain = append(plain, strings.TrimRight(charmansi.Strip(line), " "))
	}
	return plain
}

func renderAgentPromptMarkdown(markdown string, wrap int) ([]string, error) {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(agentPromptStyle()),
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
	rendered = strings.TrimRight(rendered, "\n")
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

func agentPromptStyle() ansi.StyleConfig {
	style := glamourstyles.ASCIIStyleConfig
	white := "255"
	dimWhite := "252"

	style.Document.BlockPrefix = ""
	style.Document.BlockSuffix = ""
	style.Document.Margin = nil
	style.CodeBlock.Margin = nil

	style.Document.Color = &dimWhite
	style.Paragraph.Color = &dimWhite
	style.Text.Color = &dimWhite

	style.Heading.Color = &dimWhite
	style.H1.Color = &dimWhite
	style.H2.Color = &dimWhite
	style.H3.Color = &dimWhite
	style.H4.Color = &dimWhite
	style.H5.Color = &dimWhite
	style.H6.Color = &dimWhite

	style.Link.Color = &dimWhite
	style.LinkText.Color = &dimWhite
	style.Image.Color = &dimWhite
	style.ImageText.Color = &dimWhite
	style.Code.Color = &white
	style.CodeBlock.Color = &white
	style.CodeBlock.Chroma = monochromeChroma("#ffffff")

	return style
}

func monochromeChroma(color string) *ansi.Chroma {
	return &ansi.Chroma{
		Text:                ansi.StylePrimitive{Color: &color},
		Error:               ansi.StylePrimitive{Color: &color},
		Comment:             ansi.StylePrimitive{Color: &color},
		CommentPreproc:      ansi.StylePrimitive{Color: &color},
		Keyword:             ansi.StylePrimitive{Color: &color},
		KeywordReserved:     ansi.StylePrimitive{Color: &color},
		KeywordNamespace:    ansi.StylePrimitive{Color: &color},
		KeywordType:         ansi.StylePrimitive{Color: &color},
		Operator:            ansi.StylePrimitive{Color: &color},
		Punctuation:         ansi.StylePrimitive{Color: &color},
		Name:                ansi.StylePrimitive{Color: &color},
		NameBuiltin:         ansi.StylePrimitive{Color: &color},
		NameTag:             ansi.StylePrimitive{Color: &color},
		NameAttribute:       ansi.StylePrimitive{Color: &color},
		NameClass:           ansi.StylePrimitive{Color: &color},
		NameConstant:        ansi.StylePrimitive{Color: &color},
		NameDecorator:       ansi.StylePrimitive{Color: &color},
		NameException:       ansi.StylePrimitive{Color: &color},
		NameFunction:        ansi.StylePrimitive{Color: &color},
		NameOther:           ansi.StylePrimitive{Color: &color},
		Literal:             ansi.StylePrimitive{Color: &color},
		LiteralNumber:       ansi.StylePrimitive{Color: &color},
		LiteralDate:         ansi.StylePrimitive{Color: &color},
		LiteralString:       ansi.StylePrimitive{Color: &color},
		LiteralStringEscape: ansi.StylePrimitive{Color: &color},
		GenericDeleted:      ansi.StylePrimitive{Color: &color},
		GenericEmph:         ansi.StylePrimitive{Color: &color},
		GenericInserted:     ansi.StylePrimitive{Color: &color},
		GenericStrong:       ansi.StylePrimitive{Color: &color},
		GenericSubheading:   ansi.StylePrimitive{Color: &color},
		Background:          ansi.StylePrimitive{Color: &color},
	}
}
