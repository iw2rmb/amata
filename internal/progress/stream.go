package progress

import (
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	lipgloss "charm.land/lipgloss/v2"
	liptable "charm.land/lipgloss/v2/table"
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
	spinner  spinner.Model
	styles   streamStyles
	settings streamRenderSettings
	history  []string
	active   []Step
	width    int
}

type streamStyles struct {
	colorize    bool
	detail      lipgloss.Style
	diffPlus    lipgloss.Style
	diffMinus   lipgloss.Style
	strong      lipgloss.Style
	pathAdded   lipgloss.Style
	pathDeleted lipgloss.Style
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
		spinner:  spinner.New(spinner.WithSpinner(spinner.Line)),
		styles:   newStreamStyles(true),
		settings: settings,
		history:  []string{},
		width:    settings.width,
		active:   []Step{},
	}

	program := tea.NewProgram(
		model,
		tea.WithInput(nil),
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
	block := blockForEvent(event, r.settings)
	if block == "" {
		return
	}
	_, _ = fmt.Fprintln(r.writer, block)
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
		statusToken := "•"
		if index == len(visibleActive)-1 {
			statusToken = m.spinner.View()
		}
		blocks = append(blocks, renderStepBlock(step, renderStepOptions{
			statusToken: statusToken,
			now:         m.settings.now(),
			width:       resolvedWidth(m.width),
			styles:      m.styles,
		}))
	}
	if len(blocks) == 0 {
		return ""
	}
	return strings.Join(blocks, "\n")
}

func (m streamModel) applyEvent(event Event) (streamModel, tea.Cmd) {
	switch event.Kind {
	case EventRunResumed:
		m.active = cloneActiveSteps(event.Snapshot.Active)
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
		block := blockForEvent(event, m.settings.withWidth(resolvedWidth(m.width)))
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

func defaultStreamStyles() streamStyles {
	return newStreamStyles(false)
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
	}
}

type renderStepOptions struct {
	statusToken string
	now         time.Time
	width       int
	styles      streamStyles
}

func blockForEvent(event Event, settings streamRenderSettings) string {
	switch event.Kind {
	case EventRunResumed:
		if len(event.Snapshot.Active) == 0 {
			return ""
		}

		visibleActive := visibleActiveSteps(event.Snapshot.Active)
		blocks := make([]string, 0, len(visibleActive))
		styles := defaultStreamStyles()
		for _, step := range visibleActive {
			blocks = append(blocks, renderStepBlock(step, renderStepOptions{
				statusToken: "•",
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
			statusToken: "•",
			now:         settings.now(),
			width:       resolvedWidth(settings.width),
			styles:      defaultStreamStyles(),
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
			styles:      defaultStreamStyles(),
		})
	case EventRunFinished:
		if event.Failure == nil {
			return ""
		}
		return renderFailureBlock(event.Failure, settings)
	default:
		return ""
	}
}

func renderFailureBlock(failure *Failure, settings streamRenderSettings) string {
	if failure == nil {
		return ""
	}

	lines := wrapWithPrefix(
		fmt.Sprintf("X %s run", formatElapsed(0)),
		failure.Message,
		resolvedWidth(settings.width),
		defaultStreamStyles().detail,
	)
	return strings.Join(lines, "\n")
}

func renderStepBlock(step Step, options renderStepOptions) string {
	descriptor := BuildStepDescriptor(step, DescriptorOptions{
		Now:         options.now,
		DetailWidth: detailWidth(options.width, options.styles),
	})

	headlinePrefix := strings.TrimSpace(strings.Join([]string{
		options.statusToken,
		formatElapsed(descriptor.Elapsed),
		descriptor.StepType,
	}, " "))
	lines := renderStepHeadline(step, descriptor, headlinePrefix, options)
	for _, detail := range renderStepDetails(step, descriptor, options) {
		lines = append(lines, options.styles.detail.Render(detail))
	}
	return strings.Join(lines, "\n")
}

func renderStepHeadline(step Step, descriptor StepDescriptor, prefix string, options renderStepOptions) []string {
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
	if !options.styles.colorize {
		return wrapWithPrefix(prefix, shortCommit+" "+message, options.width, options.styles.detail)
	}

	words := []styledWord{{text: shortCommit}}
	words = append(words, styledWords(message, func(text string) string {
		return options.styles.strong.Render(text)
	})...)
	return wrapStyledWordsWithPrefix(prefix, words, options.width, options.styles.detail)
}

func renderStepDetails(step Step, descriptor StepDescriptor, options renderStepOptions) []string {
	if step.Type == "codex" || step.Type == "claude" {
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

	lines = append(lines, wrapDescriptorText(
		fmt.Sprintf("+%d -%d files: %d", insertions, deletions, changedFiles),
		currentDetailWidth,
	)...)
	if len(files) == 0 {
		return lines
	}

	lines = append(lines, renderGitCommitFileTable(step, files, currentDetailWidth, options.styles)...)
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

	width = resolvedWidth(width)
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

func gitCommitRenderData(step Step) (string, int, int, int, []commitFileDescriptor, bool) {
	metadataValue, ok := mapField(step.Value, "metadata")
	if !ok {
		return "", 0, 0, 0, nil, false
	}

	shortCommit, _ := stringField(metadataValue, "shortCommit")
	changedFiles, _ := intField(metadataValue, "changedFileCount")
	insertions, _ := intField(metadataValue, "insertions")
	deletions, _ := intField(metadataValue, "deletions")
	return shortCommit, changedFiles, insertions, deletions, fileStats(metadataValue), shortCommit != ""
}

func gitCommitMessage(step Step) string {
	data := cloneDescriptorData(step.Descriptor)
	if data == nil || len(data.DetailText) == 0 {
		return ""
	}
	return strings.TrimSpace(data.DetailText[0])
}

func renderGitCommitFileTable(step Step, files []commitFileDescriptor, width int, styles streamStyles) []string {
	if len(files) == 0 {
		return nil
	}

	plusWidth := 0
	minusWidth := 0
	pathWidth := 0
	type gitCommitTableRow struct {
		plus  string
		minus string
		path  string
	}
	tableRows := make([]gitCommitTableRow, 0, len(files))
	for _, file := range files {
		plusText := fmt.Sprintf("+%d", file.Insertions)
		minusText := fmt.Sprintf("-%d", file.Deletions)
		plusWidth = max(plusWidth, lipgloss.Width(plusText))
		minusWidth = max(minusWidth, lipgloss.Width(minusText))
		pathWidth = max(pathWidth, lipgloss.Width(file.Path))
		tableRows = append(tableRows, gitCommitTableRow{
			plus:  plusText,
			minus: minusText,
			path:  file.Path,
		})
	}

	rows := make([][]string, 0, len(tableRows))
	for _, row := range tableRows {
		rows = append(rows, []string{
			fmt.Sprintf("%*s", plusWidth, row.plus),
			fmt.Sprintf("%*s", minusWidth, row.minus),
			row.path,
		})
	}

	availablePathWidth := pathWidth
	if width > 0 {
		availablePathWidth = max(1, width-plusWidth-minusWidth-2)
		if pathWidth < availablePathWidth {
			availablePathWidth = pathWidth
		}
	}

	table := liptable.New().
		Rows(rows...).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		BorderColumn(false).
		BorderHeader(false).
		BorderRow(false).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := lipgloss.NewStyle()
			switch col {
			case 0:
				style = style.PaddingRight(1)
				if styles.colorize {
					style = style.Inherit(styles.diffPlus)
				}
			case 1:
				style = style.PaddingRight(1)
				if styles.colorize {
					style = style.Inherit(styles.diffMinus)
				}
			case 2:
				style = style.Width(availablePathWidth)
				if styles.colorize {
					if link := gitCommitFileLink(step, files[row].Path); link != "" {
						style = style.Hyperlink(link)
					}
					switch {
					case files[row].Deletions == 0 && files[row].Insertions > 0:
						style = style.Inherit(styles.pathAdded)
					case files[row].Insertions == 0 && files[row].Deletions > 0:
						style = style.Inherit(styles.pathDeleted)
					}
				}
			}
			return style
		})

	rendered := strings.Split(table.String(), "\n")
	lines := make([]string, 0, len(rendered))
	for _, line := range rendered {
		lines = append(lines, strings.TrimRight(line, " "))
	}
	return lines
}

func gitCommitFileLink(step Step, path string) string {
	repoRoot, ok := stringField(step.Value, "repoRoot")
	if !ok || repoRoot == "" || path == "" {
		return ""
	}

	target := filepath.Join(repoRoot, filepath.FromSlash(path))
	return (&url.URL{
		Scheme: "file",
		Path:   filepath.Clean(target),
	}).String()
}

func wrapWithPrefix(prefix string, suffix string, width int, continuation lipgloss.Style) []string {
	if suffix == "" {
		return []string{prefix}
	}

	width = resolvedWidth(width)
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
	width = resolvedWidth(width)
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
	switch step.Status {
	case StepStatusSucceeded:
		return "✓"
	case StepStatusSkipped:
		return "-"
	case StepStatusFailed:
		return "X"
	default:
		return "•"
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

func cloneActiveSteps(steps []Step) []Step {
	if len(steps) == 0 {
		return []Step{}
	}

	cloned := make([]Step, len(steps))
	for index, step := range steps {
		cloned[index] = cloneStep(step)
	}
	return cloned
}

func currentUTC() time.Time {
	return time.Now().UTC()
}

var _ Sink = (*StreamController)(nil)
var _ streamRenderer = (*plainStreamRenderer)(nil)
var _ streamRenderer = (*teaStreamRenderer)(nil)
var _ tea.Model = streamModel{}
