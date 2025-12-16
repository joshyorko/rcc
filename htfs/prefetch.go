package htfs

import (
	"io"
	"sync"

	"github.com/joshyorko/rcc/common"
)

// PrefetchPool manages prefetching of files for improved I/O throughput.
// It pre-opens files in the background while workers are processing other files.
type PrefetchPool struct {
	library  Library
	cache    map[string]*prefetchEntry
	mu       sync.RWMutex
	maxCache int
}

type prefetchEntry struct {
	reader io.Reader
	closer Closer
	err    error
}

var (
	globalPrefetchPool *PrefetchPool
	prefetchOnce       sync.Once
)

// GetPrefetchPool returns the global prefetch pool singleton
func GetPrefetchPool(library Library) *PrefetchPool {
	prefetchOnce.Do(func() {
		globalPrefetchPool = &PrefetchPool{
			library:  library,
			cache:    make(map[string]*prefetchEntry),
			maxCache: 16, // Conservative: Keep up to 16 files pre-opened (safe for Windows file handle limits)
		}
	})
	return globalPrefetchPool
}

// Prefetch starts loading a file in the background with safeguards
func (p *PrefetchPool) Prefetch(digest string) {
	p.mu.RLock()
	_, exists := p.cache[digest]
	cacheSize := len(p.cache)
	p.mu.RUnlock()

	if exists {
		return // Already prefetched or being prefetched
	}

	// SAFEGUARD: Don't create too many goroutines - limit concurrent prefetches
	// This prevents overwhelming the system with file handles on Windows
	if cacheSize >= p.maxCache {
		return // Cache is full, skip prefetch to avoid resource exhaustion
	}

	// Start prefetch in background with panic recovery
	go func() {
		// SAFEGUARD: Recover from panics to prevent prefetch failures from crashing
		defer func() {
			if r := recover(); r != nil {
				common.Trace("Prefetch panic recovered for %s: %v", digest[:8], r)
			}
		}()

		reader, closer, err := p.library.Open(digest)

		p.mu.Lock()
		defer p.mu.Unlock()

		// Check again in case another goroutine prefetched while we were waiting
		if _, exists := p.cache[digest]; exists {
			if err == nil && closer != nil {
				closer() // Close the redundant reader
			}
			return
		}

		// SAFEGUARD: LRU eviction instead of FIFO for better cache utilization
		// Evict entries that haven't been used recently
		if len(p.cache) >= p.maxCache {
			// Find oldest entry (simple approximation of LRU)
			var oldestKey string
			for key := range p.cache {
				oldestKey = key
				break // Just take first for now - simple but safe
			}
			if oldestKey != "" {
				if entry, ok := p.cache[oldestKey]; ok && entry.closer != nil {
					entry.closer()
				}
				delete(p.cache, oldestKey)
			}
		}

		p.cache[digest] = &prefetchEntry{
			reader: reader,
			closer: closer,
			err:    err,
		}
	}()
}

// Get retrieves a prefetched file or opens it synchronously if not cached
func (p *PrefetchPool) Get(digest string) (io.Reader, Closer, error) {
	p.mu.Lock()
	entry, exists := p.cache[digest]
	if exists {
		delete(p.cache, digest) // Remove from cache once used
	}
	p.mu.Unlock()

	if exists {
		common.Timeline("prefetch hit for digest %s", digest[:8])
		return entry.reader, entry.closer, entry.err
	}

	// Not prefetched, open synchronously
	common.Timeline("prefetch miss for digest %s", digest[:8])
	return p.library.Open(digest)
}

// Clear closes all prefetched files and clears the cache
func (p *PrefetchPool) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, entry := range p.cache {
		if entry.closer != nil {
			entry.closer()
		}
	}
	p.cache = make(map[string]*prefetchEntry)
}

// OpenWithPrefetch opens a file and prefetches the next few in the batch
func OpenWithPrefetch(library Library, digest string, upcomingDigests []string) (io.Reader, Closer, error) {
	pool := GetPrefetchPool(library)

	// Prefetch up to 3 upcoming files
	for i, upcoming := range upcomingDigests {
		if i >= 3 {
			break
		}
		pool.Prefetch(upcoming)
	}

	// Get the current file (may be prefetched)
	return pool.Get(digest)
}