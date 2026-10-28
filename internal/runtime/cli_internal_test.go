package runtime

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/iw2rmb/amata/internal/progress"
)

func TestRunCLIDoesNotInitializeProgressControllerForInvalidCommand(t *testing.T) {
	var controllerCalls int
	option := CLIOption(func(options *cliOptions) {
		options.progressControllerFactory = func(io.Writer) (progress.Sink, io.Closer, error) {
			controllerCalls++
			return progress.SinkFunc(func(progress.Event) {}), noopCloser{}, nil
		}
	})

	err := RunCLI([]string{"unknown"}, &bytes.Buffer{}, &bytes.Buffer{}, option)
	if err == nil {
		t.Fatalf("RunCLI succeeded, want unknown command error")
	}
	if controllerCalls != 0 {
		t.Fatalf("progress controller calls = %d, want 0", controllerCalls)
	}
}

// Version output must work without a workflow or files in the current directory.
func TestRunCLIVersionPrintsReleaseVersion(t *testing.T) {
	release, err := os.ReadFile("../../VERSION")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	var stdout, stderr bytes.Buffer
	option := CLIOption(func(options *cliOptions) {
		options.progressControllerFactory = func(io.Writer) (progress.Sink, io.Closer, error) {
			t.Fatal("version must not initialize progress")
			return nil, nil, nil
		}
	})
	if err := RunCLI([]string{"--version"}, &stdout, &stderr, option); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != strings.TrimSpace(string(release))+"\n" || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
