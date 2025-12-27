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
	roots       map[string]*Root
	timestamps  map[string]time.Time
	accessOrder []string // Track access order for LRU eviction
	maxEntries  int
	mu          sync.RWMutex
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
func (it *MetadataCache) GetOrLoad(path string) (*Root, error) {
	// Fast path: check if we have a valid cached entry
	it.mu.RLock()
	cached, exists := it.roots[path]
	cachedTime := it.timestamps[path]
	it.mu.RUnlock()

	if exists {
		// Validate cache by checking file modification time
		stat, err := os.Stat(path)
		if err == nil && !stat.ModTime().After(cachedTime) {
			// Cache is still valid - update access order
			it.mu.Lock()
			it.promoteToFront(path)
			it.mu.Unlock()
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
	it.mu.Lock()
	it.roots[path] = root
	it.timestamps[path] = modTime
	it.promoteToFront(path)
	it.evictIfNeeded()
	it.mu.Unlock()

	return root, nil
}

// promoteToFront moves the path to the front of the access order (most recently used).
// Must be called with write lock held.
func (it *MetadataCache) promoteToFront(path string) {
	// Remove from current position if exists
	for i, p := range it.accessOrder {
		if p == path {
			it.accessOrder = append(it.accessOrder[:i], it.accessOrder[i+1:]...)
			break
		}
	}
	// Add to front (most recently used)
	it.accessOrder = append([]string{path}, it.accessOrder...)
}

// evictIfNeeded removes the least recently used entries if cache exceeds maxEntries.
// Must be called with write lock held.
func (it *MetadataCache) evictIfNeeded() {
	for len(it.accessOrder) > it.maxEntries {
		// Remove the last entry (least recently used)
		oldest := it.accessOrder[len(it.accessOrder)-1]
		it.accessOrder = it.accessOrder[:len(it.accessOrder)-1]
		delete(it.roots, oldest)
		delete(it.timestamps, oldest)
		common.Timeline("holotree metadata cache evicted LRU entry %q", oldest)
	}
}

// Invalidate removes a specific entry from the cache.
// This is useful when you know a file has been modified externally.
func (it *MetadataCache) Invalidate(path string) {
	it.mu.Lock()
	delete(it.roots, path)
	delete(it.timestamps, path)
	// Remove from access order
	for i, p := range it.accessOrder {
		if p == path {
			it.accessOrder = append(it.accessOrder[:i], it.accessOrder[i+1:]...)
			break
		}
	}
	it.mu.Unlock()
	common.Timeline("holotree metadata cache invalidated for %q", path)
}

// Clear removes all entries from the cache.
// This is useful for testing or when doing bulk operations.
func (it *MetadataCache) Clear() {
	it.mu.Lock()
	it.roots = make(map[string]*Root)
	it.timestamps = make(map[string]time.Time)
	it.accessOrder = make([]string, 0, it.maxEntries)
	it.mu.Unlock()
	common.Timeline("holotree metadata cache cleared")
}

// Size returns the current number of cached entries.
// This is primarily useful for monitoring and testing.
func (it *MetadataCache) Size() int {
	it.mu.RLock()
	size := len(it.roots)
	it.mu.RUnlock()
	return size
}

// MaxEntries returns the maximum number of entries the cache can hold.
func (it *MetadataCache) MaxEntries() int {
	return it.maxEntries
}
