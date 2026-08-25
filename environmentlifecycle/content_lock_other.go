//go:build !darwin && !linux

package environmentlifecycle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/joshyorko/rcc/pathlib"
)

var contentLocks sync.Map
var contentTransactionBeforeAcquire func()
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
	if contentTransactionBeforeAcquire != nil {
		contentTransactionBeforeAcquire()
	}
	lease, err := pathlib.Locker(filepath.Join(root, ".lifecycle-content.lock"), -1, false)
	if err != nil {
		return fmt.Errorf("lock content transaction: %w", err)
	}
	defer func() { _ = lease.Release() }()
	if contentTransactionProbe != nil {
		contentTransactionProbe()
	}
	return fn(ctx)
}
