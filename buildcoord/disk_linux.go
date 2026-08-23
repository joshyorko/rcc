//go:build linux

package buildcoord

import (
	"fmt"
	"os"
	"syscall"
)

type DiskReservation struct{ path string }

func ReserveDisk(root string, bytes int64) (*DiskReservation, error) {
	if bytes < 0 {
		return nil, fmt.Errorf("disk reservation cannot be negative")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(root, &stat); err != nil {
		return nil, err
	}
	if uint64(bytes) > stat.Bavail*uint64(stat.Bsize) {
		return nil, fmt.Errorf("insufficient staging disk capacity")
	}
	f, err := os.CreateTemp(root, ".rcc-reservation-")
	if err != nil {
		return nil, err
	}
	if bytes > 0 {
		if err := syscall.Fallocate(int(f.Fd()), 0, 0, bytes); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return nil, fmt.Errorf("reserve staging disk: %w", err)
		}
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return nil, err
	}
	return &DiskReservation{path: f.Name()}, nil
}

func (r *DiskReservation) Release() error {
	if r == nil || r.path == "" {
		return nil
	}
	err := os.Remove(r.path)
	r.path = ""
	return err
}
