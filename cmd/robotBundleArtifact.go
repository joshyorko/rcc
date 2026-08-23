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

func importBundleArtifact(zr *zip.Reader) (environmentartifact.Manifest, error) {
	var imported environmentartifact.Manifest
	var platformIndex *environmentartifact.PlatformIndex
	for _, file := range zr.File {
		if file.Name == "environment/platform-index.json" {
			reader, err := file.Open()
			if err != nil {
				return imported, err
			}
			content, readErr := io.ReadAll(io.LimitReader(reader, environmentartifact.MaxArchiveSize+1))
			closeErr := reader.Close()
			if readErr != nil {
				return imported, readErr
			}
			if closeErr != nil {
				return imported, closeErr
			}
			if int64(len(content)) > environmentartifact.MaxArchiveSize {
				return imported, fmt.Errorf("bundled platform index exceeds archive limit")
			}
			decoded, decodeErr := environmentartifact.DecodePlatformIndex(content)
			if decodeErr != nil {
				return imported, fmt.Errorf("decode bundled platform index: %w", decodeErr)
			}
			platformIndex = &decoded
			continue
		}
		if file.Name != "environment/artifact.rcca" {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return imported, err
		}
		temporary, err := os.CreateTemp("", "rcc-bundle-artifact-*.rcca")
		if err != nil {
			_ = reader.Close()
			return imported, err
		}
		name := temporary.Name()
		_, copyErr := io.Copy(temporary, io.LimitReader(reader, environmentartifact.MaxArchiveSize+1))
		closeErr := reader.Close()
		fileCloseErr := temporary.Close()
		defer os.Remove(name)
		if copyErr != nil {
			return imported, copyErr
		}
		if closeErr != nil {
			return imported, closeErr
		}
		if fileCloseErr != nil {
			return imported, fileCloseErr
		}
		if info, err := os.Stat(name); err != nil {
			return imported, err
		} else if info.Size() > environmentartifact.MaxArchiveSize {
			return imported, fmt.Errorf("bundled environment artifact exceeds %d bytes", environmentartifact.MaxArchiveSize)
		}
		manifest, err := environmentlifecycle.ImportArchive(context.Background(), environmentlifecycle.ImportArchiveRequest{Path: name})
		if err != nil {
			return imported, fmt.Errorf("import bundled artifact: %w", err)
		}
		if imported.ArtifactDigest.Hex() != "" && imported.ArtifactDigest != manifest.ArtifactDigest {
			return imported, fmt.Errorf("bundle contains multiple environment artifacts")
		}
		imported = manifest
	}
	if platformIndex != nil {
		if imported.ArtifactDigest.Hex() == "" {
			return imported, fmt.Errorf("platform index requires an embedded artifact")
		}
		if platformIndex.Specification != imported.Specification.Descriptor.Digest {
			return imported, fmt.Errorf("bundled platform index specification does not match artifact")
		}
		selected, err := platformIndex.Select(environmentartifact.CurrentPlatform())
		if err != nil {
			return imported, fmt.Errorf("select bundled platform artifact: %w", err)
		}
		if selected != imported.ArtifactDigest {
			return imported, fmt.Errorf("bundled platform index selected %s, embedded artifact is %s", selected, imported.ArtifactDigest)
		}
	}
	return imported, nil
}
