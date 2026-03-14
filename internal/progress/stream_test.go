package progress

import (
	"bytes"
	"strings"
	"testing"
	"time"

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
				"  Implement descriptor-repo with enough",
				"  detail to wrap across multiple",
				"  descriptor lines cleanly.",
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
				},
			},
			width: 34,
			want: strings.Join([]string{
				"✓ 00:05 git.commit abc123d files 2",
				"  +7 -3",
				"  engine: persist structured",
				"  commit summary",
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
	if strings.Count(view, "\n")+1 != 2 {
		t.Fatalf("view = %q, want exactly two active lines", view)
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

func TestRenderGitCommitFileTableAlignsDiffColumns(t *testing.T) {
	t.Parallel()

	got := renderGitCommitFileTable(Step{}, []commitFileDescriptor{
		{Path: "docs/schemas/address.schema.json", Insertions: 53, Deletions: 2},
		{Path: "deleted.txt", Insertions: 0, Deletions: 24},
	}, 60, defaultStreamStyles())

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

	got := strings.Join(renderGitCommitFileTable(Step{
		Value: map[string]any{
			"repoRoot": "/repo",
		},
	}, []commitFileDescriptor{
		{Path: "notes/todo.txt", Insertions: 1, Deletions: 0},
	}, 60, newStreamStyles(true)), "\n")

	if !strings.Contains(got, "file:///repo/notes/todo.txt") {
		t.Fatalf("file table = %q, want file hyperlink", got)
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

func boolPointer(value bool) *bool {
	return &value
}
