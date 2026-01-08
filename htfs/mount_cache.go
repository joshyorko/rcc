package htfs

import (
	"sync"
)

// DeviceCache caches device IDs for paths to avoid repeated syscalls
// This makes cross-filesystem detection BLAZINGLY FAST
type DeviceCache struct {
	devices map[string]int64 // path -> device ID
	mu      sync.RWMutex
}

// NewDeviceCache creates a cache for device IDs
func NewDeviceCache() *DeviceCache {
	return &DeviceCache{
		devices: make(map[string]int64),
	}
}

// GetDeviceID returns the cached device ID for a path, or fetches and caches it
func (d *DeviceCache) GetDeviceID(path string) int64 {
	d.mu.RLock()
	if id, ok := d.devices[path]; ok {
		d.mu.RUnlock()
		return id
	}
	d.mu.RUnlock()

	// Not in cache, fetch it
	id := getDeviceID(path)

	// Cache the result
	d.mu.Lock()
	d.devices[path] = id
	d.mu.Unlock()

	return id
}

// SameDevice checks if two paths are on the same device using cached lookups
func (d *DeviceCache) SameDevice(path1, path2 string) bool {
	id1 := d.GetDeviceID(path1)
	id2 := d.GetDeviceID(path2)

	// If either lookup failed, assume different devices (safer)
	if id1 == -1 || id2 == -1 {
		return false
	}

	return id1 == id2
}

// Clear empties the cache (useful between operations)
func (d *DeviceCache) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.devices = make(map[string]int64)
}