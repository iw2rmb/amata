package runtime_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	runtimelib "github.com/iw2rmb/amata/internal/runtime"
	"github.com/iw2rmb/amata/internal/state"
)

func TestReferenceWorkflowHelperProcess(t *testing.T) {
	if os.Getenv("AMATA_REFERENCE_WORKFLOW_HELPER") != "1" {
		return
	}

	if err := os.Chdir(os.Getenv("AMATA_REFERENCE_WORKFLOW_CWD")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("AMATA_REFERENCE_WORKFLOW_ARGS")), &args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := runtimelib.RunCLI(args, nil, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestReferenceWorkflowSmoke(t *testing.T) {
	harness := setupReferenceWorkflowHarness(t, "")

	if err := runtimelib.RunCLI([]string{
		"run",
		harness.specPath,
		"--workspace", harness.repoDir,
		"--run-id", "run-smoke",
		"--set", "roadmap_file=" + harness.roadmapRelPath,
	}, nil, nil); err != nil {
		t.Fatalf("run cli: %v", err)
	}

	store := state.NewStore(filepath.Join(harness.repoDir, ".amata", "runs", "run-smoke"))
	snapshot, err := store.LoadSnapshot()
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot.Status != state.RunStatusSucceeded {
		t.Fatalf("snapshot status = %q, want succeeded", snapshot.Status)
	}

	if _, err := os.Stat(filepath.Join(harness.repoDir, harness.roadmapRelPath)); !os.IsNotExist(err) {
		t.Fatalf("roadmap file err = %v, want removed after docs cleanup", err)
	}

	currentState := readWorkflowFile(t, filepath.Join(harness.repoDir, "docs", "current-state.md"))
	for _, want := range []string{
		"The first fixture change is implemented.",
		"The workflow uses built-in executors only.",
	} {
		if !strings.Contains(currentState, want) {
			t.Fatalf("current-state.md missing %q:\n%s", want, currentState)
		}
	}

	commits := gitLogSubjects(t, harness.repoDir)
	wantPrefixes := []string{
		"docs: remove completed roadmap",
		"fixture: validate built-in executor wiring",
		"fixture: implement first fixture change",
	}
	for index, want := range wantPrefixes {
		if index >= len(commits) || commits[index] != want {
			t.Fatalf("git log subject[%d] = %#v, want %q (full log=%#v)", index, commits, want, commits)
		}
	}
}

func TestReferenceWorkflowResumeDoesNotReplayCommittedWork(t *testing.T) {
	harness := setupReferenceWorkflowHarness(t, "1.2")

	args, err := json.Marshal([]string{
		"run",
		harness.specPath,
		"--workspace", harness.repoDir,
		"--run-id", "run-resume",
		"--set", "roadmap_file=" + harness.roadmapRelPath,
	})
	if err != nil {
		t.Fatalf("marshal helper args: %v", err)
	}

	var stderr bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=^TestReferenceWorkflowHelperProcess$")
	cmd.Env = append(os.Environ(),
		"AMATA_REFERENCE_WORKFLOW_HELPER=1",
		"AMATA_REFERENCE_WORKFLOW_CWD="+harness.repoDir,
		"AMATA_REFERENCE_WORKFLOW_ARGS="+string(args),
	)
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}

	waitForWorkflowBlock(t, harness.repoDir, filepath.Join(harness.repoDir, ".amata", "fake-agent", "waiting-1.2"), "fixture: implement first fixture change", &stderr)

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill helper process: %v", err)
	}
	_ = cmd.Wait()

	if got := countSubject(gitLogSubjects(t, harness.repoDir), "fixture: implement first fixture change"); got != 1 {
		t.Fatalf("first item commit count after interruption = %d, want 1", got)
	}
	if got := countLogLine(readWorkflowFile(t, filepath.Join(harness.repoDir, ".amata", "fake-agent", "invocations.log")), "codex:implement:1.1"); got != 1 {
		t.Fatalf("item 1.1 implement count after interruption = %d, want 1", got)
	}

	if err := os.WriteFile(filepath.Join(harness.repoDir, ".amata", "fake-agent", "allow-1.2"), []byte("go\n"), 0o644); err != nil {
		t.Fatalf("write unblock marker: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(harness.repoDir); err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	})

	if err := runtimelib.RunCLI([]string{"resume", "run-resume"}, nil, nil); err != nil {
		t.Fatalf("resume cli: %v", err)
	}

	store := state.NewStore(filepath.Join(harness.repoDir, ".amata", "runs", "run-resume"))
	snapshot, err := store.LoadSnapshot()
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot.Status != state.RunStatusSucceeded {
		t.Fatalf("snapshot status = %q, want succeeded", snapshot.Status)
	}

	if got := countSubject(gitLogSubjects(t, harness.repoDir), "fixture: implement first fixture change"); got != 1 {
		t.Fatalf("first item commit count after resume = %d, want 1", got)
	}
	if got := countSubject(gitLogSubjects(t, harness.repoDir), "fixture: validate built-in executor wiring"); got != 1 {
		t.Fatalf("second item commit count after resume = %d, want 1", got)
	}
	if got := countSubject(gitLogSubjects(t, harness.repoDir), "docs: remove completed roadmap"); got != 1 {
		t.Fatalf("docs cleanup commit count after resume = %d, want 1", got)
	}

	invocations := readWorkflowFile(t, filepath.Join(harness.repoDir, ".amata", "fake-agent", "invocations.log"))
	if got := countLogLine(invocations, "codex:implement:1.1"); got != 1 {
		t.Fatalf("item 1.1 implement count after resume = %d, want 1", got)
	}
	if got := countLogLine(invocations, "codex:implement:1.2"); got != 2 {
		t.Fatalf("item 1.2 implement count after resume = %d, want 2", got)
	}
}

type referenceWorkflowHarness struct {
	specPath       string
	repoDir        string
	roadmapRelPath string
}

func setupReferenceWorkflowHarness(t *testing.T, blockLabel string) referenceWorkflowHarness {
	t.Helper()

	repoRoot := testRepositoryRoot(t)
	specPath := filepath.Join(repoRoot, "tests", "fixtures", "reference-workflow", "implement-roadmap.yaml")
	fixtureSource := filepath.Join(repoRoot, "tests", "fixtures", "reference-workflow", "fixture-repo")

	repoDir := t.TempDir()
	copyFixtureTree(t, fixtureSource, repoDir)
	initWorkflowRepository(t, repoDir)

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir fake agent bin dir: %v", err)
	}
	writeExecutable(t, filepath.Join(binDir, "codex"), fakeAgentScript)
	writeExecutable(t, filepath.Join(binDir, "claude"), fakeAgentScript)

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMATA_FAKE_ROADMAP_FILE", "roadmap/index.md")
	t.Setenv("AMATA_FAKE_BLOCK_LABEL", blockLabel)

	return referenceWorkflowHarness{
		specPath:       specPath,
		repoDir:        repoDir,
		roadmapRelPath: "roadmap/index.md",
	}
}

func testRepositoryRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatalf("resolve test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func copyFixtureTree(t *testing.T, src string, dst string) {
	t.Helper()

	if err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relative, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		t.Fatalf("copy fixture tree: %v", err)
	}
}

func initWorkflowRepository(t *testing.T, repoDir string) {
	t.Helper()

	runWorkflowGit(t, repoDir, "init")
	runWorkflowGit(t, repoDir, "config", "user.name", "Test User")
	runWorkflowGit(t, repoDir, "config", "user.email", "test@example.com")
	runWorkflowGit(t, repoDir, "add", ".")
	runWorkflowGit(t, repoDir, "commit", "-m", "init")
}

func writeExecutable(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func waitForWorkflowBlock(t *testing.T, repoDir string, waitingPath string, committedSubject string, helperStderr *bytes.Buffer) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(waitingPath); err == nil {
			if countSubject(gitLogSubjects(t, repoDir), committedSubject) == 1 {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for workflow block (stderr=%q)", helperStderr.String())
}

func gitLogSubjects(t *testing.T, repoDir string) []string {
	t.Helper()

	output := strings.TrimSpace(runWorkflowGit(t, repoDir, "log", "--format=%s"))
	if output == "" {
		return nil
	}
	return strings.Split(output, "\n")
}

func countSubject(subjects []string, want string) int {
	count := 0
	for _, subject := range subjects {
		if subject == want {
			count++
		}
	}
	return count
}

func countLogLine(text string, want string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line == want {
			count++
		}
	}
	return count
}

func readWorkflowFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func runWorkflowGit(t *testing.T, repoDir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return string(output)
}

const fakeAgentScript = `#!/usr/bin/env python3
import json
import os
import sys
import time


def repo_root():
    return os.getcwd()


def agent_state_dir(root):
    path = os.path.join(root, '.amata', 'fake-agent')
    os.makedirs(path, exist_ok=True)
    return path


def append_log(root, entry):
    with open(os.path.join(agent_state_dir(root), 'invocations.log'), 'a', encoding='utf-8') as handle:
        handle.write(entry + '\n')


def roadmap_path(root):
    return os.path.join(root, os.environ['AMATA_FAKE_ROADMAP_FILE'])


def mark_item_done(root, label):
    path = roadmap_path(root)
    with open(path, encoding='utf-8') as handle:
        text = handle.read()
    replacements = {
        '1.1': '- [ ] 1.1 Implement the first fixture change',
        '1.2': '- [ ] 1.2 Validate built-in executor wiring',
    }
    marker = replacements[label]
    if marker in text:
        text = text.replace(marker, marker.replace('[ ]', '[x]'), 1)
    with open(path, 'w', encoding='utf-8') as handle:
        handle.write(text)


def ensure_doc_line(root, line):
    path = os.path.join(root, 'docs', 'current-state.md')
    with open(path, encoding='utf-8') as handle:
        text = handle.read()
    if line not in text:
        if not text.endswith('\n'):
            text += '\n'
        text += '- ' + line + '\n'
    with open(path, 'w', encoding='utf-8') as handle:
        handle.write(text)


def maybe_block(root, label):
    block_label = os.environ.get('AMATA_FAKE_BLOCK_LABEL', '')
    if label != block_label:
        return
    state_dir = agent_state_dir(root)
    allow_path = os.path.join(state_dir, 'allow-' + label)
    if os.path.exists(allow_path):
        return
    waiting_path = os.path.join(state_dir, 'waiting-' + label)
    append_log(root, 'codex:implement:' + label + ':blocking')
    with open(waiting_path, 'w', encoding='utf-8') as handle:
        handle.write('waiting\n')
    while not os.path.exists(allow_path):
        time.sleep(0.05)


def codex_output_path(argv):
    for index, value in enumerate(argv):
        if value == '-o' and index + 1 < len(argv):
            return argv[index + 1]
    raise SystemExit('missing codex output path')


def write_payload(provider, payload):
    text = json.dumps(payload, sort_keys=True) + '\n'
    if provider == 'codex':
        with open(codex_output_path(sys.argv[1:]), 'w', encoding='utf-8') as handle:
            handle.write(text)
    else:
        sys.stdout.write(text)


def handle_codex(root, prompt):
    if 'Implement next open item from the' in prompt and '1.1 Implement the first fixture change' in prompt:
        append_log(root, 'codex:implement:1.1')
        ensure_doc_line(root, 'The first fixture change is implemented.')
        mark_item_done(root, '1.1')
        return {
            'itemTitle': '1.1 Implement the first fixture change',
            'itemLabel': '1.1',
            'commitMessage': 'fixture: implement first fixture change',
            'reviewReasoning': 'medium',
            'summary': 'Updated the current-state docs.',
        }
    if 'Implement next open item from the' in prompt and '1.2 Validate built-in executor wiring' in prompt:
        append_log(root, 'codex:implement:1.2')
        maybe_block(root, '1.2')
        ensure_doc_line(root, 'The workflow uses built-in executors only.')
        mark_item_done(root, '1.2')
        return {
            'itemTitle': '1.2 Validate built-in executor wiring',
            'itemLabel': '1.2',
            'commitMessage': 'fixture: validate built-in executor wiring',
            'reviewReasoning': 'high',
            'summary': 'Confirmed the workflow uses built-in executors.',
        }
    if 'Review the current uncommitted diff for the selected roadmap item.' in prompt and '1.1 Implement the first fixture change' in prompt:
        append_log(root, 'codex:review:1.1')
        return {
            'approved': True,
            'notes': 'ready',
            'commitMessage': 'fixture: implement first fixture change',
            'itemTitle': '1.1 Implement the first fixture change',
            'itemLabel': '1.1',
        }
    if 'Review the current uncommitted diff for the selected roadmap item.' in prompt and '1.2 Validate built-in executor wiring' in prompt:
        append_log(root, 'codex:review:1.2')
        return {
            'approved': True,
            'notes': 'ready',
            'commitMessage': 'fixture: validate built-in executor wiring',
            'itemTitle': '1.2 Validate built-in executor wiring',
            'itemLabel': '1.2',
        }
    if 'Confirm by inspecting the codebase, tests, and current documentation' in prompt:
        append_log(root, 'codex:correctness')
        return {'approved': True, 'notes': 'wired end-to-end'}
    if 'Review the current uncommitted diff.' in prompt and '"commitMessage"' in prompt:
        append_log(root, 'codex:sanity')
        return {'approved': True, 'notes': 'no remaining diff issues', 'commitMessage': 'fixture: finalize workflow'}
    if 'Update documentation for the completed roadmap work.' in prompt:
        append_log(root, 'codex:docs')
        path = roadmap_path(root)
        if os.path.exists(path):
            os.remove(path)
        return {'commitMessage': 'docs: remove completed roadmap'}
    raise SystemExit('unsupported codex prompt: ' + prompt)


def handle_claude(root, prompt):
    if 'Review the codebase related to the implemented roadmap items.' in prompt:
        append_log(root, 'claude:refactor')
        return {'approved': True, 'notes': 'no refactors needed'}
    raise SystemExit('unsupported claude prompt: ' + prompt)


def main():
    provider = os.path.basename(sys.argv[0])
    root = repo_root()
    prompt = sys.stdin.read()
    if provider == 'codex':
        payload = handle_codex(root, prompt)
    elif provider == 'claude':
        payload = handle_claude(root, prompt)
    else:
        raise SystemExit('unsupported provider: ' + provider)
    write_payload(provider, payload)


if __name__ == '__main__':
    main()
`
