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

func TestSummarizeClaudeOutputAndRenderBlock(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	stdoutPath := filepath.Join(tempDir, "stdout.txt")
	content := strings.Join([]string{
		`{"timestamp":"2026-03-29T10:00:00Z","message":{"input_tokens":10,"cache_creation_input_tokens":3,"cache_read_input_tokens":2,"output_tokens":4},"content":[{"type":"thinking","thinking":"plan"}]}`,
		`{"timestamp":"2026-03-29T10:07:00Z","message":{"input_tokens":20,"cache_creation_input_tokens":1,"cache_read_input_tokens":4,"output_tokens":6},"content":[{"type":"tool_use","name":"Bash","input":{"command":"ls -la"}}]}`,
	}, "\n")
	if err := os.WriteFile(stdoutPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write stdout: %v", err)
	}

	startedAt := time.Date(2026, time.March, 29, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(8 * time.Second)

	block := blockForEvent(Event{
		Kind: EventStepFinished,
		Step: &Step{
			Type:       "claude",
			Status:     StepStatusSucceeded,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			Artifacts: Artifacts{
				Stdout: stdoutPath,
			},
			Descriptor: &DescriptorData{
				PrimaryText: "claude-sonnet-4-5:high",
				DetailText:  []string{"Review the diff."},
			},
		},
		Snapshot: Snapshot{},
	}, streamRenderSettings{
		now:   func() time.Time { return finishedAt },
		width: 120,
	}, newStreamStyles(false))

	if !strings.Contains(block, "claude claude-sonnet-4-5:high | 🢁10 🢃30 🢃🢃10") {
		t.Fatalf("block = %q, want summed token summary in headline", block)
	}
	if strings.Contains(block, "Bash | 🢁6 🢃20 🢃🢃5") {
		t.Fatalf("block = %q, should not render last action line for completed step", block)
	}
	if strings.Contains(block, "ls -la") {
		t.Fatalf("block = %q, should not render last action content for completed step", block)
	}
}

func TestSummarizeCodexOutputAndRenderBlock(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	stdoutPath := filepath.Join(tempDir, "stdout.txt")
	content := strings.Join([]string{
		`{"timestamp":"2026-03-29T10:00:00Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"}}`,
		`{"timestamp":"2026-03-29T10:02:00Z","type":"event_msg","payload":{"type":"exec_command_end","parsed_cmd":[{"name":"Bash","cmd":"pwd"}]}}`,
		`{"timestamp":"2026-03-29T10:03:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":120,"cached_input_tokens":110,"output_tokens":30},"last_token_usage":{"input_tokens":12,"cached_input_tokens":11,"output_tokens":3}}}}`,
	}, "\n")
	if err := os.WriteFile(stdoutPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write stdout: %v", err)
	}

	startedAt := time.Date(2026, time.March, 29, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(9 * time.Second)

	block := blockForEvent(Event{
		Kind: EventStepFinished,
		Step: &Step{
			Type:       "codex",
			Status:     StepStatusSucceeded,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			Artifacts: Artifacts{
				Stdout: stdoutPath,
			},
			Descriptor: &DescriptorData{
				PrimaryText: "gpt-5.4:high",
				DetailText:  []string{"Implement feature X."},
			},
		},
		Snapshot: Snapshot{},
	}, streamRenderSettings{
		now:   func() time.Time { return finishedAt },
		width: 120,
	}, newStreamStyles(false))

	if !strings.Contains(block, "codex gpt-5.4:high | 🢁30 🢃120 🢃🢃110") {
		t.Fatalf("block = %q, want total token summary in headline", block)
	}
	if strings.Contains(block, "Bash | 🢁3 🢃12 🢃🢃11") {
		t.Fatalf("block = %q, should not render last action line for completed step", block)
	}
	if strings.Contains(block, "pwd") {
		t.Fatalf("block = %q, should not render command content for completed step", block)
	}
}

func TestSummarizeCodexStderrFallbackAndRenderBlock(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	stdoutPath := filepath.Join(tempDir, "stdout.txt")
	stderrPath := filepath.Join(tempDir, "stderr.txt")
	if err := os.WriteFile(stdoutPath, []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatalf("write stdout: %v", err)
	}

	stderr := strings.Join([]string{
		"exec",
		"/bin/zsh -lc 'pwd' in /tmp succeeded in 1ms:",
		"tokens used",
		"11,496",
	}, "\n")
	if err := os.WriteFile(stderrPath, []byte(stderr), 0o644); err != nil {
		t.Fatalf("write stderr: %v", err)
	}

	startedAt := time.Date(2026, time.March, 29, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(9 * time.Second)

	block := blockForEvent(Event{
		Kind: EventStepFinished,
		Step: &Step{
			Type:       "codex",
			Status:     StepStatusSucceeded,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			Artifacts: Artifacts{
				Stdout: stdoutPath,
				Stderr: stderrPath,
			},
			Descriptor: &DescriptorData{
				PrimaryText: "gpt-5.4:high",
				DetailText:  []string{"Implement feature X."},
			},
		},
		Snapshot: Snapshot{},
	}, streamRenderSettings{
		now:   func() time.Time { return finishedAt },
		width: 120,
	}, newStreamStyles(false))

	if !strings.Contains(block, "codex gpt-5.4:high | 🢁0 🢃11.5k 🢃🢃0") {
		t.Fatalf("block = %q, want token summary from stderr fallback", block)
	}
	if strings.Contains(block, "Bash | 🢁0 🢃11.5k 🢃🢃0") {
		t.Fatalf("block = %q, should not render last action line for completed step", block)
	}
	if strings.Contains(block, "/bin/zsh -lc 'pwd'") {
		t.Fatalf("block = %q, should not render command content for completed step", block)
	}
}

func TestSummarizeClaudeResultEnvelopeAndRenderBlock(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	stdoutPath := filepath.Join(tempDir, "stdout.txt")
	content := `{"type":"result","stop_reason":"end_turn","usage":{"input_tokens":18,"cache_creation_input_tokens":24700,"cache_read_input_tokens":472210,"output_tokens":3543}}`
	if err := os.WriteFile(stdoutPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write stdout: %v", err)
	}

	startedAt := time.Date(2026, time.March, 29, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(8 * time.Second)

	block := blockForEvent(Event{
		Kind: EventStepFinished,
		Step: &Step{
			Type:       "claude",
			Status:     StepStatusSucceeded,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			Artifacts: Artifacts{
				Stdout: stdoutPath,
			},
			Descriptor: &DescriptorData{
				PrimaryText: "claude-sonnet-4-6:high",
				DetailText:  []string{"Review the diff."},
			},
		},
		Snapshot: Snapshot{},
	}, streamRenderSettings{
		now:   func() time.Time { return finishedAt },
		width: 120,
	}, newStreamStyles(false))

	if !strings.Contains(block, "claude claude-sonnet-4-6:high | 🢁3.5k 🢃18 🢃🢃496.9k") {
		t.Fatalf("block = %q, want total token summary from result envelope", block)
	}
	if strings.Contains(block, "result | 🢁3.5k 🢃18 🢃🢃496.9k") {
		t.Fatalf("block = %q, should not render last action line for completed step", block)
	}
	if strings.Contains(block, "end_turn") {
		t.Fatalf("block = %q, should not render stop reason content for completed step", block)
	}
}

func TestSummarizeCodexModernEventStreamAndRenderBlock(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	stdoutPath := filepath.Join(tempDir, "stdout.txt")
	content := strings.Join([]string{
		`{"timestamp":"2026-03-29T10:00:00Z","type":"thread.started","thread_id":"tid-1"}`,
		`{"timestamp":"2026-03-29T10:01:00Z","type":"item.started","item":{"id":"item_2","type":"command_execution","command":"/bin/zsh -lc \"pwd\"","status":"in_progress"}}`,
		`{"timestamp":"2026-03-29T10:02:00Z","type":"item.completed","item":{"id":"item_2","type":"command_execution","command":"/bin/zsh -lc \"pwd\"","status":"completed"}}`,
		`{"timestamp":"2026-03-29T10:03:00Z","type":"turn.completed","usage":{"input_tokens":31506,"cached_input_tokens":4864,"output_tokens":595}}`,
	}, "\n")
	if err := os.WriteFile(stdoutPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write stdout: %v", err)
	}

	startedAt := time.Date(2026, time.March, 29, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(9 * time.Second)

	block := blockForEvent(Event{
		Kind: EventStepFinished,
		Step: &Step{
			Type:       "codex",
			Status:     StepStatusSucceeded,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			Artifacts: Artifacts{
				Stdout: stdoutPath,
			},
			Descriptor: &DescriptorData{
				PrimaryText: "gpt-5.3-codex:low",
				DetailText:  []string{"Implement feature X."},
			},
		},
		Snapshot: Snapshot{},
	}, streamRenderSettings{
		now:   func() time.Time { return finishedAt },
		width: 120,
	}, newStreamStyles(false))

	if !strings.Contains(block, "codex gpt-5.3-codex:low | 🢁595 🢃31.5k 🢃🢃4.9k") {
		t.Fatalf("block = %q, want token summary from turn.completed usage", block)
	}
	if strings.Contains(block, "Bash | 🢁0 🢃0 🢃🢃0") {
		t.Fatalf("block = %q, should not render last action line for completed step", block)
	}
	if strings.Contains(block, "/bin/zsh -lc \"pwd\"") {
		t.Fatalf("block = %q, should not render command content for completed step", block)
	}
}

func TestRenderRunningAgentShowsLastActionBelowPrompt(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	stdoutPath := filepath.Join(tempDir, "stdout.txt")
	content := strings.Join([]string{
		`{"timestamp":"2026-03-29T10:00:00Z","type":"item.started","item":{"id":"item_2","type":"command_execution","command":"/bin/zsh -lc \"pwd\"","status":"in_progress"}}`,
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
	if !strings.Contains(block, "Bash") {
		t.Fatalf("block = %q, want running tool line", block)
	}
	if strings.Contains(block, "Bash | 🢁0 🢃0 🢃🢃0") {
		t.Fatalf("block = %q, should not render empty token triplet", block)
	}
	if !strings.Contains(block, "/bin/zsh -lc \"pwd\"") {
		t.Fatalf("block = %q, want last action content while running", block)
	}
}
