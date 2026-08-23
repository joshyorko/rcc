//go:build darwin || linux

package environmentlifecycle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

var contentLocks sync.Map
var contentTransactionProbe func()

func withContentTransaction(ctx context.Context, root string, fn func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	value, _ := contentLocks.LoadOrStore(root, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	path := filepath.Join(root, ".lifecycle-content.lock")
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("open content transaction lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return fmt.Errorf("lock content transaction: %w", err)
	}
	defer func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN); _ = file.Close() }()
	if contentTransactionProbe != nil {
		contentTransactionProbe()
	}
	return fn(ctx)
}
