package pathlib

import (
	"os"

	"github.com/robocorp/rcc/fail"
)

type (
	Shared interface {
		MakeSharedFile(fullpath string) (string, error)
		MakeSharedDir(fullpath string) (string, error)
	}

	privateSetup uint8
	sharedSetup  uint8
)

func (it privateSetup) MakeSharedFile(fullpath string) (string, error) {
	return fullpath, nil
}

func (it privateSetup) MakeSharedDir(fullpath string) (string, error) {
	return makeModedDir(fullpath, 0750)
}

func (it sharedSetup) MakeSharedFile(fullpath string) (string, error) {
	stat, err := os.Stat(fullpath)
	fail.On(err != nil, "Failed to stat file %q, reason: %v", fullpath, err)
	return ensureCorrectMode(fullpath, stat, 0666)
}

func (it sharedSetup) MakeSharedDir(fullpath string) (string, error) {
	return makeModedDir(fullpath, 0777)
}
