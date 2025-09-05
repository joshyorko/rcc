package pathlib

import (
	"fmt"
	"os"

	"github.com/robocorp/rcc/pretty"
)

func FileExist(name string) bool {
	stat, err := os.Stat(name)
	if err != nil {
		return false
	}
	return !stat.IsDir()
}

func EnsureDirectoryExists(directory string) error {
	_, err := EnsureDirectory(directory)
	return err
}

func EnsureEmptyDirectory(directory string) error {
	fullpath, err := EnsureDirectory(directory)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(fullpath)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("Directory %s is not empty!", fullpath)
	}
	return nil
}

func NoteDirectoryContent(context, directory string, guide bool) {
	if !IsDir(directory) {
		return
	}
	fullpath, err := Abs(directory)
	if err != nil {
		return
	}
	entries, err := os.ReadDir(fullpath)
	if err != nil {
		return
	}
	noted := false
	for _, entry := range entries {
		if entry.Name() != "journal.run" {
			pretty.Note("%s %q already has %q in it.", context, fullpath, entry.Name())
			noted = true
		}
	}
	if guide && noted {
		pretty.Highlight("Above notes mean, that there were files present in directory that was supposed to be empty!")
		pretty.Highlight("In robot development phase, it might be ok to have these files while building robot.")
		pretty.Highlight("In production robot/assistant, this might be a mistake, where development files were")
		pretty.Highlight("left inside robot.zip file. Report these to developer who made this robot/assistant.")
	}
}
