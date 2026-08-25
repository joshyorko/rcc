package environmentartifact

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/joshyorko/rcc/htfs"
)

var windowsVolumePattern = regexp.MustCompile(`^[A-Za-z]:`)

type catalogPathSemantics bool

const (
	posixCatalogPaths   catalogPathSemantics = false
	windowsCatalogPaths catalogPathSemantics = true
)

func ValidateV12Catalog(root *htfs.Root, index ObjectIndex, producerIdentity string) error {
	if root == nil || root.Info == nil || root.Tree == nil {
		return fmt.Errorf("catalog root is incomplete")
	}
	semantics, err := catalogSemantics(root.Platform)
	if err != nil {
		return err
	}
	if producerIdentity == "" || semantics.base(root.Path) != producerIdentity || root.Identity != producerIdentity {
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
	if err := validateCatalogDirectory(root.Tree, nil, indexed, referenced, len(producerIdentity), semantics); err != nil {
		return err
	}
	for legacyID := range indexed {
		if !referenced[legacyID] {
			return fmt.Errorf("object index entry %s is not referenced by catalog", legacyID)
		}
	}
	return nil
}

func catalogSemantics(platform string) (catalogPathSemantics, error) {
	switch platform {
	case "linux_amd64", "darwin_amd64", "darwin_arm64":
		return posixCatalogPaths, nil
	case "windows_amd64":
		return windowsCatalogPaths, nil
	default:
		return posixCatalogPaths, fmt.Errorf("unsupported catalog target platform %q", platform)
	}
}

func (it catalogPathSemantics) base(value string) string {
	if it == windowsCatalogPaths {
		value = strings.ReplaceAll(value, `\`, "/")
	}
	return path.Base(value)
}

func validateCatalogDirectory(directory *htfs.Dir, components []string, indexed map[string]ObjectEntry, referenced map[string]bool, rewriteWidth int, semantics catalogPathSemantics) error {
	if directory == nil {
		return fmt.Errorf("nil catalog directory")
	}
	if !directory.IsSymlink() && directory.Mode&^(os.ModeDir|os.ModePerm) != 0 {
		return fmt.Errorf("unsupported directory mode %v", directory.Mode)
	}
	if directory.IsSymlink() {
		return validateCatalogSymlink(components, directory.Symlink, semantics)
	}
	for key, child := range directory.Dirs {
		if err := validateCatalogName(key, semantics); err != nil {
			return err
		}
		if child == nil || child.Name != key {
			return fmt.Errorf("catalog directory key/name mismatch for %q", key)
		}
		if _, collision := directory.Files[key]; collision {
			return fmt.Errorf("catalog file/directory collision at %q", key)
		}
		if err := validateCatalogDirectory(child, appendPath(components, key), indexed, referenced, rewriteWidth, semantics); err != nil {
			return err
		}
	}
	for key, file := range directory.Files {
		if err := validateCatalogName(key, semantics); err != nil {
			return err
		}
		if file == nil || file.Name != key {
			return fmt.Errorf("catalog file key/name mismatch for %q", key)
		}
		path := appendPath(components, key)
		if file.IsSymlink() {
			if err := validateCatalogSymlink(path, file.Symlink, semantics); err != nil {
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

func validateCatalogName(name string, semantics catalogPathSemantics) error {
	unsafe := name == "" || name == "." || name == ".." || strings.Contains(name, "/")
	if semantics == windowsCatalogPaths {
		unsafe = unsafe || windowsVolumePattern.MatchString(name) || strings.Contains(name, `\`)
	}
	if unsafe {
		return fmt.Errorf("unsafe catalog path component %q", name)
	}
	return nil
}

func validateCatalogSymlink(catalogPath []string, target string, semantics catalogPathSemantics) error {
	if target == "" || strings.HasPrefix(target, "/") || (semantics == windowsCatalogPaths && (windowsVolumePattern.MatchString(target) || strings.Contains(target, `\`))) {
		return fmt.Errorf("unsafe catalog symlink target %q", target)
	}
	depth := len(catalogPath) - 1
	for _, component := range strings.Split(target, "/") {
		switch component {
		case "", ".":
			continue
		case "..":
			depth--
			if depth < 0 {
				return fmt.Errorf("catalog symlink %q escapes materialization root", target)
			}
		default:
			if err := validateCatalogName(component, semantics); err != nil {
				return fmt.Errorf("unsafe catalog symlink target %q", target)
			}
			depth++
		}
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
