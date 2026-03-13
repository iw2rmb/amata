package runtime

import (
	"bytes"
	"io"
	"testing"

	"auto/internal/progress"
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
