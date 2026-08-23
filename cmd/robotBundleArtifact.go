package cmd

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
)

func importBundleArtifact(zr *zip.Reader) error {
	for _, file := range zr.File {
		if file.Name != "environment/artifact.rcca" { continue }
		reader, err := file.Open(); if err != nil { return err }
		temporary, err := os.CreateTemp("", "rcc-bundle-artifact-*.rcca"); if err != nil { _ = reader.Close(); return err }
		name := temporary.Name()
		_, copyErr := io.Copy(temporary, io.LimitReader(reader, environmentartifact.MaxArchiveSize+1))
		closeErr := reader.Close(); fileCloseErr := temporary.Close()
		defer os.Remove(name)
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if fileCloseErr != nil {
			return fileCloseErr
		}
		if info, err := os.Stat(name); err != nil {
			return err
		} else if info.Size() > environmentartifact.MaxArchiveSize {
			return fmt.Errorf("bundled environment artifact exceeds %d bytes", environmentartifact.MaxArchiveSize)
		}
		if _, err := environmentlifecycle.ImportArchive(context.Background(), environmentlifecycle.ImportArchiveRequest{Path: name}); err != nil { return fmt.Errorf("import bundled artifact: %w", err) }
	}
	return nil
}
