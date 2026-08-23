package cmd

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func safeBundleTarget(dest, name string) (string, error) {
	if strings.Contains(name, `\`) {
		return "", fmt.Errorf("zip entry path %q contains a non-portable path separator", name)
	}
	cleanName := path.Clean(name)
	if cleanName == "." || path.IsAbs(cleanName) || cleanName == ".." || strings.HasPrefix(cleanName, "../") {
		return "", fmt.Errorf("zip entry path %q is unsafe", name)
	}

	absDest, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	target := filepath.Join(absDest, filepath.FromSlash(cleanName))
	rel, err := filepath.Rel(absDest, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("zip entry path %q would be extracted outside the destination directory", name)
	}
	return target, nil
}

func ensureSafeBundleParents(dest, target string) error {
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	relParent, err := filepath.Rel(absDest, filepath.Dir(target))
	if err != nil {
		return err
	}

	current := absDest
	if relParent == "." {
		return nil
	}
	for _, component := range strings.Split(relParent, string(os.PathSeparator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		switch {
		case err == nil && info.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("refusing to extract through symbolic link %q", current)
		case err == nil && !info.IsDir():
			return fmt.Errorf("refusing to extract through non-directory %q", current)
		case err == nil:
			continue
		case os.IsNotExist(err):
			if err := os.Mkdir(current, 0o755); err != nil {
				return err
			}
		default:
			return err
		}
	}
	return nil
}

func writeBundleFile(f *zip.File, dest string) error {
	if f.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to extract symbolic link %q", f.Name)
	}
	if !f.Mode().IsRegular() {
		return fmt.Errorf("refusing to extract non-regular file %q", f.Name)
	}

	target, err := safeBundleTarget(dest, strings.TrimPrefix(filepath.ToSlash(f.Name), "robot/"))
	if err != nil {
		return err
	}
	if err := ensureSafeBundleParents(dest, target); err != nil {
		return err
	}
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to replace symbolic link %q", target)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	rc, err := f.Open()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		_ = rc.Close()
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		_ = out.Close()
		_ = rc.Close()
		return err
	}
	if err := out.Close(); err != nil {
		_ = rc.Close()
		return err
	}
	return rc.Close()
}

func copyBundleArtifacts(artifactDir, workarea, projectRoot string) error {
	source, err := safeBundleTarget(workarea, artifactDir)
	if err != nil {
		return err
	}
	projectRoot, err = filepath.Abs(projectRoot)
	if err != nil {
		return err
	}
	if err := ensureExistingSafeBundleParents(workarea, source); err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("artifact source %q is not a directory", artifactDir)
	}
	relPath, err := filepath.Rel(filepath.Clean(workarea), source)
	if err != nil {
		return err
	}
	targetRoot, err := safeBundleTarget(projectRoot, filepath.ToSlash(relPath))
	if err != nil {
		return err
	}
	if err := ensureExistingSafeBundleParents(projectRoot, targetRoot); err != nil {
		return err
	}
	return filepath.Walk(source, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		destination := targetRoot
		if rel != "." {
			destination = filepath.Join(targetRoot, rel)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to copy symbolic link %q", current)
		}
		if info.IsDir() {
			return ensureBundleDirectory(destination, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to copy non-regular artifact %q", current)
		}
		return copyBundleFileNoOverwrite(current, destination, info.Mode().Perm())
	})
}

func ensureExistingSafeBundleParents(root, target string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("bundle root %q is not a directory", root)
	}
	rel, err := filepath.Rel(root, filepath.Dir(target))
	if err != nil {
		return err
	}
	current := root
	if rel == "." {
		return nil
	}
	for _, component := range strings.Split(rel, string(os.PathSeparator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("refusing to copy through unsafe bundle parent %q", current)
		}
	}
	return nil
}

func ensureBundleDirectory(target string, mode os.FileMode) error {
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("refusing to replace existing artifact target %q", target)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Mkdir(target, mode)
}

func copyBundleFileNoOverwrite(source, target string, mode os.FileMode) error {
	if err := ensureExistingSafeBundleParents(filepath.Dir(target), target); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		_ = in.Close()
		return fmt.Errorf("refusing to replace existing artifact target %q: %w", target, err)
	}
	_, copyErr := io.Copy(out, in)
	inCloseErr := in.Close()
	if copyErr != nil {
		_ = out.Close()
		_ = os.Remove(target)
		return copyErr
	}
	if inCloseErr != nil {
		_ = out.Close()
		_ = os.Remove(target)
		return inCloseErr
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(target)
		return err
	}
	return nil
}

// extractRobotTree extracts all files under the 'robot/' directory from the zip archive
// represented by zr to the destination path dest. It returns an error if no 'robot/' directory
// is found in the archive.
func extractRobotTree(zr *zip.Reader, dest string) error {
	info, err := os.Lstat(dest)
	switch {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("refusing to extract into symbolic link %q", dest)
	case err == nil && !info.IsDir():
		return fmt.Errorf("refusing to extract into non-directory %q", dest)
	case err == nil:
	case os.IsNotExist(err):
		if err := os.Mkdir(dest, 0o755); err != nil {
			return err
		}
	default:
		return err
	}

	found := false
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		if strings.HasPrefix(name, "robot/") {
			found = true
			relPath := strings.TrimPrefix(name, "robot/")
			if relPath == "" {
				continue
			}
			if f.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing to extract symbolic link %q", f.Name)
			}
			if strings.HasSuffix(relPath, "/") {
				if _, err := safeBundleTarget(dest, strings.TrimSuffix(relPath, "/")); err != nil {
					return err
				}
				continue
			}
			if err := writeBundleFile(f, dest); err != nil {
				return err
			}
		}
	}
	if !found {
		return fmt.Errorf("no robot/ directory found in bundle")
	}
	return nil
}

// unpackRobotTree stages extraction beside dest and only replaces dest after the
// complete robot tree has been validated and written successfully.
func unpackRobotTree(zr *zip.Reader, dest string, force bool) error {
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	parent := filepath.Dir(absDest)
	if parent == absDest {
		return fmt.Errorf("refusing to unpack over filesystem root %q", dest)
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if info, err := os.Lstat(absDest); err == nil {
		if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("output path %q exists but is not a directory", dest)
		}
		if !force {
			return fmt.Errorf("output directory %q already exists; use --force to overwrite", dest)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	stage, err := os.MkdirTemp(parent, "."+filepath.Base(absDest)+".unpack-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := extractRobotTree(zr, stage); err != nil {
		return err
	}

	if _, err := os.Lstat(absDest); os.IsNotExist(err) {
		return os.Rename(stage, absDest)
	} else if err != nil {
		return err
	} else if !force {
		return fmt.Errorf("output directory %q already exists; use --force to overwrite", dest)
	}

	backup, err := os.MkdirTemp(parent, "."+filepath.Base(absDest)+".previous-")
	if err != nil {
		return err
	}
	if err := os.Remove(backup); err != nil {
		return err
	}
	if err := os.Rename(absDest, backup); err != nil {
		return err
	}
	if err := os.Rename(stage, absDest); err != nil {
		if rollbackErr := os.Rename(backup, absDest); rollbackErr != nil {
			return fmt.Errorf("replace destination: %w (rollback failed: %v)", err, rollbackErr)
		}
		return err
	}
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove previous destination %q: %w", backup, err)
	}
	return nil
}
