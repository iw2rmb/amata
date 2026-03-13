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
						PrimaryText: "{abc123d files 2 +7 -3}",
						DetailText: []string{
							"engine: persist structured commit summary",
							"engine.txt +5 -2",
							"notes/todo.txt +2 -1",
						},
					},
				},
			},
			width: 34,
			want: strings.Join([]string{
				"✓ 00:05 git.commit {abc123d files",
				"  2 +7 -3}",
				"  engine: persist structured",
				"  commit summary",
				"  engine.txt +5 -2",
				"  notes/todo.txt +2 -1",
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

func boolPointer(value bool) *bool {
	return &value
}
