package environmentlifecycle

import (
	"context"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

// ExecuteInDirectoryWithStreams runs a source command in the materialized
// environment, preserving the supplied streams until the child has been reaped.
func ExecuteInDirectoryWithStreams(ctx context.Context, materializer Materializer, materialization Materialization, command []string, workingDirectory string, stdin io.Reader, stdout, stderr io.Writer) (ExecutionHandle, ChildResult, error) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	return executeWithSignalsAndStreamsInDirectory(ctx, materializer, materialization, command, signals, stdin, stdout, stderr, filepath.Clean(workingDirectory))
}
