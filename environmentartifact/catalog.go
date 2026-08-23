package environmentartifact

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/joshyorko/rcc/htfs"
)

var windowsVolumePattern = regexp.MustCompile(`^[A-Za-z]:`)

func ValidateV12Catalog(root *htfs.Root, index ObjectIndex, producerIdentity string) error {
	if root == nil || root.Info == nil || root.Tree == nil {
		return fmt.Errorf("catalog root is incomplete")
	}
	if producerIdentity == "" || filepath.Base(root.Path) != producerIdentity || root.Identity != producerIdentity {
		return fmt.Errorf("catalog producer identity mismatch")
	}
	if root.Tree.IsSymlink() {
		return fmt.Errorf("catalog root cannot be a symlink")
	}
	if err := index.Validate(); err != nil {
		return fmt.Errorf("invalid object index: %w", err)
	}
	indexed := make(map[string]ObjectEntry, len(index.Entries))
	for _, entry := range index.Entries {
		indexed[entry.LegacyObjectID] = entry
	}
	referenced := make(map[string]bool, len(index.Entries))
	if err := validateCatalogDirectory(root.Tree, nil, indexed, referenced, len(producerIdentity)); err != nil {
		return err
	}
	for legacyID := range indexed {
		if !referenced[legacyID] {
			return fmt.Errorf("object index entry %s is not referenced by catalog", legacyID)
		}
	}
	return nil
}

func validateCatalogDirectory(directory *htfs.Dir, components []string, indexed map[string]ObjectEntry, referenced map[string]bool, rewriteWidth int) error {
	if directory == nil {
		return fmt.Errorf("nil catalog directory")
	}
	if !directory.IsSymlink() && directory.Mode&^(os.ModeDir|os.ModePerm) != 0 {
		return fmt.Errorf("unsupported directory mode %v", directory.Mode)
	}
	if directory.IsSymlink() {
		return validateCatalogSymlink(components, directory.Symlink)
	}
	for key, child := range directory.Dirs {
		if err := validateCatalogName(key); err != nil {
			return err
		}
		if child == nil || child.Name != key {
			return fmt.Errorf("catalog directory key/name mismatch for %q", key)
		}
		if _, collision := directory.Files[key]; collision {
			return fmt.Errorf("catalog file/directory collision at %q", key)
		}
		if err := validateCatalogDirectory(child, appendPath(components, key), indexed, referenced, rewriteWidth); err != nil {
			return err
		}
	}
	for key, file := range directory.Files {
		if err := validateCatalogName(key); err != nil {
			return err
		}
		if file == nil || file.Name != key {
			return fmt.Errorf("catalog file key/name mismatch for %q", key)
		}
		path := appendPath(components, key)
		if file.IsSymlink() {
			if err := validateCatalogSymlink(path, file.Symlink); err != nil {
				return err
			}
			continue
		}
		if file.Mode&^os.ModePerm != 0 || file.Size < 0 {
			return fmt.Errorf("unsupported file mode or size at %q", strings.Join(path, "/"))
		}
		entry, found := indexed[file.Digest]
		if !found || entry.LogicalSize != file.Size {
			return fmt.Errorf("catalog object %s at %q is absent or conflicts with index", file.Digest, strings.Join(path, "/"))
		}
		if err := validateRewriteOffsets(file.Rewrite, file.Size, rewriteWidth); err != nil {
			return fmt.Errorf("unsafe rewrite offsets at %q: %w", strings.Join(path, "/"), err)
		}
		referenced[file.Digest] = true
	}
	return nil
}

func validateCatalogName(name string) error {
	if name == "" || name == "." || name == ".." || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || windowsVolumePattern.MatchString(name) || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("unsafe catalog path component %q", name)
	}
	return nil
}

func validateCatalogSymlink(path []string, target string) error {
	if target == "" || strings.HasPrefix(target, "/") || filepath.IsAbs(target) || filepath.VolumeName(target) != "" || windowsVolumePattern.MatchString(target) || strings.Contains(target, `\`) {
		return fmt.Errorf("unsafe catalog symlink target %q", target)
	}
	parent := path[:len(path)-1]
	parts := append([]string{"/materialization"}, parent...)
	parts = append(parts, target)
	resolved := filepath.Clean(filepath.Join(parts...))
	root := string(filepath.Separator) + "materialization"
	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return fmt.Errorf("catalog symlink %q escapes materialization root", target)
	}
	return nil
}

func validateRewriteOffsets(offsets []int64, logicalSize int64, width int) error {
	for _, offset := range offsets {
		end := offset + int64(width)
		if offset < 0 || end < offset || end > logicalSize {
			return fmt.Errorf("rewrite span %d:%d is outside logical file size %d", offset, end, logicalSize)
		}
	}
	return nil
}

func appendPath(path []string, name string) []string {
	result := make([]string, len(path), len(path)+1)
	copy(result, path)
	return append(result, name)
}
