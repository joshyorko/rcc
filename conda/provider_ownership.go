package conda

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type providerFile struct {
	mode   os.FileMode
	digest [32]byte
	info   os.FileInfo
}

func snapshotProviderFiles(root string) (map[string]providerFile, error) {
	files := make(map[string]providerFile)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		entry := providerFile{mode: info.Mode(), info: info}
		switch {
		case info.Mode().IsRegular():
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			h := sha256.New()
			_, err = io.Copy(h, f)
			closeErr := f.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			copy(entry.digest[:], h.Sum(nil))
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entry.digest = sha256.Sum256([]byte(target))
		case info.IsDir():
		default:
			return fmt.Errorf("unsupported staged file type: %s", rel)
		}
		files[rel] = entry
		return nil
	})
	return files, err
}

func verifyProviderOwnership(root string, before map[string]providerFile) error {
	after, err := snapshotProviderFiles(root)
	if err != nil {
		return err
	}
	for path, original := range before {
		if current, ok := after[path]; !ok || current.mode != original.mode || current.digest != original.digest || !os.SameFile(current.info, original.info) {
			return fmt.Errorf("PyPI install changed preexisting path %s", path)
		}
	}
	absoluteRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	absoluteRoot, err = filepath.Abs(absoluteRoot)
	if err != nil {
		return err
	}
	for path, entry := range after {
		if _, existed := before[path]; existed || entry.mode&os.ModeSymlink == 0 {
			continue
		}
		resolved, err := filepath.EvalSymlinks(filepath.Join(root, path))
		if err != nil {
			return fmt.Errorf("invalid new symlink %s: %w", path, err)
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(absoluteRoot, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("new symlink escapes prefix: %s", path)
		}
	}
	return nil
}

var providerNameSeparator = regexp.MustCompile(`[-_.]+`)

func providerPackageName(requirement string) string {
	name := strings.SplitN(requirement, "=", 2)[0]
	name = strings.SplitN(name, "<", 2)[0]
	name = strings.SplitN(name, ">", 2)[0]
	name = strings.SplitN(name, "!", 2)[0]
	name = strings.SplitN(name, "~", 2)[0]
	return providerNameSeparator.ReplaceAllString(strings.ToLower(name), "-")
}

func checkProviderCollisions(root string, packages []string) error {
	owned := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(root, "conda-meta"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, "conda-meta", entry.Name()))
		if err != nil {
			return err
		}
		var meta struct {
			Name string `json:"name"`
		}
		if err = json.Unmarshal(data, &meta); err != nil {
			return err
		}
		if meta.Name != "" {
			owned[providerPackageName(meta.Name)] = true
		}
	}
	files, err := snapshotProviderFiles(root)
	if err != nil {
		return err
	}
	for path := range files {
		base := filepath.Base(path)
		lower := strings.ToLower(base)
		if strings.HasSuffix(lower, ".dist-info") || strings.HasSuffix(lower, ".egg-info") {
			name := strings.SplitN(base, "-", 2)[0]
			owned[providerPackageName(name)] = true
		}
	}
	for _, pkg := range packages {
		name := providerPackageName(pkg)
		if owned[name] {
			return fmt.Errorf("PyPI package %s collides with an existing package", name)
		}
		owned[name] = true
	}
	return nil
}
