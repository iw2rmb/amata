package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

const defaultStreamWidth = 80

type StreamController struct {
	renderer streamRenderer
}

type streamRenderer interface {
	WriteProgress(Event)
	Close() error
}

type streamControllerOptions struct {
	forceTTY *bool
	now      func() time.Time
	width    int
}

type streamRenderSettings struct {
	now   func() time.Time
	width int
}

type plainStreamRenderer struct {
	writer   io.Writer
	settings streamRenderSettings
}

type teaStreamRenderer struct {
	program *tea.Program
	done    chan error
}

type streamModel struct {
	spinner          spinner.Model
	styles           streamStyles
	settings         streamRenderSettings
	history          []string
	active           []Step
	width            int
	promptExpanded   bool
	thinkingExpanded bool
	shellExpanded    bool
}

type streamStyles struct {
	colorize    bool
	detail      lipgloss.Style
	diffPlus    lipgloss.Style
	diffMinus   lipgloss.Style
	strong      lipgloss.Style
	pathAdded   lipgloss.Style
	pathDeleted lipgloss.Style
	statusOK    lipgloss.Style
	statusFail  lipgloss.Style
}

type progressEventMsg struct {
	event Event
}

func NewStreamController(writer io.Writer) (*StreamController, error) {
	return newStreamController(writer, streamControllerOptions{
		now: currentUTC,
	})
}

func newStreamController(writer io.Writer, options streamControllerOptions) (*StreamController, error) {
	if writer == nil {
		return nil, nil
	}

	settings := streamRenderSettings{
		now:   options.now,
		width: options.width,
	}
	if settings.now == nil {
		settings.now = currentUTC
	}
	if settings.width <= 0 {
		settings.width = defaultStreamWidth
	}

	isTTY := false
	if options.forceTTY != nil {
		isTTY = *options.forceTTY
	} else if fd, ok := writerFD(writer); ok {
		isTTY = term.IsTerminal(fd)
		if isTTY {
			if width, _, err := term.GetSize(fd); err == nil && width > 0 {
				settings.width = width
			}
		}
	}

	if !isTTY {
		return &StreamController{
			renderer: &plainStreamRenderer{
				writer:   writer,
				settings: settings,
			},
		}, nil
	}

	renderer, err := newTeaStreamRenderer(writer, settings)
	if err != nil {
		return nil, err
	}
	return &StreamController{renderer: renderer}, nil
}

func (c *StreamController) WriteProgress(event Event) {
	if c == nil || c.renderer == nil {
		return
	}
	c.renderer.WriteProgress(event)
}

func (c *StreamController) Close() error {
	if c == nil || c.renderer == nil {
		return nil
	}
	return c.renderer.Close()
}

func newTeaStreamRenderer(writer io.Writer, settings streamRenderSettings) (*teaStreamRenderer, error) {
	model := streamModel{
		spinner:  spinner.New(spinner.WithSpinner(spinner.Dot)),
		styles:   newStreamStyles(true),
		settings: settings,
		history:  []string{},
		width:    settings.width,
		active:   []Step{},
	}

	program := tea.NewProgram(
		model,
		tea.WithOutput(writer),
		tea.WithoutSignalHandler(),
	)
	renderer := &teaStreamRenderer{
		program: program,
		done:    make(chan error, 1),
	}
	go func() {
		_, err := program.Run()
		renderer.done <- err
	}()
	return renderer, nil
}

func (r *teaStreamRenderer) WriteProgress(event Event) {
	r.program.Send(progressEventMsg{event: event})
}

func (r *teaStreamRenderer) Close() error {
	if r == nil || r.program == nil {
		return nil
	}
	r.program.Quit()
	return <-r.done
}

func (r *plainStreamRenderer) WriteProgress(event Event) {
	block := blockForEvent(event, r.settings, newStreamStyles(false))
	if block == "" {
		return
	}
	_, _ = fmt.Fprintf(r.writer, "\n%s\n", block)
}

func (r *plainStreamRenderer) Close() error {
	return nil
}

func (m streamModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m streamModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case spinner.TickMsg:
		if len(m.active) == 0 {
			return m, nil
		}
		nextSpinner, cmd := m.spinner.Update(msg)
		m.spinner = nextSpinner
		return m, cmd
	case tea.WindowSizeMsg:
		if typed.Width > 0 {
			m.width = typed.Width
		}
		return m, nil
	case tea.KeyMsg:
		if typed.Type == tea.KeyCtrlC {
			interruptFn()
			return m, tea.Quit
		}
		switch strings.ToLower(strings.TrimSpace(typed.String())) {
		case "p":
			m.promptExpanded = !m.promptExpanded
		case "t":
			m.thinkingExpanded = !m.thinkingExpanded
		case "s":
			m.shellExpanded = !m.shellExpanded
		}
		return m, nil
	case progressEventMsg:
		return m.applyEvent(typed.event)
	default:
		return m, nil
	}
}

func (m streamModel) View() string {
	visibleActive := visibleActiveSteps(m.active)
	blocks := make([]string, 0, len(m.history)+len(visibleActive))
	blocks = append(blocks, m.history...)
	for index, step := range visibleActive {
		statusToken := "⏺"
		if index == len(visibleActive)-1 {
			statusToken = m.spinner.View()
		}
		blocks = append(blocks, renderStepBlock(step, renderStepOptions{
			statusToken:      statusToken,
			now:              m.settings.now(),
			width:            resolvedWidth(m.width),
			styles:           m.styles,
			promptExpanded:   m.promptExpanded,
			thinkingExpanded: m.thinkingExpanded,
			shellExpanded:    m.shellExpanded,
		}))
	}
	if len(blocks) == 0 {
		return ""
	}
	return renderProgressBlocks(blocks)
}

func (m streamModel) applyEvent(event Event) (streamModel, tea.Cmd) {
	switch event.Kind {
	case EventRunResumed:
		m.active = cloneSteps(event.Snapshot.Active)
		if len(m.active) == 0 {
			return m, nil
		}
		return m, m.spinner.Tick
	case EventStepStarted:
		if event.Step == nil {
			return m, nil
		}
		m.active = append(m.active, cloneStep(*event.Step))
		if len(m.active) == 1 {
			return m, m.spinner.Tick
		}
		return m, nil
	case EventStepFinished:
		if event.Step == nil {
			return m, nil
		}
		step := cloneStep(*event.Step)
		if index := findActiveStep(m.active, step); index >= 0 {
			m.active = append(m.active[:index], m.active[index+1:]...)
		}
		if shouldRenderFinishedStep(step, event.Snapshot.Active) {
			m.history = append(m.history, renderStepBlock(step, renderStepOptions{
				statusToken: statusTokenForStep(step),
				now:         step.FinishedAt,
				width:       resolvedWidth(m.width),
				styles:      m.styles,
			}))
		}
		return m, nil
	case EventRunFinished:
		block := blockForEvent(event, m.settings.withWidth(resolvedWidth(m.width)), m.styles)
		if block == "" {
			return m, nil
		}
		m.history = append(m.history, block)
		return m, nil
	default:
		return m, nil
	}
}

func (s streamRenderSettings) withWidth(width int) streamRenderSettings {
	s.width = width
	return s
}

func newStreamStyles(colorize bool) streamStyles {
	return streamStyles{
		colorize:    colorize,
		detail:      lipgloss.NewStyle().PaddingLeft(2),
		diffPlus:    lipgloss.NewStyle().Foreground(lipgloss.Color("#98c379")),
		diffMinus:   lipgloss.NewStyle().Foreground(lipgloss.Color("#e06c75")),
		strong:      lipgloss.NewStyle().Bold(true),
		pathAdded:   lipgloss.NewStyle().Foreground(lipgloss.Color("#98c379")),
		pathDeleted: lipgloss.NewStyle().Foreground(lipgloss.Color("#e06c75")),
		statusOK:    lipgloss.NewStyle().Foreground(lipgloss.Color("#98c379")),
		statusFail:  lipgloss.NewStyle().Foreground(lipgloss.Color("#e06c75")),
	}
}

type renderStepOptions struct {
	statusToken      string
	now              time.Time
	width            int
	styles           streamStyles
	agentOutput      *agentOutputSummary
	promptExpanded   bool
	thinkingExpanded bool
	shellExpanded    bool
}

func blockForEvent(event Event, settings streamRenderSettings, styles streamStyles) string {
	switch event.Kind {
	case EventRunResumed:
		if len(event.Snapshot.Active) == 0 {
			return ""
		}

		visibleActive := visibleActiveSteps(event.Snapshot.Active)
		blocks := make([]string, 0, len(visibleActive))
		for _, step := range visibleActive {
			blocks = append(blocks, renderStepBlock(step, renderStepOptions{
				statusToken: "⏺",
				now:         settings.now(),
				width:       resolvedWidth(settings.width),
				styles:      styles,
			}))
		}
		return strings.Join(blocks, "\n")
	case EventStepStarted:
		if event.Step == nil {
			return ""
		}
		if !shouldRenderStartedStep(*event.Step, event.Snapshot.Active) {
			return ""
		}
		return renderStepBlock(*event.Step, renderStepOptions{
			statusToken: "⏺",
			now:         settings.now(),
			width:       resolvedWidth(settings.width),
			styles:      styles,
		})
	case EventStepFinished:
		if event.Step == nil {
			return ""
		}
		if !shouldRenderFinishedStep(*event.Step, event.Snapshot.Active) {
			return ""
		}
		return renderStepBlock(*event.Step, renderStepOptions{
			statusToken: statusTokenForStep(*event.Step),
			now:         event.Step.FinishedAt,
			width:       resolvedWidth(settings.width),
			styles:      styles,
		})
	case EventRunFinished:
		if event.Failure == nil {
			return ""
		}
		return renderFailureBlock(event.Failure, settings, styles)
	default:
		return ""
	}
}

func renderFailureBlock(failure *Failure, settings streamRenderSettings, styles streamStyles) string {
	if failure == nil {
		return ""
	}

	prefix := "⏺"
	if styles.colorize {
		prefix = styles.statusFail.Render(prefix)
	}
	lines := wrapWithPrefix(
		fmt.Sprintf("%s %s run", prefix, formatElapsed(0)),
		failure.Message,
		resolvedWidth(settings.width),
		styles.detail,
	)
	return strings.Join(lines, "\n")
}

func renderStepBlock(step Step, options renderStepOptions) string {
	descriptor := BuildStepDescriptor(step, DescriptorOptions{
		Now:         options.now,
		DetailWidth: detailWidth(options.width, options.styles),
	})
	if isAgentStepType(step.Type) {
		if summary, ok := summarizeAgentStepOutput(step); ok {
			options.agentOutput = &summary
			if !summaryHasNoTokens(summary) {
				descriptor.PrimaryText = formatAgentTokenSummary(descriptor.PrimaryText, summary.Totals)
			}
		}
	}

	headlinePrefix := strings.TrimSpace(strings.Join([]string{
		renderStatusToken(step, options.statusToken, options.styles),
		formatElapsed(descriptor.Elapsed),
	}, " "))
	if !options.styles.colorize {
		headlinePrefix = strings.TrimSpace(strings.Join([]string{headlinePrefix, descriptor.StepType}, " "))
	}
	lines := renderStepHeadline(step, descriptor, headlinePrefix, options)
	for _, detail := range renderStepDetails(step, descriptor, options) {
		lines = append(lines, options.styles.detail.Render(detail))
	}
	return strings.Join(lines, "\n")
}

func renderStepHeadline(step Step, descriptor StepDescriptor, prefix string, options renderStepOptions) []string {
	if !options.styles.colorize {
		if step.Type != "git.commit" {
			return wrapWithPrefix(prefix, descriptor.PrimaryText, options.width, options.styles.detail)
		}

		shortCommit, _, _, _, _, ok := gitCommitRenderData(step)
		if !ok {
			return wrapWithPrefix(prefix, descriptor.PrimaryText, options.width, options.styles.detail)
		}

		message := gitCommitMessage(step)
		if message == "" {
			return wrapWithPrefix(prefix, shortCommit, options.width, options.styles.detail)
		}
		return wrapWithPrefix(prefix, shortCommit+" "+message, options.width, options.styles.detail)
	}

	if step.Type != "git.commit" {
		return renderHeadlineWithStepType(prefix, descriptor.StepType, descriptor.PrimaryText, options)
	}

	shortCommit, _, _, _, _, ok := gitCommitRenderData(step)
	if !ok {
		return renderHeadlineWithStepType(prefix, descriptor.StepType, descriptor.PrimaryText, options)
	}

	message := gitCommitMessage(step)
	if message == "" {
		return renderHeadlineWithStepType(prefix, descriptor.StepType, shortCommit, options)
	}

	words := []styledWord{renderStepTypeWord(descriptor.StepType, options.styles), {text: shortCommit}}
	words = append(words, styledWords(message, func(text string) string {
		return options.styles.strong.Render(text)
	})...)
	return wrapStyledWordsWithPrefix(prefix, words, options.width, options.styles.detail)
}

func renderHeadlineWithStepType(prefix string, stepType string, primaryText string, options renderStepOptions) []string {
	if !options.styles.colorize {
		return wrapWithPrefix(prefix, strings.TrimSpace(strings.Join(nonEmptyStrings(stepType, primaryText), " ")), options.width, options.styles.detail)
	}

	words := []styledWord{renderStepTypeWord(stepType, options.styles)}
	words = append(words, styledWords(primaryText, nil)...)
	return wrapStyledWordsWithPrefix(prefix, words, options.width, options.styles.detail)
}

func renderStepTypeWord(stepType string, styles streamStyles) styledWord {
	word := styledWord{text: stepType}
	if styles.colorize {
		word.render = func(text string) string {
			return styles.strong.Render(text)
		}
	}
	return word
}

func renderStepDetails(step Step, descriptor StepDescriptor, options renderStepOptions) []string {
	if isAgentStepType(step.Type) {
		if step.Type == "codex" && step.Status == StepStatusRunning {
			return renderRunningCodexDetails(step, options)
		}
		return renderAgentPromptDetails(step, descriptor, options)
	}
	if step.Type != "git.commit" {
		return descriptor.DetailLines
	}

	data := cloneDescriptorData(step.Descriptor)
	if data == nil {
		return descriptor.DetailLines
	}

	lines := []string{}
	currentDetailWidth := detailWidth(options.width, options.styles)
	_, changedFiles, insertions, deletions, files, ok := gitCommitRenderData(step)
	if !ok {
		return descriptor.DetailLines
	}
	plusWidth, minusWidth := gitCommitDiffColumnWidths(insertions, deletions, files)

	lines = append(lines, renderGitCommitTotalsLine(
		insertions,
		deletions,
		changedFiles,
		plusWidth,
		minusWidth,
		currentDetailWidth,
		options.styles,
	)...)
	if len(files) == 0 {
		return lines
	}

	lines = append(lines, renderGitCommitFileTable(step, files, plusWidth, minusWidth, currentDetailWidth, options.styles)...)
	return lines
}

type styledWord struct {
	text   string
	render func(string) string
}

func styledWords(text string, render func(string) string) []styledWord {
	parts := strings.Fields(text)
	words := make([]styledWord, 0, len(parts))
	for _, part := range parts {
		words = append(words, styledWord{text: part, render: render})
	}
	return words
}

func wrapStyledWordsWithPrefix(prefix string, words []styledWord, width int, continuation lipgloss.Style) []string {
	plainWords := make([]string, 0, len(words))
	for _, word := range words {
		plainWords = append(plainWords, word.text)
	}
	if len(plainWords) == 0 {
		return []string{prefix}
	}

	available := width - lipgloss.Width(prefix) - 1
	switch {
	case available > 0:
		rendered := wrapStyledWords(words, available)
		if len(rendered) == 0 {
			return []string{prefix}
		}
		lines := []string{prefix + " " + rendered[0]}
		for _, line := range rendered[1:] {
			lines = append(lines, continuation.Render(line))
		}
		return lines
	default:
		lines := []string{prefix}
		for _, line := range wrapStyledWords(words, max(width-2, 1)) {
			lines = append(lines, continuation.Render(line))
		}
		return lines
	}
}

func wrapStyledWords(words []styledWord, width int) []string {
	if len(words) == 0 {
		return nil
	}
	if width <= 0 {
		width = 1
	}

	lines := []string{}
	current := []styledWord{words[0]}
	currentWidth := lipgloss.Width(words[0].text)
	for _, word := range words[1:] {
		wordWidth := lipgloss.Width(word.text)
		if currentWidth+1+wordWidth <= width {
			current = append(current, word)
			currentWidth += 1 + wordWidth
			continue
		}
		lines = append(lines, renderStyledWords(current))
		current = []styledWord{word}
		currentWidth = wordWidth
	}
	lines = append(lines, renderStyledWords(current))
	return lines
}

func renderStyledWords(words []styledWord) string {
	parts := make([]string, 0, len(words))
	for _, word := range words {
		if word.render == nil {
			parts = append(parts, word.text)
			continue
		}
		parts = append(parts, word.render(word.text))
	}
	return strings.Join(parts, " ")
}

func wrapWithPrefix(prefix string, suffix string, width int, continuation lipgloss.Style) []string {
	if suffix == "" {
		return []string{prefix}
	}

	available := width - lipgloss.Width(prefix) - 1
	switch {
	case available > 0:
		wrapped := wrapDescriptorText(suffix, available)
		if len(wrapped) == 0 {
			return []string{prefix}
		}
		lines := []string{prefix + " " + wrapped[0]}
		for _, line := range wrapped[1:] {
			lines = append(lines, continuation.Render(line))
		}
		return lines
	default:
		lines := []string{prefix}
		for _, line := range wrapDescriptorText(suffix, width-2) {
			lines = append(lines, continuation.Render(line))
		}
		return lines
	}
}

func detailWidth(width int, styles streamStyles) int {
	indentWidth := lipgloss.Width(styles.detail.Render("x")) - lipgloss.Width("x")
	if width <= indentWidth {
		return width
	}
	return width - indentWidth
}

func resolvedWidth(width int) int {
	if width <= 0 {
		return defaultStreamWidth
	}
	return width
}

func visibleActiveSteps(active []Step) []Step {
	if len(active) <= 2 {
		return active
	}
	return []Step{active[0], active[len(active)-1]}
}

func shouldRenderStartedStep(step Step, active []Step) bool {
	return shouldRenderStep(step, startedStepAncestors(active, step))
}

func shouldRenderFinishedStep(step Step, active []Step) bool {
	return shouldRenderStep(step, active)
}

func shouldRenderStep(step Step, ancestors []Step) bool {
	if !hasControlAncestor(ancestors) {
		return true
	}
	if isControlStepType(step.Type) {
		return false
	}
	if step.Type == "expr" && !stepHasVisibleDescriptor(step) {
		return false
	}
	return true
}

func startedStepAncestors(active []Step, step Step) []Step {
	if len(active) == 0 {
		return nil
	}
	if index := findActiveStep(active, step); index >= 0 {
		return active[:index]
	}
	return active[:max(len(active)-1, 0)]
}

func hasControlAncestor(active []Step) bool {
	for _, step := range active {
		if isControlStepType(step.Type) {
			return true
		}
	}
	return false
}

func isControlStepType(stepType string) bool {
	switch stepType {
	case "call", "switch", "for_each":
		return true
	default:
		return false
	}
}

func stepHasVisibleDescriptor(step Step) bool {
	if step.Descriptor == nil {
		return false
	}
	if strings.TrimSpace(step.Descriptor.PrimaryText) != "" {
		return true
	}
	for _, detail := range step.Descriptor.DetailText {
		if strings.TrimSpace(detail) != "" {
			return true
		}
	}
	return false
}

func statusTokenForStep(step Step) string {
	if step.Status == StepStatusSkipped {
		return "-"
	}
	return "⏺"
}

func renderStatusToken(step Step, token string, styles streamStyles) string {
	token = strings.TrimSpace(token)
	if token == "" {
		token = "⏺"
	}
	if !styles.colorize {
		return token
	}
	switch step.Status {
	case StepStatusSucceeded:
		return styles.statusOK.Render(token)
	case StepStatusFailed:
		return styles.statusFail.Render(token)
	default:
		return token
	}
}

func formatElapsed(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}

	totalSeconds := int(duration.Round(time.Second).Seconds())
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func writerFD(writer io.Writer) (int, bool) {
	type fdWriter interface {
		Fd() uintptr
	}

	file, ok := writer.(fdWriter)
	if !ok {
		return 0, false
	}
	return int(file.Fd()), true
}

func currentUTC() time.Time {
	return time.Now().UTC()
}

func interruptSelf() {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		return
	}
	_ = process.Signal(os.Interrupt)
}

var interruptFn = interruptSelf

func renderProgressBlocks(blocks []string) string {
	if len(blocks) == 0 {
		return ""
	}
	return "\n" + strings.Join(blocks, "\n\n")
}

var _ Sink = (*StreamController)(nil)
var _ streamRenderer = (*plainStreamRenderer)(nil)
var _ streamRenderer = (*teaStreamRenderer)(nil)
var _ tea.Model = streamModel{}
