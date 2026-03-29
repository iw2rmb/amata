package progress

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatTokenCount(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		value int
		want  string
	}{
		{value: 999, want: "999"},
		{value: 1000, want: "1k"},
		{value: 1549, want: "1.5k"},
		{value: 15_500, want: "15.5k"},
		{value: 999_949, want: "999.9k"},
		{value: 1_000_000, want: "1M"},
		{value: 1_250_000, want: "1.3M"},
	}

	for _, tc := range testCases {
		if got := formatTokenCount(tc.value); got != tc.want {
			t.Fatalf("formatTokenCount(%d) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestAgentOutputRenderBlock(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		stepType       string
		stdout         string
		stderr         string
		primaryText    string
		detailText     []string
		duration       time.Duration
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:     "claude multi-message output",
			stepType: "claude",
			stdout: strings.Join([]string{
				`{"timestamp":"2026-03-29T10:00:00Z","message":{"input_tokens":10,"cache_creation_input_tokens":3,"cache_read_input_tokens":2,"output_tokens":4},"content":[{"type":"thinking","thinking":"plan"}]}`,
				`{"timestamp":"2026-03-29T10:07:00Z","message":{"input_tokens":20,"cache_creation_input_tokens":1,"cache_read_input_tokens":4,"output_tokens":6},"content":[{"type":"tool_use","name":"Bash","input":{"command":"ls -la"}}]}`,
			}, "\n"),
			primaryText: "claude-sonnet-4-5:high",
			detailText:  []string{"Review the diff."},
			duration:    8 * time.Second,
			wantContains: []string{
				"claude claude-sonnet-4-5:high | 🢁10 🢃30 🢃🢃10",
			},
			wantNotContain: []string{
				"Bash | 🢁6 🢃20 🢃🢃5",
				"ls -la",
			},
		},
		{
			name:     "codex event stream output",
			stepType: "codex",
			stdout: strings.Join([]string{
				`{"timestamp":"2026-03-29T10:00:00Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"}}`,
				`{"timestamp":"2026-03-29T10:02:00Z","type":"event_msg","payload":{"type":"exec_command_end","parsed_cmd":[{"name":"Bash","cmd":"pwd"}]}}`,
				`{"timestamp":"2026-03-29T10:03:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":120,"cached_input_tokens":110,"output_tokens":30},"last_token_usage":{"input_tokens":12,"cached_input_tokens":11,"output_tokens":3}}}}`,
			}, "\n"),
			primaryText: "gpt-5.4:high",
			detailText:  []string{"Implement feature X."},
			duration:    9 * time.Second,
			wantContains: []string{
				"codex gpt-5.4:high | 🢁30 🢃120 🢃🢃110",
			},
			wantNotContain: []string{
				"Shell | 🢁3 🢃12 🢃🢃11",
				"pwd",
			},
		},
		{
			name:     "codex stderr fallback token parsing",
			stepType: "codex",
			stdout:   `{"ok":true}`,
			stderr: strings.Join([]string{
				"exec",
				"/bin/zsh -lc 'pwd' in /tmp succeeded in 1ms:",
				"tokens used",
				"11,496",
			}, "\n"),
			primaryText: "gpt-5.4:high",
			detailText:  []string{"Implement feature X."},
			duration:    9 * time.Second,
			wantContains: []string{
				"codex gpt-5.4:high | 🢁0 🢃11.5k 🢃🢃0",
			},
			wantNotContain: []string{
				"Shell | 🢁0 🢃11.5k 🢃🢃0",
				"/bin/zsh -lc 'pwd'",
			},
		},
		{
			name:        "claude result envelope output",
			stepType:    "claude",
			stdout:      `{"type":"result","stop_reason":"end_turn","usage":{"input_tokens":18,"cache_creation_input_tokens":24700,"cache_read_input_tokens":472210,"output_tokens":3543}}`,
			primaryText: "claude-sonnet-4-6:high",
			detailText:  []string{"Review the diff."},
			duration:    8 * time.Second,
			wantContains: []string{
				"claude claude-sonnet-4-6:high | 🢁3.5k 🢃18 🢃🢃496.9k",
			},
			wantNotContain: []string{
				"result | 🢁3.5k 🢃18 🢃🢃496.9k",
				"end_turn",
			},
		},
		{
			name:     "codex modern event stream with turn.completed",
			stepType: "codex",
			stdout: strings.Join([]string{
				`{"timestamp":"2026-03-29T10:00:00Z","type":"thread.started","thread_id":"tid-1"}`,
				`{"timestamp":"2026-03-29T10:01:00Z","type":"item.started","item":{"id":"item_2","type":"command_execution","command":"/bin/zsh -lc \"pwd\"","status":"in_progress"}}`,
				`{"timestamp":"2026-03-29T10:02:00Z","type":"item.completed","item":{"id":"item_2","type":"command_execution","command":"/bin/zsh -lc \"pwd\"","status":"completed"}}`,
				`{"timestamp":"2026-03-29T10:03:00Z","type":"turn.completed","usage":{"input_tokens":31506,"cached_input_tokens":4864,"output_tokens":595}}`,
			}, "\n"),
			primaryText: "gpt-5.3-codex:low",
			detailText:  []string{"Implement feature X."},
			duration:    9 * time.Second,
			wantContains: []string{
				"codex gpt-5.3-codex:low | 🢁595 🢃31.5k 🢃🢃4.9k",
			},
			wantNotContain: []string{
				"Shell | 🢁0 🢃0 🢃🢃0",
				`/bin/zsh -lc "pwd"`,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			stdoutPath := filepath.Join(tempDir, "stdout.txt")
			if err := os.WriteFile(stdoutPath, []byte(tc.stdout), 0o644); err != nil {
				t.Fatalf("write stdout: %v", err)
			}

			artifacts := Artifacts{Stdout: stdoutPath}
			if tc.stderr != "" {
				stderrPath := filepath.Join(tempDir, "stderr.txt")
				if err := os.WriteFile(stderrPath, []byte(tc.stderr), 0o644); err != nil {
					t.Fatalf("write stderr: %v", err)
				}
				artifacts.Stderr = stderrPath
			}

			startedAt := time.Date(2026, time.March, 29, 10, 0, 0, 0, time.UTC)
			finishedAt := startedAt.Add(tc.duration)

			block := blockForEvent(Event{
				Kind: EventStepFinished,
				Step: &Step{
					Type:       tc.stepType,
					Status:     StepStatusSucceeded,
					StartedAt:  startedAt,
					FinishedAt: finishedAt,
					Artifacts:  artifacts,
					Descriptor: &DescriptorData{
						PrimaryText: tc.primaryText,
						DetailText:  tc.detailText,
					},
				},
				Snapshot: Snapshot{},
			}, streamRenderSettings{
				now:   func() time.Time { return finishedAt },
				width: 120,
			}, newStreamStyles(false))

			for _, want := range tc.wantContains {
				if !strings.Contains(block, want) {
					t.Fatalf("block = %q, want %q", block, want)
				}
			}
			for _, notWant := range tc.wantNotContain {
				if strings.Contains(block, notWant) {
					t.Fatalf("block = %q, should not contain %q", block, notWant)
				}
			}
		})
	}
}

func TestRenderRunningCodexShowsPromptThinkingShellBlocks(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	stdoutPath := filepath.Join(tempDir, "stdout.txt")
	content := strings.Join([]string{
		`{"timestamp":"2026-03-29T10:00:20Z","type":"item.started","item":{"id":"item_2","type":"command_execution","command":"/bin/zsh -lc \"echo first\"","status":"in_progress"}}`,
		`{"timestamp":"2026-03-29T10:00:30Z","type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"{\"$thinking\":\"Plan command execution\"}"}}`,
		`{"timestamp":"2026-03-29T10:00:40Z","type":"item.started","item":{"id":"item_3","type":"command_execution","command":"/bin/zsh -lc \"pwd\"","status":"in_progress"}}`,
		`{"timestamp":"2026-03-29T10:03:00Z","type":"turn.completed","usage":{"input_tokens":31506,"cached_input_tokens":4864,"output_tokens":595}}`,
	}, "\n")
	if err := os.WriteFile(stdoutPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write stdout: %v", err)
	}

	startedAt := time.Date(2026, time.March, 29, 10, 0, 0, 0, time.UTC)
	now := startedAt.Add(9 * time.Second)

	block := blockForEvent(Event{
		Kind: EventStepStarted,
		Step: &Step{
			Type:      "codex",
			Status:    StepStatusRunning,
			StartedAt: startedAt,
			Artifacts: Artifacts{
				Stdout: stdoutPath,
				Files:  map[string]string{"prompt": filepath.Join(tempDir, "prompt.md")},
			},
			Descriptor: &DescriptorData{
				PrimaryText: "gpt-5.3-codex:low",
				DetailText:  []string{"Implement feature X."},
			},
		},
		Snapshot: Snapshot{},
	}, streamRenderSettings{
		now:   func() time.Time { return now },
		width: 120,
	}, newStreamStyles(false))

	if !strings.Contains(block, "codex gpt-5.3-codex:low | 🢁595 🢃31.5k 🢃🢃4.9k") {
		t.Fatalf("block = %q, want token summary while running", block)
	}
	if !strings.Contains(block, "[P]rompt") {
		t.Fatalf("block = %q, want collapsed prompt line", block)
	}
	if !strings.Contains(block, "[T]hinking Plan command execution") {
		t.Fatalf("block = %q, want collapsed thinking line", block)
	}
	if !strings.Contains(block, `[S]hell /bin/zsh -lc "pwd"`) {
		t.Fatalf("block = %q, want collapsed shell line", block)
	}
}
