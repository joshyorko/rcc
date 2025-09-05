package pathlib

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/robocorp/rcc/common"
)

type PathParts []string

func noDuplicates(paths PathParts) PathParts {
	seen := make(map[string]bool)
	result := make(PathParts, 0, len(paths))
	for _, part := range paths {
		if seen[part] {
			continue
		}
		result = append(result, part)
		seen[part] = true
	}
	return result
}

func noPreviousHolotrees(paths PathParts) PathParts {
	form := fmt.Sprintf("\\b(?:%s|UNMNGED)_[0-9a-f]{7}_[0-9a-f]{8}\\b", common.SymbolicUserIdentity())
	pattern, err := regexp.Compile(form)
	if err != nil {
		return paths
	}
	result := make(PathParts, 0, len(paths))
	for _, part := range paths {
		if !pattern.MatchString(part) {
			result = append(result, part)
		}
	}
	return result
}

func TargetPath() PathParts {
	return noPreviousHolotrees(noDuplicates(filepath.SplitList(os.Getenv("PATH"))))
}

func EnvironmentPath(environment []string) PathParts {
	path := ""
	for _, entry := range environment {
		if strings.HasPrefix(strings.ToLower(entry), "path=") {
			path = entry[5:]
		}
	}
	return noDuplicates(filepath.SplitList(path))
}

func PathFrom(parts ...string) PathParts {
	if parts == nil {
		return PathParts{}
	}
	return parts
}

func (it PathParts) AsEnvironmental(name string) string {
	return fmt.Sprintf("%s=%s", name, strings.Join(it.Absolute(), string(filepath.ListSeparator)))
}

func (it PathParts) Remove(patterns []string) PathParts {
	result := make([]string, 0, len(it))
	for _, path := range it {
		reference := strings.ToLower(path)
		skip := false
		for _, ignore := range patterns {
			if strings.Index(reference, ignore) > -1 {
				skip = true
				break
			}
		}
		if !skip {
			result = append(result, path)
		}
	}
	return result
}

func (it PathParts) Append(parts ...string) PathParts {
	result := make([]string, 0, len(it)+len(parts))
	result = append(result, it...)
	result = append(result, parts...)
	return result
}

func (it PathParts) Prepend(parts ...string) PathParts {
	result := make([]string, 0, len(it)+len(parts))
	result = append(result, parts...)
	result = append(result, it...)
	return result
}

func (it PathParts) Absolute() PathParts {
	result := make([]string, 0, len(it))
	for _, item := range it {
		full, err := filepath.Abs(item)
		if err == nil {
			result = append(result, full)
		}
	}
	return result
}

func whichVariation(fullPath string, fileExtensions []string) (string, bool) {
	for _, extension := range fileExtensions {
		variation := fullPath + extension
		stat, err := os.Stat(variation)
		if err == nil && !stat.IsDir() {
			return variation, true
		}
	}
	return "", false
}

func (it PathParts) Which(application string, extensions []string) (string, bool) {
	if filepath.IsAbs(application) && IsFile(application) {
		return application, true
	}
	for _, directory := range it {
		stat, err := os.Stat(directory)
		if err != nil || !stat.IsDir() {
			continue
		}
		found, ok := whichVariation(filepath.Join(directory, application), extensions)
		if ok {
			return found, ok
		}
	}
	return "", false
}
