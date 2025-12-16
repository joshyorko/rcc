package htfs

import (
	"os"
	"sync"
	"time"

	"github.com/joshyorko/rcc/common"
)

const (
	// DefaultCacheMaxEntries is the default maximum number of entries in the metadata cache.
	// This prevents unbounded memory growth in long-running processes.
	DefaultCacheMaxEntries = 100
)

// MetadataCache provides thread-safe caching of Root metadata structures
// to avoid redundant filesystem queries and JSON parsing operations.
// Implements LRU eviction when the cache exceeds maxEntries.
type MetadataCache struct {
	roots      map[string]*Root
	timestamps map[string]time.Time
	accessOrder []string // Track access order for LRU eviction
	maxEntries int
	mu         sync.RWMutex
}

// NewMetadataCache creates a new empty metadata cache with default max entries.
func NewMetadataCache() *MetadataCache {
	return NewMetadataCacheWithLimit(DefaultCacheMaxEntries)
}

// NewMetadataCacheWithLimit creates a new metadata cache with a custom max entries limit.
func NewMetadataCacheWithLimit(maxEntries int) *MetadataCache {
	if maxEntries <= 0 {
		maxEntries = DefaultCacheMaxEntries
	}
	return &MetadataCache{
		roots:       make(map[string]*Root),
		timestamps:  make(map[string]time.Time),
		accessOrder: make([]string, 0, maxEntries),
		maxEntries:  maxEntries,
	}
}

// GetOrLoad retrieves a cached Root or loads it from disk if not cached
// or if the file has been modified since last load.
// Returns the Root and any error encountered during loading.
func (mc *MetadataCache) GetOrLoad(path string) (*Root, error) {
	// Fast path: check if we have a valid cached entry
	mc.mu.RLock()
	cached, exists := mc.roots[path]
	cachedTime := mc.timestamps[path]
	mc.mu.RUnlock()

	if exists {
		// Validate cache by checking file modification time
		stat, err := os.Stat(path)
		if err == nil && !stat.ModTime().After(cachedTime) {
			// Cache is still valid - update access order
			mc.mu.Lock()
			mc.promoteToFront(path)
			mc.mu.Unlock()
			common.Timeline("holotree metadata cache hit for %q", path)
			return cached, nil
		}
		// File was modified or stat failed, need to reload
		common.Timeline("holotree metadata cache miss (modified) for %q", path)
	} else {
		common.Timeline("holotree metadata cache miss (new) for %q", path)
	}

	// Slow path: load from disk
	// First get file stat to capture the modification time
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	modTime := stat.ModTime()

	// Create new Root and load metadata
	root, err := NewRoot(path[:len(path)-5]) // Remove .meta extension
	if err != nil {
		return nil, err
	}

	err = root.LoadFrom(path)
	if err != nil {
		return nil, err
	}

	// Update cache with write lock
	mc.mu.Lock()
	mc.roots[path] = root
	mc.timestamps[path] = modTime
	mc.promoteToFront(path)
	mc.evictIfNeeded()
	mc.mu.Unlock()

	return root, nil
}

// promoteToFront moves the path to the front of the access order (most recently used).
// Must be called with write lock held.
func (mc *MetadataCache) promoteToFront(path string) {
	// Remove from current position if exists
	for i, p := range mc.accessOrder {
		if p == path {
			mc.accessOrder = append(mc.accessOrder[:i], mc.accessOrder[i+1:]...)
			break
		}
	}
	// Add to front (most recently used)
	mc.accessOrder = append([]string{path}, mc.accessOrder...)
}

// evictIfNeeded removes the least recently used entries if cache exceeds maxEntries.
// Must be called with write lock held.
func (mc *MetadataCache) evictIfNeeded() {
	for len(mc.accessOrder) > mc.maxEntries {
		// Remove the last entry (least recently used)
		oldest := mc.accessOrder[len(mc.accessOrder)-1]
		mc.accessOrder = mc.accessOrder[:len(mc.accessOrder)-1]
		delete(mc.roots, oldest)
		delete(mc.timestamps, oldest)
		common.Timeline("holotree metadata cache evicted LRU entry %q", oldest)
	}
}

// Invalidate removes a specific entry from the cache.
// This is useful when you know a file has been modified externally.
func (mc *MetadataCache) Invalidate(path string) {
	mc.mu.Lock()
	delete(mc.roots, path)
	delete(mc.timestamps, path)
	// Remove from access order
	for i, p := range mc.accessOrder {
		if p == path {
			mc.accessOrder = append(mc.accessOrder[:i], mc.accessOrder[i+1:]...)
			break
		}
	}
	mc.mu.Unlock()
	common.Timeline("holotree metadata cache invalidated for %q", path)
}

// Clear removes all entries from the cache.
// This is useful for testing or when doing bulk operations.
func (mc *MetadataCache) Clear() {
	mc.mu.Lock()
	mc.roots = make(map[string]*Root)
	mc.timestamps = make(map[string]time.Time)
	mc.accessOrder = make([]string, 0, mc.maxEntries)
	mc.mu.Unlock()
	common.Timeline("holotree metadata cache cleared")
}

// Size returns the current number of cached entries.
// This is primarily useful for monitoring and testing.
func (mc *MetadataCache) Size() int {
	mc.mu.RLock()
	size := len(mc.roots)
	mc.mu.RUnlock()
	return size
}

// MaxEntries returns the maximum number of entries the cache can hold.
func (mc *MetadataCache) MaxEntries() int {
	return mc.maxEntries
}
