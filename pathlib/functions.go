package pathlib

import (
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
	"github.com/robocorp/rcc/pretty"
)

func TempDir() string {
	base := os.TempDir()
	_, err := EnsureDirectory(base)
	if err != nil {
		pretty.Warning("TempDir %q challenge, reason: %v", base, err)
	}
	return base
}

func RestrictOwnerOnly(filename string) error {
	return os.Chmod(filename, 0o600)
}

func Create(filename string) (*os.File, error) {
	_, err := EnsureParentDirectory(filename)
	if err != nil {
		return nil, fmt.Errorf("Failed to ensure that parent directories for %q exist, reason: %v", filename, err)
	}
	return os.Create(filename)
}

func WriteFile(filename string, data []byte, mode os.FileMode) error {
	if common.WarrantyVoided() {
		return nil
	}
	_, err := EnsureParentDirectory(filename)
	if err != nil {
		return fmt.Errorf("Failed to ensure that parent directories for %q exist, reason: %v", filename, err)
	}
	return os.WriteFile(filename, data, mode)
}

func Glob(directory string, pattern string) []string {
	fullpath := filepath.Join(directory, pattern)
	result, _ := filepath.Glob(fullpath)
	return result
}

func Exists(pathname string) bool {
	_, err := os.Stat(pathname)
	return !os.IsNotExist(err)
}

func Age(pathname string) uint64 {
	var milliseconds int64
	stat, err := os.Stat(pathname)
	if !os.IsNotExist(err) {
		milliseconds = time.Now().Sub(stat.ModTime()).Milliseconds()
	}
	seconds := milliseconds / 1000
	if seconds < 0 {
		return 0
	}
	return uint64(seconds)
}

func Abs(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	fullpath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(fullpath), nil
}

func Symlink(pathname string) (string, bool) {
	stat, err := os.Lstat(pathname)
	if err != nil {
		return "", false
	}
	mode := stat.Mode()
	if mode&fs.ModeSymlink == 0 {
		return "", false
	}
	name, err := os.Readlink(pathname)
	if err != nil {
		return "", false
	}
	return name, true
}

func IsDir(pathname string) bool {
	stat, err := os.Stat(pathname)
	return err == nil && stat.IsDir()
}

func IsEmptyDir(pathname string) bool {
	if !IsDir(pathname) {
		return false
	}
	content, err := os.ReadDir(pathname)
	if err != nil {
		return false
	}
	return len(content) == 0
}

func IsFile(pathname string) bool {
	stat, err := os.Stat(pathname)
	return err == nil && !stat.IsDir()
}

func DaysSinceModified(filename string) (int, error) {
	stat, err := os.Stat(filename)
	if err != nil {
		return -1, err
	}
	return common.DayCountSince(stat.ModTime()), nil
}

func Size(pathname string) (int64, bool) {
	stat, err := os.Stat(pathname)
	if err != nil {
		return 0, false
	}
	return stat.Size(), true
}

func kiloShift(size float64) float64 {
	return size / 1024.0
}

func HumaneSizer(rawsize int64) (float64, string) {
	kilos := kiloShift(float64(rawsize))
	if kilos < 1.0 {
		return float64(rawsize), "b"
	}
	megas := kiloShift(kilos)
	if megas < 1.0 {
		return kilos, "K"
	}
	gigas := kiloShift(megas)
	if gigas < 1.0 {
		return megas, "M"
	}
	return gigas, "G"
}

func HumaneSize(pathname string) string {
	rawsize, ok := Size(pathname)
	if !ok {
		return "N/A"
	}
	value, suffix := HumaneSizer(rawsize)
	return fmt.Sprintf("%3.1f%s", value, suffix)
}

func Modtime(pathname string) (time.Time, error) {
	stat, err := os.Stat(pathname)
	if err != nil {
		return time.Now(), err
	}
	return stat.ModTime(), nil
}

func TryRemove(context, target string) (err error) {
	for delay := 0; delay < 5; delay += 1 {
		time.Sleep(time.Duration(delay*100) * time.Millisecond)
		err = os.Remove(target)
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("Remove failure [%s, %s, %s], reason: %s", context, common.ControllerIdentity(), common.HolotreeSpace, err)
}

func TryRemoveAll(context, target string) (err error) {
	for delay := 0; delay < 5; delay += 1 {
		time.Sleep(time.Duration(delay*100) * time.Millisecond)
		err = os.RemoveAll(target)
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("RemoveAll failure [%s, %s, %s], reason: %s", context, common.ControllerIdentity(), common.HolotreeSpace, err)
}

func TryRename(context, source, target string) (err error) {
	for delay := 0; delay < 5; delay += 1 {
		time.Sleep(time.Duration(delay*100) * time.Millisecond)
		err = os.Rename(source, target)
		if err == nil {
			return nil
		}
	}
	common.Debug("Heads up: rename about to fail [%q -> %q], reason: %s", source, target, err)
	origin := "source"
	intermediate := fmt.Sprintf("%s.%d_%x", source, os.Getpid(), rand.Intn(4096))
	err = os.Rename(source, intermediate)
	if err == nil {
		source = intermediate
		origin = "target"
	}
	for delay := 0; delay < 5; delay += 1 {
		time.Sleep(time.Duration(delay*100) * time.Millisecond)
		err = os.Rename(source, target)
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("Rename failure [%s, %s, %s, %s], reason: %s", context, common.ControllerIdentity(), common.HolotreeSpace, origin, err)
}

func hasCorrectMode(stat fs.FileInfo, expected fs.FileMode) bool {
	return expected == (stat.Mode() & expected)
}

func ensureCorrectMode(fullpath string, stat fs.FileInfo, correct fs.FileMode) (string, error) {
	if hasCorrectMode(stat, correct) {
		return fullpath, nil
	}
	err := os.Chmod(fullpath, correct)
	if err != nil {
		return "", err
	}
	return fullpath, nil
}

func makeModedDir(fullpath string, correct fs.FileMode) (path string, err error) {
	defer fail.Around(&err)

	if common.WarrantyVoided() {
		return fullpath, nil
	}

	stat, err := os.Stat(fullpath)
	if err == nil && stat.IsDir() {
		return ensureCorrectMode(fullpath, stat, correct)
	}
	fail.On(err == nil, "Path %q exists, but is not a directory!", fullpath)
	_, err = shared.MakeSharedDir(filepath.Dir(fullpath))
	fail.On(err != nil, "%v", err)
	err = os.Mkdir(fullpath, correct)
	fail.On(err != nil, "Failed to create directory %q, reason: %v", fullpath, err)
	stat, err = os.Stat(fullpath)
	fail.On(err != nil, "Failed to stat created directory %q, reason: %v", fullpath, err)
	_, err = ensureCorrectMode(fullpath, stat, correct)
	fail.On(err != nil, "Failed to make created directory shared %q, reason: %v", fullpath, err)
	return fullpath, nil
}

func MakeSharedFile(fullpath string) (string, error) {
	return shared.MakeSharedFile(fullpath)
}

func MakeSharedDir(fullpath string) (string, error) {
	return shared.MakeSharedDir(fullpath)
}

func ForceSharedDir(fullpath string) (string, error) {
	return makeModedDir(fullpath, 0777)
}

func IsSharedDir(fullpath string) bool {
	stat, err := os.Stat(fullpath)
	if err != nil {
		return false
	}
	return stat.IsDir() && hasCorrectMode(stat, 0777)
}

func doEnsureDirectory(directory string, mode fs.FileMode) (string, error) {
	fullpath, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	if common.WarrantyVoided() || IsDir(fullpath) {
		return fullpath, nil
	}
	err = os.MkdirAll(fullpath, mode)
	if err != nil {
		return "", err
	}
	stats, err := os.Stat(fullpath)
	if !stats.IsDir() {
		return "", fmt.Errorf("Path %s is not a directory!", fullpath)
	}
	return fullpath, nil
}

func EnsureSharedDirectory(directory string) (string, error) {
	return shared.MakeSharedDir(directory)
}

func EnsureSharedParentDirectory(resource string) (string, error) {
	return EnsureSharedDirectory(filepath.Dir(resource))
}

func EnsureDirectory(directory string) (string, error) {
	return doEnsureDirectory(directory, 0o750)
}

func EnsureParentDirectory(resource string) (string, error) {
	return doEnsureDirectory(filepath.Dir(resource), 0o750)
}

func RemoveEmptyDirectores(starting string) (err error) {
	defer fail.Around(&err)

	return DirWalk(starting, func(fullpath, relative string, entry os.FileInfo) {
		if IsEmptyDir(fullpath) {
			err = os.Remove(fullpath)
			fail.On(err != nil, "%s", err)
		}
	})
}

func AppendFile(filename string, blob []byte) (err error) {
	defer fail.Around(&err)
	if common.WarrantyVoided() {
		return nil
	}
	handle, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	fail.On(err != nil, "Failed to open file %v -> %v", filename, err)
	defer handle.Close()
	_, err = handle.Write(blob)
	fail.On(err != nil, "Failed to write file %v -> %v", filename, err)
	return handle.Sync()
}
