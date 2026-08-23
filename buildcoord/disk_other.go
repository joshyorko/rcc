//go:build !linux

package buildcoord

import (
	"fmt"
	"os"
)

type DiskReservation struct{ path string }

func ReserveDisk(root string, bytes int64) (*DiskReservation, error) {
	if bytes < 0 {
		return nil, fmt.Errorf("disk reservation cannot be negative")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(root, ".rcc-reservation-")
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(bytes); err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
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
