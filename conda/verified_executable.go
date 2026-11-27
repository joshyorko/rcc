package conda

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func verifiedExecutable(path, digest string, maxSize int64) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode()&0100 == 0) || info.Size() > maxSize {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return false
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(f, maxSize+1))
	return err == nil && n == info.Size() && hex.EncodeToString(hash.Sum(nil)) == digest
}

func publishExecutable(path, digest string, maxSize int64, source io.Reader) error {
	if len(digest) != 64 || maxSize <= 0 {
		return errors.New("invalid executable pin")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("invalid executable cache directory")
	}
	lockPath := path + ".install.lock"
	lock, err := openExecutableLock(lockPath)
	if err != nil {
		return err
	}
	defer func() { _ = releaseExecutableLock(lock) }()
	for attempts := 0; ; attempts++ {
		locked, lockErr := acquireExecutableLock(lock)
		if lockErr != nil {
			return lockErr
		}
		if locked {
			break
		}
		if attempts >= 100 {
			return errors.New("timed out waiting for executable install lock")
		}
		time.Sleep(40 * time.Millisecond)
	}
	// Keep the advisory lock inode stable while other processes may have it open.
	if verifiedExecutable(path, digest, maxSize) {
		return nil
	}
	f, err := os.CreateTemp(dir, ".install-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(source, maxSize+1))
	if err == nil && (n > maxSize || hex.EncodeToString(hash.Sum(nil)) != digest) {
		err = fmt.Errorf("executable integrity check failed")
	}
	if err == nil {
		err = f.Chmod(0700)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
