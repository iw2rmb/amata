package progress

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/bubbles/spinner"
)

func TestBlockForEventFormatsPlainText(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.March, 14, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(5 * time.Second)

	testCases := []struct {
		name  string
		event Event
		width int
		want  string
	}{
		{
			name: "codex step started",
			event: Event{
				Kind: EventStepStarted,
				Step: &Step{
					Type:      "codex",
					Status:    StepStatusRunning,
					StartedAt: startedAt,
					Descriptor: &DescriptorData{
						PrimaryText: "gpt-5.4 high",
						DetailText: []string{
							"Implement descriptor-repo with enough detail to wrap across multiple descriptor lines cleanly.",
						},
					},
				},
			},
			width: 40,
			want: strings.Join([]string{
				"• 00:05 codex gpt-5.4 high",
				"  ",
				"   Implement descriptor-repo with enough",
				"   detail to wrap across multiple",
				"   descriptor lines cleanly.",
			}, "\n"),
		},
		{
			name: "git commit finished",
			event: Event{
				Kind: EventStepFinished,
				Step: &Step{
					Type:       "git.commit",
					Status:     StepStatusSucceeded,
					StartedAt:  startedAt,
					FinishedAt: finishedAt,
					Descriptor: &DescriptorData{
						PrimaryText: "abc123d files 2 +7 -3",
						DetailText: []string{
							"engine: persist structured commit summary",
							"+5 -2 engine.txt",
							"+2 -1 notes/todo.txt",
						},
					},
					Value: map[string]any{
						"metadata": map[string]any{
							"shortCommit":      "abc123d",
							"changedFileCount": 2,
							"insertions":       7,
							"deletions":        3,
							"files": []any{
								map[string]any{"path": "engine.txt", "insertions": 5, "deletions": 2},
								map[string]any{"path": "notes/todo.txt", "insertions": 2, "deletions": 1},
							},
						},
					},
				},
			},
			width: 34,
			want: strings.Join([]string{
				"✓ 00:05 git.commit abc123d engine:",
				"  persist",
				"  structured",
				"  commit summary",
				"  +7 -3 files: 2",
				"  +5 -2 engine.txt",
				"  +2 -1 notes/todo.txt",
			}, "\n"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := blockForEvent(testCase.event, streamRenderSettings{
				now: func() time.Time {
					return finishedAt
				},
				width: testCase.width,
			})

			if got != testCase.want {
				t.Fatalf("block = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestStreamModelRendersSingleAnimatedSpinnerForDeepestActiveStep(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 14, 10, 0, 5, 0, time.UTC)
	model := streamModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.Line)),
		styles:  defaultStreamStyles(),
		settings: streamRenderSettings{
			now: func() time.Time {
				return now
			},
			width: 60,
		},
		width: 60,
	}

	startedAt := now.Add(-5 * time.Second)
	parent := Step{
		Type:      "call",
		Status:    StepStatusRunning,
		StartedAt: startedAt,
		Descriptor: &DescriptorData{
			PrimaryText: "apply",
		},
	}
	child := Step{
		Type:      "shell",
		Status:    StepStatusRunning,
		StartedAt: startedAt,
		Descriptor: &DescriptorData{
			PrimaryText: "go test ./internal/runtime",
		},
	}

	nextModel, _ := model.applyEvent(Event{Kind: EventStepStarted, Step: &parent})
	nextModel, _ = nextModel.applyEvent(Event{Kind: EventStepStarted, Step: &child})

	view := nextModel.View()
	if !strings.Contains(view, "• 00:05 call apply") {
		t.Fatalf("view = %q, want static running parent line", view)
	}

	activeSpinnerPrefix := nextModel.spinner.View() + " 00:05 shell go test ./internal/runtime"
	if !strings.Contains(view, activeSpinnerPrefix) {
		t.Fatalf("view = %q, want animated child line %q", view, activeSpinnerPrefix)
	}
	if strings.Contains(view, "• 00:05 shell go test ./internal/runtime") {
		t.Fatalf("view = %q, want only one static running bullet", view)
	}
}

func TestStreamModelViewCollapsesDeepActiveStackToOuterAndLeaf(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 14, 10, 0, 5, 0, time.UTC)
	model := streamModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.Line)),
		styles:  newStreamStyles(true),
		settings: streamRenderSettings{
			now: func() time.Time {
				return now
			},
			width: 80,
		},
		width: 80,
		active: []Step{
			{
				Type:      "call",
				Status:    StepStatusRunning,
				StartedAt: now.Add(-10 * time.Second),
				Descriptor: &DescriptorData{
					PrimaryText: "implement_loop",
				},
			},
			{
				Type:      "switch",
				Status:    StepStatusRunning,
				StartedAt: now.Add(-9 * time.Second),
				Descriptor: &DescriptorData{
					PrimaryText: "2 cases",
				},
			},
			{
				Type:      "call",
				Status:    StepStatusRunning,
				StartedAt: now.Add(-8 * time.Second),
				Descriptor: &DescriptorData{
					PrimaryText: "implement_loop",
				},
			},
			{
				Type:      "codex",
				Status:    StepStatusRunning,
				StartedAt: now.Add(-2 * time.Second),
				Descriptor: &DescriptorData{
					PrimaryText: "gpt-5.4 medium",
				},
			},
		},
	}

	view := model.View()
	if !strings.Contains(view, "• 00:10 call implement_loop") {
		t.Fatalf("view = %q, want outermost active step", view)
	}
	if !strings.Contains(view, model.spinner.View()+" 00:02 codex gpt-5.4 medium") {
		t.Fatalf("view = %q, want deepest active step", view)
	}
	if strings.Contains(view, "switch 2 cases") {
		t.Fatalf("view = %q, want intermediate active steps collapsed", view)
	}
	if got := countNonEmptyLines(view); got != 2 {
		t.Fatalf("view = %q, want exactly two visible active lines", view)
	}
}

func TestStreamModelKeepsFinishedHistoryVisibleAcrossLaterEvents(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.March, 14, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(5 * time.Second)
	failedAt := finishedAt.Add(1 * time.Second)
	model := streamModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.Line)),
		styles:  defaultStreamStyles(),
		settings: streamRenderSettings{
			now: func() time.Time {
				return failedAt
			},
			width: 60,
		},
		history: []string{},
		width:   60,
	}

	shellStep := Step{
		Type:       "shell",
		Status:     StepStatusSucceeded,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		Descriptor: &DescriptorData{
			PrimaryText: `/bin/sh -c "sleep 1"`,
		},
	}
	assertStep := Step{
		Type:       "assert",
		Status:     StepStatusFailed,
		StartedAt:  finishedAt,
		FinishedAt: failedAt,
		Error: &Failure{
			Message: "intentional failure",
		},
		Descriptor: &DescriptorData{
			PrimaryText: "false",
			DetailText:  []string{"intentional failure"},
		},
	}

	nextModel, _ := model.applyEvent(Event{Kind: EventStepStarted, Step: &shellStep})
	nextModel, _ = nextModel.applyEvent(Event{Kind: EventStepFinished, Step: &shellStep})
	nextModel, _ = nextModel.applyEvent(Event{Kind: EventStepStarted, Step: &Step{
		Type:      "assert",
		Status:    StepStatusRunning,
		StartedAt: finishedAt,
		Descriptor: &DescriptorData{
			PrimaryText: "false",
			DetailText:  []string{"intentional failure"},
		},
	}})
	nextModel, _ = nextModel.applyEvent(Event{Kind: EventStepFinished, Step: &assertStep})
	nextModel, _ = nextModel.applyEvent(Event{
		Kind: EventRunFinished,
		Failure: &Failure{
			Message: "intentional failure",
		},
	})

	view := nextModel.View()
	for _, want := range []string{
		`✓ 00:05 shell /bin/sh -c "sleep 1"`,
		"X 00:01 assert false",
		"X 00:00 run intentional failure",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view = %q, want %q", view, want)
		}
	}
	if strings.Contains(view, "| 00:") {
		t.Fatalf("view = %q, want no active spinner after failure is recorded", view)
	}
}

func TestStreamModelCollapsesNestedLoopScaffoldingInFinishedHistory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 14, 10, 6, 0, 0, time.UTC)
	model := streamModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.Line)),
		styles:  defaultStreamStyles(),
		settings: streamRenderSettings{
			now: func() time.Time {
				return now
			},
			width: 80,
		},
		width: 80,
	}

	finished := func(step Step, active []Step) {
		next, _ := model.applyEvent(Event{
			Kind:     EventStepFinished,
			Step:     &step,
			Snapshot: Snapshot{Active: active},
		})
		model = next
	}

	finished(Step{
		Type:       "expr",
		Status:     StepStatusSucceeded,
		StartedAt:  now,
		FinishedAt: now,
	}, nil)
	finished(Step{
		Type:       "switch",
		Status:     StepStatusSucceeded,
		StartedAt:  now.Add(-5 * time.Minute),
		FinishedAt: now.Add(-8 * time.Second),
		Descriptor: &DescriptorData{PrimaryText: "case 0"},
	}, []Step{
		{Type: "call", Descriptor: &DescriptorData{PrimaryText: "implement_loop"}},
	})
	finished(Step{
		Type:       "expr",
		Status:     StepStatusSucceeded,
		StartedAt:  now.Add(-2 * time.Second),
		FinishedAt: now.Add(-2 * time.Second),
	}, []Step{
		{Type: "call", Descriptor: &DescriptorData{PrimaryText: "implement_loop"}},
		{Type: "switch", Descriptor: &DescriptorData{PrimaryText: "case 1"}},
	})
	finished(Step{
		Type:       "call",
		Status:     StepStatusSucceeded,
		StartedAt:  now.Add(-15 * time.Second),
		FinishedAt: now,
		Descriptor: &DescriptorData{PrimaryText: "implement_loop"},
	}, []Step{
		{Type: "call", Descriptor: &DescriptorData{PrimaryText: "implement_loop"}},
		{Type: "switch", Descriptor: &DescriptorData{PrimaryText: "case 1"}},
	})
	finished(Step{
		Type:       "switch",
		Status:     StepStatusSucceeded,
		StartedAt:  now.Add(-5 * time.Minute),
		FinishedAt: now.Add(-1 * time.Second),
		Descriptor: &DescriptorData{PrimaryText: "case 1"},
	}, []Step{
		{Type: "call", Descriptor: &DescriptorData{PrimaryText: "implement_loop"}},
	})
	finished(Step{
		Type:       "call",
		Status:     StepStatusSucceeded,
		StartedAt:  now.Add(-5*time.Minute - 59*time.Second),
		FinishedAt: now,
		Descriptor: &DescriptorData{PrimaryText: "implement_loop"},
	}, nil)
	finished(Step{
		Type:       "assert",
		Status:     StepStatusSucceeded,
		StartedAt:  now,
		FinishedAt: now,
		Descriptor: &DescriptorData{
			PrimaryText: "true",
			DetailText:  []string{"implement_loop must terminate with hasItem=false"},
		},
	}, nil)

	view := model.View()
	if strings.Count(view, "call implement_loop") != 1 {
		t.Fatalf("view = %q, want one visible loop call summary", view)
	}
	for _, unwanted := range []string{
		"switch case 0",
		"switch case 1",
		"\n✓ 00:00 expr\n✓ 00:00 expr",
	} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("view = %q, want nested loop scaffolding hidden", view)
		}
	}
	if !strings.Contains(view, "✓ 05:59 call implement_loop") {
		t.Fatalf("view = %q, want outer loop summary", view)
	}
	if !strings.Contains(view, "✓ 00:00 assert true") {
		t.Fatalf("view = %q, want terminal assertion", view)
	}
}

func TestBlockForEventSkipsNestedControlAndExprScaffolding(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 14, 10, 0, 5, 0, time.UTC)
	settings := streamRenderSettings{
		now: func() time.Time {
			return now
		},
		width: 80,
	}

	for _, event := range []Event{
		{
			Kind: EventStepStarted,
			Step: &Step{
				Type:      "call",
				Status:    StepStatusRunning,
				StartedAt: now.Add(-5 * time.Second),
				Descriptor: &DescriptorData{
					PrimaryText: "implement_loop",
				},
			},
			Snapshot: Snapshot{
				Active: []Step{
					{Type: "call", Descriptor: &DescriptorData{PrimaryText: "implement_loop"}},
					{Type: "call", Descriptor: &DescriptorData{PrimaryText: "implement_loop"}},
				},
			},
		},
		{
			Kind: EventStepFinished,
			Step: &Step{
				Type:       "switch",
				Status:     StepStatusSucceeded,
				StartedAt:  now.Add(-5 * time.Second),
				FinishedAt: now,
				Descriptor: &DescriptorData{
					PrimaryText: "case 1",
				},
			},
			Snapshot: Snapshot{
				Active: []Step{
					{Type: "call", Descriptor: &DescriptorData{PrimaryText: "implement_loop"}},
				},
			},
		},
		{
			Kind: EventStepFinished,
			Step: &Step{
				Type:       "expr",
				Status:     StepStatusSucceeded,
				StartedAt:  now,
				FinishedAt: now,
			},
			Snapshot: Snapshot{
				Active: []Step{
					{Type: "call", Descriptor: &DescriptorData{PrimaryText: "implement_loop"}},
					{Type: "switch", Descriptor: &DescriptorData{PrimaryText: "case 1"}},
				},
			},
		},
	} {
		if got := blockForEvent(event, settings); got != "" {
			t.Fatalf("block = %q, want nested scaffolding omitted", got)
		}
	}
}

func TestRenderGitCommitFileTableAlignsDiffColumns(t *testing.T) {
	t.Parallel()

	files := []commitFileDescriptor{
		{Path: "docs/schemas/address.schema.json", Insertions: 53, Deletions: 2},
		{Path: "deleted.txt", Insertions: 0, Deletions: 24},
	}
	plusWidth, minusWidth := gitCommitDiffColumnWidths(53, 24, files)
	got := renderGitCommitFileTable(Step{}, files, plusWidth, minusWidth, 60, defaultStreamStyles())

	want := []string{
		"+53  -2 docs/schemas/address.schema.json",
		" +0 -24 deleted.txt",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("file table = %#v, want %#v", got, want)
	}
}

func TestRenderGitCommitFileTableAddsHyperlinksWhenRepoRootIsAvailable(t *testing.T) {
	t.Parallel()

	files := []commitFileDescriptor{
		{Path: "notes/todo.txt", Insertions: 1, Deletions: 0},
	}
	plusWidth, minusWidth := gitCommitDiffColumnWidths(1, 0, files)
	got := strings.Join(renderGitCommitFileTable(Step{
		Value: map[string]any{
			"repoRoot": "/repo",
		},
	}, files, plusWidth, minusWidth, 60, newStreamStyles(true)), "\n")

	if !strings.Contains(got, "file:///repo/notes/todo.txt") {
		t.Fatalf("file table = %q, want file hyperlink", got)
	}
}

func TestRenderGitCommitTotalsLineSharesWidthsWithFileRows(t *testing.T) {
	t.Parallel()

	files := []commitFileDescriptor{
		{Path: "docs/refactor.md", Insertions: 1, Deletions: 1},
	}
	plusWidth, minusWidth := gitCommitDiffColumnWidths(2611, 89, files)

	totals := renderGitCommitTotalsLine(2611, 89, 21, plusWidth, minusWidth, 80, defaultStreamStyles())
	rows := renderGitCommitFileTable(Step{}, files, plusWidth, minusWidth, 80, defaultStreamStyles())
	if got, want := totals, []string{"+2611 -89 files: 21"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("totals = %#v, want %#v", got, want)
	}
	if got, want := rows, []string{"   +1  -1 docs/refactor.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %#v, want %#v", got, want)
	}

	colored := strings.Join(renderGitCommitTotalsLine(2611, 89, 21, plusWidth, minusWidth, 80, newStreamStyles(true)), "\n")
	if !strings.Contains(colored, "\x1b[") {
		t.Fatalf("colored totals = %q, want ANSI colors", colored)
	}
}

func TestNewStreamControllerUsesPlainRendererForNonTTYWriter(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	controller, err := newStreamController(&output, streamControllerOptions{
		forceTTY: boolPointer(false),
		now: func() time.Time {
			return time.Date(2026, time.March, 14, 10, 0, 5, 0, time.UTC)
		},
		width: 32,
	})
	if err != nil {
		t.Fatalf("newStreamController: %v", err)
	}

	controller.WriteProgress(Event{
		Kind: EventStepFinished,
		Step: &Step{
			Type:       "assert",
			Status:     StepStatusSucceeded,
			StartedAt:  time.Date(2026, time.March, 14, 10, 0, 0, 0, time.UTC),
			FinishedAt: time.Date(2026, time.March, 14, 10, 0, 5, 0, time.UTC),
			Descriptor: &DescriptorData{
				PrimaryText: "true",
			},
		},
	})

	if got, want := strings.TrimSpace(output.String()), "✓ 00:05 assert true"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestNewStreamControllerDefaultNowAdvances(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	controller, err := newStreamController(&output, streamControllerOptions{
		forceTTY: boolPointer(false),
	})
	if err != nil {
		t.Fatalf("newStreamController: %v", err)
	}

	renderer, ok := controller.renderer.(*plainStreamRenderer)
	if !ok {
		t.Fatalf("renderer type = %T, want *plainStreamRenderer", controller.renderer)
	}

	first := renderer.settings.now()
	time.Sleep(20 * time.Millisecond)
	second := renderer.settings.now()
	if !second.After(first) {
		t.Fatalf("default now callback is frozen: first=%s second=%s", first, second)
	}
}

func TestRenderProgressBlocksAddsBlankLineBeforeEachBlock(t *testing.T) {
	t.Parallel()

	got := renderProgressBlocks([]string{"first", "second"})
	want := "\nfirst\n\nsecond"
	if got != want {
		t.Fatalf("renderProgressBlocks = %q, want %q", got, want)
	}
}

func countNonEmptyLines(text string) int {
	count := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func TestRenderAgentPromptMarkdownAppliesPaddingAndWidth(t *testing.T) {
	t.Parallel()

	lines, err := renderAgentPromptMarkdown(
		"First paragraph with enough words to wrap across multiple lines.\n\n```go\nfmt.Println(\"hi\")\n```",
		32,
	)
	if err != nil {
		t.Fatalf("renderAgentPromptMarkdown: %v", err)
	}
	if len(lines) < 4 {
		t.Fatalf("lines = %#v, want rendered markdown output", lines)
	}
	if lines[0] != "" {
		t.Fatalf("top padding line = %q, want empty", lines[0])
	}
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, " ") {
			t.Fatalf("line = %q, want one-space left padding", line)
		}
		if got := lipgloss.Width(line); got > 32+agentPromptLeftPadding {
			t.Fatalf("line width = %d for %q, want <= %d", got, line, 32+agentPromptLeftPadding)
		}
	}
}

func TestRenderAgentPromptMarkdownColorizesBodyAndKeepsCodeWhite(t *testing.T) {
	t.Parallel()

	rendered, err := renderAgentPromptMarkdown("Paragraph\n\n```go\nfmt.Println(\"hi\")\n```", 40)
	if err != nil {
		t.Fatalf("renderAgentPromptMarkdown: %v", err)
	}

	joined := strings.Join(rendered, "\n")
	if !strings.Contains(joined, "\x1b[") {
		t.Fatalf("rendered = %q, want ANSI styling", joined)
	}
	if !strings.Contains(joined, "[38;5;252mParagraph") {
		t.Fatalf("rendered = %q, want dim white paragraph text", joined)
	}
	if !strings.Contains(joined, "[38;5;231mfmt") && !strings.Contains(joined, "[38;5;255mfmt") {
		t.Fatalf("rendered = %q, want white code block text", joined)
	}
}

func boolPointer(value bool) *bool {
	return &value
}
