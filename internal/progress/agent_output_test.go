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

	if !strings.Contains(block, "claude claude-sonnet-4-5:high Out: 10 In: 30 Cached: 10") {
		t.Fatalf("block = %q, want summed token summary in headline", block)
	}
	if !strings.Contains(block, "00:07 Bash Out: 6 In: 20 Cached: 5") {
		t.Fatalf("block = %q, want last action line with per-event tokens", block)
	}
	if !strings.Contains(block, "ls -la") {
		t.Fatalf("block = %q, want last action content", block)
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

	if !strings.Contains(block, "codex gpt-5.4:high Out: 30 In: 120 Cached: 110") {
		t.Fatalf("block = %q, want total token summary in headline", block)
	}
	if !strings.Contains(block, "00:02 Bash Out: 3 In: 12 Cached: 11") {
		t.Fatalf("block = %q, want last action token line", block)
	}
	if !strings.Contains(block, "pwd") {
		t.Fatalf("block = %q, want command content", block)
	}
}
