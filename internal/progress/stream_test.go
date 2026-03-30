package progress

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	charmansi "github.com/charmbracelet/x/ansi"
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
						PrimaryText: "gpt-5.4:high",
						DetailText: []string{
							"Implement descriptor-repo with enough detail to wrap across multiple descriptor lines cleanly.",
						},
					},
				},
			},
			width: 40,
			want: strings.Join([]string{
				"⏺ 00:05 codex gpt-5.4:high",
				"  ",
				"   [P]rompt prompt.md",
				"   [T]hinking (none yet)",
				"   [S]hell (none yet)",
			}, "\n"),
		},
		{
			name: "crush step started",
			event: Event{
				Kind: EventStepStarted,
				Step: &Step{
					Type:      "crush",
					Status:    StepStatusRunning,
					StartedAt: startedAt,
					Descriptor: &DescriptorData{
						PrimaryText: "claude-sonnet-4-5",
						DetailText:  []string{"Implement the feature."},
					},
				},
			},
			width: 40,
			want: strings.Join([]string{
				"⏺ 00:05 crush claude-sonnet-4-5",
				"  ",
				"     Implement the feature.",
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
				"⏺ 00:05 git.commit abc123d engine:",
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
			}, newStreamStyles(false))

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
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
		styles:  newStreamStyles(false),
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
	if !strings.Contains(view, "⏺ 00:05 call apply") {
		t.Fatalf("view = %q, want static running parent line", view)
	}

	activeSpinnerPrefix := strings.TrimSpace(nextModel.spinner.View()) + " 00:05 shell go test ./internal/runtime"
	if !strings.Contains(view, activeSpinnerPrefix) {
		t.Fatalf("view = %q, want animated child line %q", view, activeSpinnerPrefix)
	}
	if strings.Contains(view, "⏺ 00:05 shell go test ./internal/runtime") {
		t.Fatalf("view = %q, want only one static running bullet", view)
	}
}

func TestRenderStepBlockColorizesStatusTokensAndBoldsStepType(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 14, 10, 0, 5, 0, time.UTC)
	styles := newStreamStyles(true)
	options := renderStepOptions{
		now:         now,
		width:       80,
		styles:      styles,
		statusToken: "⏺",
	}

	testCases := []struct {
		name    string
		step    Step
		wantSub string
	}{
		{
			name: "succeeded renders green bullet and bold type",
			step: Step{
				Type:       "shell",
				Status:     StepStatusSucceeded,
				StartedAt:  now.Add(-5 * time.Second),
				FinishedAt: now,
				Descriptor: &DescriptorData{PrimaryText: "echo ok"},
			},
			wantSub: styles.statusOK.Render("⏺") + " 00:05 " + styles.strong.Render("shell") + " echo ok",
		},
		{
			name: "failed renders red bullet and bold type",
			step: Step{
				Type:       "assert",
				Status:     StepStatusFailed,
				StartedAt:  now.Add(-1 * time.Second),
				FinishedAt: now,
				Descriptor: &DescriptorData{PrimaryText: "false"},
			},
			wantSub: styles.statusFail.Render("⏺") + " 00:01 " + styles.strong.Render("assert") + " false",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := renderStepBlock(tc.step, options)
			if !strings.Contains(got, tc.wantSub) {
				t.Fatalf("block = %q, want %q", got, tc.wantSub)
			}
		})
	}
}

func TestStreamModelViewCollapsesDeepActiveStackToOuterAndLeaf(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 14, 10, 0, 5, 0, time.UTC)
	model := streamModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
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
					PrimaryText: "gpt-5.4:medium",
				},
			},
		},
	}

	view := model.View()
	plainView := charmansi.Strip(view)
	if !strings.Contains(view, "⏺ 00:10 "+model.styles.strong.Render("call")+" implement_loop") {
		t.Fatalf("view = %q, want outermost active step", view)
	}
	if !strings.Contains(view, strings.TrimSpace(model.spinner.View())+" 00:02 "+model.styles.strong.Render("codex")+" gpt-5.4:medium") {
		t.Fatalf("view = %q, want deepest active step", view)
	}
	if strings.Contains(view, "switch 2 cases") {
		t.Fatalf("view = %q, want intermediate active steps collapsed", view)
	}
	for _, want := range []string{"[P]rompt", "[T]hinking (none yet)", "[S]hell (none yet)"} {
		if !strings.Contains(plainView, want) {
			t.Fatalf("view = %q, want codex collapsed detail %q", view, want)
		}
	}
}

func TestStreamModelKeyTogglesExpandRunningCodexSections(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	promptPath := filepath.Join(tempDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Implement **feature**."), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	now := time.Date(2026, time.March, 14, 10, 0, 5, 0, time.UTC)
	model := streamModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
		styles:  newStreamStyles(false),
		settings: streamRenderSettings{
			now: func() time.Time {
				return now
			},
			width: 120,
		},
		width: 120,
		active: []Step{
			{
				Type:      "codex",
				Status:    StepStatusRunning,
				StartedAt: now.Add(-5 * time.Second),
				Artifacts: Artifacts{
					Files: map[string]string{"prompt": promptPath},
				},
				Descriptor: &DescriptorData{
					PrimaryText: "gpt-5.4:high",
				},
			},
		},
	}

	collapsed := model.View()
	collapsedPlain := charmansi.Strip(collapsed)
	for _, want := range []string{"[P]rompt", "[T]hinking (none yet)", "[S]hell (none yet)"} {
		if !strings.Contains(collapsedPlain, want) {
			t.Fatalf("collapsed view = %q, want %q", collapsed, want)
		}
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	expandedPrompt := next.(streamModel).View()
	if !strings.Contains(expandedPrompt, " [P]rompt") {
		t.Fatalf("expanded prompt view = %q, want prompt header", expandedPrompt)
	}
	if !strings.Contains(charmansi.Strip(expandedPrompt), "Implement feature.") {
		t.Fatalf("expanded prompt view = %q, want rendered prompt body", expandedPrompt)
	}

	next, _ = next.(streamModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	expandedThinking := next.(streamModel).View()
	if !strings.Contains(expandedThinking, " [T]hinking") || !strings.Contains(expandedThinking, "(none yet)") {
		t.Fatalf("expanded thinking view = %q, want thinking block", expandedThinking)
	}

	next, _ = next.(streamModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	expandedShell := next.(streamModel).View()
	if !strings.Contains(expandedShell, " [S]hell") || !strings.Contains(expandedShell, "(none yet)") {
		t.Fatalf("expanded shell view = %q, want shell block", expandedShell)
	}
}

func TestStreamModelKeyTogglesExpandRunningClaudeSections(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	promptPath := filepath.Join(tempDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Review **changes**."), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	now := time.Date(2026, time.March, 14, 10, 0, 5, 0, time.UTC)
	model := streamModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
		styles:  newStreamStyles(false),
		settings: streamRenderSettings{
			now: func() time.Time {
				return now
			},
			width: 120,
		},
		width: 120,
		active: []Step{
			{
				Type:      "claude",
				Status:    StepStatusRunning,
				StartedAt: now.Add(-5 * time.Second),
				Artifacts: Artifacts{
					Files: map[string]string{"prompt": promptPath},
				},
				Descriptor: &DescriptorData{
					PrimaryText: "claude-sonnet-4-6:high",
				},
			},
		},
	}

	collapsed := model.View()
	collapsedPlain := charmansi.Strip(collapsed)
	for _, want := range []string{"[P]rompt", "[T]hinking (none yet)", "[S]hell (none yet)"} {
		if !strings.Contains(collapsedPlain, want) {
			t.Fatalf("collapsed view = %q, want %q", collapsed, want)
		}
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	expandedPrompt := next.(streamModel).View()
	if !strings.Contains(expandedPrompt, " [P]rompt") {
		t.Fatalf("expanded prompt view = %q, want prompt header", expandedPrompt)
	}
	if !strings.Contains(charmansi.Strip(expandedPrompt), "Review changes.") {
		t.Fatalf("expanded prompt view = %q, want rendered prompt body", expandedPrompt)
	}

	next, _ = next.(streamModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	expandedThinking := next.(streamModel).View()
	if !strings.Contains(expandedThinking, " [T]hinking") || !strings.Contains(expandedThinking, "(none yet)") {
		t.Fatalf("expanded thinking view = %q, want thinking block", expandedThinking)
	}

	next, _ = next.(streamModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	expandedShell := next.(streamModel).View()
	if !strings.Contains(expandedShell, " [S]hell") || !strings.Contains(expandedShell, "(none yet)") {
		t.Fatalf("expanded shell view = %q, want shell block", expandedShell)
	}
}

func TestStreamModelCtrlCReturnsQuitCommand(t *testing.T) {
	t.Parallel()
	originalInterrupt := interruptFn
	interruptFn = func() {}
	t.Cleanup(func() {
		interruptFn = originalInterrupt
	})

	model := streamModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
		styles:  newStreamStyles(false),
		settings: streamRenderSettings{
			now:   func() time.Time { return time.Now().UTC() },
			width: 80,
		},
		width: 80,
	}

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatalf("ctrl+c update cmd = nil, want quit command")
	}
}

func TestStreamModelKeepsFinishedHistoryVisibleAcrossLaterEvents(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.March, 14, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(5 * time.Second)
	failedAt := finishedAt.Add(1 * time.Second)
	model := streamModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
		styles:  newStreamStyles(false),
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
		`⏺ 00:05 shell /bin/sh -c "sleep 1"`,
		"⏺ 00:01 assert false",
		"⏺ 00:00 run intentional failure",
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
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
		styles:  newStreamStyles(false),
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
		"\n⏺ 00:00 expr\n⏺ 00:00 expr",
	} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("view = %q, want nested loop scaffolding hidden", view)
		}
	}
	if !strings.Contains(view, "⏺ 05:59 call implement_loop") {
		t.Fatalf("view = %q, want outer loop summary", view)
	}
	if !strings.Contains(view, "⏺ 00:00 assert true") {
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
		if got := blockForEvent(event, settings, newStreamStyles(false)); got != "" {
			t.Fatalf("block = %q, want nested scaffolding omitted", got)
		}
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

	if got, want := strings.TrimSpace(output.String()), "⏺ 00:05 assert true"; got != want {
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

func TestRenderAgentPromptMarkdownUsesDraculaStyle(t *testing.T) {
	t.Parallel()

	rendered, err := renderAgentPromptMarkdown("Paragraph\n\n```go\nfmt.Println(\"hi\")\n```", 40)
	if err != nil {
		t.Fatalf("renderAgentPromptMarkdown: %v", err)
	}

	joined := strings.Join(rendered, "\n")
	if !strings.Contains(joined, "\x1b[") {
		t.Fatalf("rendered = %q, want ANSI styling", joined)
	}
	if !strings.Contains(charmansi.Strip(joined), "Paragraph") {
		t.Fatalf("rendered = %q, want paragraph text", joined)
	}
	if !strings.Contains(charmansi.Strip(joined), "fmt.Println(\"hi\")") {
		t.Fatalf("rendered = %q, want code block text", joined)
	}
}

func boolPointer(value bool) *bool {
	return &value
}

func TestPromptPathDisplayPrefersShortestCandidate(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		t.Skip("home directory unavailable")
	}

	workspaceRoot := filepath.Join(home, "workspace")
	promptPath := filepath.Join(workspaceRoot, "flows", "prompt.md")

	got := promptPathDisplay(promptPath, workspaceRoot)
	want := filepath.ToSlash(filepath.Join("flows", "prompt.md"))
	if got != want {
		t.Fatalf("promptPathDisplay = %q, want %q", got, want)
	}
}

func TestPromptPathDisplayFallsBackToHomeVariantWhenRelativeUnavailable(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		t.Skip("home directory unavailable")
	}

	workspaceRoot := filepath.Join(home, "workspace")
	promptPath := filepath.Join(home, "other-project", "prompt.md")

	got := promptPathDisplay(promptPath, workspaceRoot)
	want := filepath.ToSlash(filepath.Join("~", "other-project", "prompt.md"))
	if got != want {
		t.Fatalf("promptPathDisplay = %q, want %q", got, want)
	}
}
