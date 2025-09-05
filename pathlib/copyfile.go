package pathlib

import (
	"io"
	"os"
	"path/filepath"

	"github.com/robocorp/rcc/common"
)

type copyfunc func(io.Writer, io.Reader) (int64, error)

type Copier func(string, string, bool) error

func CopyFile(source, target string, overwrite bool) error {
	mark, err := Modtime(source)
	if err != nil {
		return err
	}
	err = copyFile(source, target, overwrite, io.Copy)
	TouchWhen(target, mark)
	return err
}

func copyFile(source, target string, overwrite bool, copier copyfunc) error {
	_, err := shared.MakeSharedDir(filepath.Dir(target))
	if err != nil {
		return err
	}
	if overwrite && Exists(target) {
		err = os.Remove(target)
	}
	if err != nil {
		return err
	}
	readable, err := os.Open(source)
	if err != nil {
		return err
	}
	defer readable.Close()
	stats, err := readable.Stat()
	if err != nil {
		return err
	}
	writable, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, stats.Mode())
	if err != nil {
		return err
	}
	defer writable.Close()

	_, err = copier(writable, readable)
	if err != nil {
		common.Error("copy-file", err)
	}

	return err
}
