package progress

import (
	"fmt"
	"io"
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
	spinner  spinner.Model
	styles   streamStyles
	settings streamRenderSettings
	history  []string
	active   []Step
	width    int
}

type streamStyles struct {
	detail lipgloss.Style
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
		styles:   defaultStreamStyles(),
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
	blocks := make([]string, 0, len(m.history)+len(m.active))
	blocks = append(blocks, m.history...)
	for index, step := range m.active {
		statusToken := "•"
		if index == len(m.active)-1 {
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
		m.history = append(m.history, renderStepBlock(step, renderStepOptions{
			statusToken: statusTokenForStep(step),
			now:         step.FinishedAt,
			width:       resolvedWidth(m.width),
			styles:      m.styles,
		}))
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
	return streamStyles{
		detail: lipgloss.NewStyle().PaddingLeft(2),
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

		blocks := make([]string, 0, len(event.Snapshot.Active))
		styles := defaultStreamStyles()
		for _, step := range event.Snapshot.Active {
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
	lines := wrapWithPrefix(headlinePrefix, descriptor.PrimaryText, options.width, options.styles.detail)
	for _, detail := range descriptor.DetailLines {
		lines = append(lines, options.styles.detail.Render(detail))
	}
	return strings.Join(lines, "\n")
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
