package agent

import (
	"io"
	"os"
	"path/filepath"
)

// StreamCapture owns the lifecycle of stdout.txt and stderr.txt artifact files
// for a single agent step. It pre-creates both files before provider execution
// so they exist on disk throughout the step lifetime, enabling both buffered
// providers (which return bytes in Response.Stdout/Stderr) and streaming
// providers (which write to the io.Writer fields on Request directly).
type StreamCapture struct {
	stdoutPath string
	stderrPath string
	stdout     *os.File
	stderr     *os.File
}

// OpenStreamCapture creates stdout.txt and stderr.txt inside stepDir and
// returns a StreamCapture that owns both file handles. The caller must call
// Close after execution completes, regardless of whether it errors.
func OpenStreamCapture(stepDir string) (*StreamCapture, error) {
	stdoutPath := filepath.Join(stepDir, "stdout.txt")
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}

	stderrPath := filepath.Join(stepDir, "stderr.txt")
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		_ = stdout.Close()
		return nil, err
	}

	return &StreamCapture{
		stdoutPath: stdoutPath,
		stderrPath: stderrPath,
		stdout:     stdout,
		stderr:     stderr,
	}, nil
}

// StdoutWriter returns a writer backed by the pre-created stdout.txt artifact.
// Wire this into Request.StdoutWriter so streaming providers can write
// incrementally during execution.
func (c *StreamCapture) StdoutWriter() io.Writer {
	return c.stdout
}

// StderrWriter returns a writer backed by the pre-created stderr.txt artifact.
// Wire this into Request.StderrWriter so streaming providers can write
// incrementally during execution.
func (c *StreamCapture) StderrWriter() io.Writer {
	return c.stderr
}

// StdoutPath returns the absolute path to stdout.txt.
func (c *StreamCapture) StdoutPath() string {
	return c.stdoutPath
}

// StderrPath returns the absolute path to stderr.txt.
func (c *StreamCapture) StderrPath() string {
	return c.stderrPath
}

// Write appends stdout and stderr bytes to the respective artifact files.
// Buffered providers that return content in Response.Stdout/Response.Stderr
// instead of streaming use this to flush their output after execution.
// Empty slices are skipped without error.
func (c *StreamCapture) Write(stdout, stderr []byte) error {
	if len(stdout) > 0 {
		if _, err := c.stdout.Write(stdout); err != nil {
			return err
		}
	}
	if len(stderr) > 0 {
		if _, err := c.stderr.Write(stderr); err != nil {
			return err
		}
	}
	return nil
}

// Close closes both artifact files. It always attempts to close both
// regardless of individual errors and returns the first error encountered.
func (c *StreamCapture) Close() error {
	stdoutErr := c.stdout.Close()
	stderrErr := c.stderr.Close()
	if stdoutErr != nil {
		return stdoutErr
	}
	return stderrErr
}
