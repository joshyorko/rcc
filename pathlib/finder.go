package pathlib

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	emptyString = ""
)

func FindNamedPath(basedir, name string) (string, error) {
	fullpath, err := filepath.Abs(basedir)
	if err != nil {
		return emptyString, err
	}
	pending := make([]string, 0, 20)
	pending = append(pending, fullpath, emptyString)
	result := make([]string, 0, 10)
	for len(pending) > 0 {
		next := pending[0]
		pending = pending[1:]
		if next == emptyString {
			if len(result) == 1 {
				return result[0], nil
			}
			if len(result) > 1 {
				return emptyString, fmt.Errorf("Found %d files named as '%s'. Expecting exactly one. %s", len(result), name, result)
			}
			if len(pending) > 0 {
				pending = append(pending, emptyString)
			}
			continue
		}
		handle, err := os.Open(next)
		if err != nil {
			return emptyString, err
		}
		entries, err := handle.Readdir(-1)
		if err != nil {
			return emptyString, err
		}
		handle.Close()
		for _, entry := range entries {
			fullpath := filepath.Join(next, entry.Name())
			if entry.Name() == name {
				result = append(result, fullpath)
			}
			if entry.IsDir() {
				pending = append(pending, fullpath)
			}
		}
	}
	return emptyString, fmt.Errorf("Could not find path named '%s'.", name)
}
