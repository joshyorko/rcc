package cmd

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// extractRobotTree extracts all files under the 'robot/' directory from the zip archive
// represented by zr to the destination path dest. It returns an error if no 'robot/' directory
// is found in the archive.
func extractRobotTree(zr *zip.Reader, dest string) error {
	found := false
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		if strings.HasPrefix(name, "robot/") {
			found = true
			relPath := strings.TrimPrefix(name, "robot/")
			if relPath == "" || strings.HasSuffix(relPath, "/") {
				continue
			}

			targetPath := filepath.Join(dest, relPath)
			cleanTargetPath := filepath.Clean(targetPath)
			absDest, err := filepath.Abs(dest)
			if err != nil {
				return err
			}
			absTarget, err := filepath.Abs(cleanTargetPath)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(absDest, absTarget)
			if err != nil {
				return err
			}
			if strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
				return fmt.Errorf("zip entry %q would be extracted outside the destination directory", f.Name)
			}

			if err := os.MkdirAll(filepath.Dir(cleanTargetPath), 0755); err != nil {
				return err
			}

			rc, err := f.Open()
			if err != nil {
				return err
			}

			out, err := os.Create(cleanTargetPath)
			if err != nil {
				rc.Close()
				return err
			}

			_, err = io.Copy(out, rc)
			out.Close()
			rc.Close()
			if err != nil {
				return err
			}
		}
	}
	if !found {
		return fmt.Errorf("no robot/ directory found in bundle")
	}
	return nil
}
