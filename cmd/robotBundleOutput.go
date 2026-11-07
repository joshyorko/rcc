package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshyorko/rcc/environmentlifecycle"
)

type bundleOutputWriters struct {
	stdout io.Writer
	stderr io.Writer
	close  func() error
}

func prepareBundleOutputWriters(workarea, artifactDirectory string, capture bool, stdout, stderr io.Writer) (bundleOutputWriters, error) {
	_, artifactPath, err := ensureBundleArtifactDirectory(workarea, artifactDirectory)
	if err != nil {
		return bundleOutputWriters{}, err
	}
	writers := bundleOutputWriters{stdout: stdout, stderr: stderr, close: func() error { return nil }}
	if !capture {
		return writers, nil
	}
	stdoutFile, err := os.OpenFile(filepath.Join(artifactPath, "stdout.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
	if err != nil {
		return bundleOutputWriters{}, fmt.Errorf("create bundled stdout log: %w", err)
	}
	stderrFile, err := os.OpenFile(filepath.Join(artifactPath, "stderr.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
	if err != nil {
		_ = stdoutFile.Close()
		return bundleOutputWriters{}, fmt.Errorf("create bundled stderr log: %w", err)
	}
	writers.stdout = io.MultiWriter(stdout, stdoutFile)
	writers.stderr = io.MultiWriter(stderr, stderrFile)
	writers.close = func() error {
		stdoutErr := stdoutFile.Close()
		stderrErr := stderrFile.Close()
		if stdoutErr != nil {
			return stdoutErr
		}
		return stderrErr
	}
	return writers, nil
}

func ensureBundleArtifactDirectory(workarea, artifactDirectory string) (string, string, error) {
	relative, path, err := bundleArtifactPath(workarea, artifactDirectory)
	if err != nil {
		return "", "", err
	}
	if err := ensureSafeBundleParents(workarea, path); err != nil {
		return "", "", err
	}
	if err := ensureBundleDirectory(path, 0o755); err != nil {
		return "", "", err
	}
	return relative, path, nil
}

func bundleArtifactPath(workarea, artifactDirectory string) (string, string, error) {
	root, err := filepath.Abs(workarea)
	if err != nil {
		return "", "", err
	}
	directory := artifactDirectory
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(root, directory)
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return "", "", err
	}
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("artifact directory %q is outside the bundle workarea", artifactDirectory)
	}
	if _, err := safeBundleTarget(root, filepath.ToSlash(relative)); err != nil {
		return "", "", err
	}
	return filepath.ToSlash(relative), directory, nil
}

func finalizeBundleArtifacts(workarea, artifactDirectory string) error {
	return finalizeBundleArtifactsTo(workarea, artifactDirectory, ".")
}

func finalizeBundleArtifactsTo(workarea, artifactDirectory, projectRoot string) error {
	relative, _, err := bundleArtifactPath(workarea, artifactDirectory)
	if err != nil {
		return err
	}
	return copyBundleArtifacts(relative, workarea, projectRoot)
}

func executeArtifactBundleTask(ctx context.Context, materializer environmentlifecycle.Materializer, materialization environmentlifecycle.Materialization, command []string, workarea, artifactDirectory string, capture bool, stdin io.Reader, stdout, stderr io.Writer) (child environmentlifecycle.ChildResult, err error) {
	writers, err := prepareBundleOutputWriters(workarea, artifactDirectory, capture, stdout, stderr)
	if err != nil {
		return child, err
	}
	defer func() {
		if closeErr := writers.close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close bundled output logs: %w", closeErr)
		}
	}()
	_, child, err = environmentlifecycle.ExecuteInDirectoryWithStreams(ctx, materializer, materialization, command, workarea, stdin, writers.stdout, writers.stderr)
	return child, err
}
