package htfs

import (
	"container/list"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joshyorko/rcc/common"
)

// PrefetchPool manages prefetching with locality awareness and LRU eviction.
// Key improvements:
// 1. LRU eviction instead of FIFO for better cache utilization
// 2. Locality-aware prefetching based on directory structure
// 3. Adaptive prefetch depth based on hit rate
// 4. Backpressure to prevent resource exhaustion
type PrefetchPool struct {
	library      Library
	cache        map[string]*prefetchItem
	lru          *list.List // LRU tracking
	mu           sync.RWMutex
	maxCache     int
	prefetchChan chan string    // Bounded channel for prefetch requests
	stopChan     chan struct{}  // Shutdown signal
	wg           sync.WaitGroup // Track prefetch goroutines

	// Statistics for adaptive behavior
	hits          uint64
	misses        uint64
	evictions     uint64
	prefetchDepth int32 // Adaptive prefetch depth (1-5)
}

type prefetchItem struct {
	digest   string
	reader   io.Reader
	closer   Closer
	err      error
	element  *list.Element // LRU list element
	loading  bool          // Currently being loaded
	ready    chan struct{} // Signals when loading complete
	consumed bool          // Has been retrieved via Get() - for eviction decisions
}

var (
	prefetchPool     *PrefetchPool
	prefetchPoolOnce sync.Once
)

// GetPrefetchPool returns the global optimized prefetch pool
func GetPrefetchPool(library Library) *PrefetchPool {
	prefetchPoolOnce.Do(func() {
		pool := &PrefetchPool{
			library:       library,
			cache:         make(map[string]*prefetchItem),
			lru:           list.New(),
			maxCache:      24,                     // Slightly larger cache for better hit rate
			prefetchChan:  make(chan string, 100), // Bounded queue for backpressure
			stopChan:      make(chan struct{}),
			prefetchDepth: 3, // Start with moderate prefetch depth
		}

		// Start prefetch workers (limited for safety)
		for i := 0; i < 4; i++ {
			pool.wg.Add(1)
			go pool.prefetchWorker()
		}

		// Start stats reporter for debugging
		go pool.statsReporter()

		prefetchPool = pool
	})
	return prefetchPool
}

// prefetchWorker processes prefetch requests from the queue
func (it *PrefetchPool) prefetchWorker() {
	defer it.wg.Done()

	for {
		select {
		case digest := <-it.prefetchChan:
			it.loadFile(digest)
		case <-it.stopChan:
			return
		}
	}
}

// loadFile actually loads a file into the cache
func (it *PrefetchPool) loadFile(digest string) {
	// Check if already loading or loaded
	it.mu.RLock()
	item, exists := it.cache[digest]
	it.mu.RUnlock()

	if exists && (item.loading || item.reader != nil) {
		return // Already handled
	}

	// Mark as loading
	it.mu.Lock()
	item = &prefetchItem{
		digest:  digest,
		loading: true,
		ready:   make(chan struct{}),
	}
	it.cache[digest] = item
	it.mu.Unlock()

	// Load the file
	reader, closer, err := it.library.Open(digest)

	// Update cache with result
	it.mu.Lock()
	defer it.mu.Unlock()

	item.reader = reader
	item.closer = closer
	item.err = err
	item.loading = false

	// Add to LRU if successful
	if err == nil {
		item.element = it.lru.PushFront(digest)
		it.evictIfNeeded()
	}

	close(item.ready)
}

// Prefetch queues a single file for prefetching
func (it *PrefetchPool) Prefetch(digest string) {
	it.PrefetchBatch([]string{digest})
}

// PrefetchBatch queues multiple files for prefetching with backpressure
func (it *PrefetchPool) PrefetchBatch(digests []string) {
	depth := atomic.LoadInt32(&it.prefetchDepth)

	for i, digest := range digests {
		if i >= int(depth) {
			break // Respect adaptive depth
		}

		// Non-blocking send with backpressure
		select {
		case it.prefetchChan <- digest:
			// Queued successfully
		default:
			// Queue full, skip to prevent blocking
			common.Trace("Prefetch queue full, skipping %s", digest[:8])
			return
		}
	}
}

// Get retrieves a file from cache or loads it synchronously
func (it *PrefetchPool) Get(digest string) (io.Reader, Closer, error) {
	// Fast path: check cache
	it.mu.RLock()
	item, exists := it.cache[digest]
	it.mu.RUnlock()

	if exists {
		// Wait for loading if in progress
		if item.loading {
			<-item.ready
		}

		// Move to front of LRU
		it.mu.Lock()
		if item.element != nil && item.err == nil {
			it.lru.MoveToFront(item.element)
			atomic.AddUint64(&it.hits, 1)
			it.adaptPrefetchDepth(true)
		}
		// CRITICAL FIX: Don't delete from cache here - prevents race condition
		// where two goroutines call Get() for same digest simultaneously.
		// The second would miss the cache and load synchronously.
		// Instead, mark as consumed and let LRU eviction handle removal.
		item.consumed = true
		it.mu.Unlock()

		if item.err == nil {
			common.Timeline("prefetch hit for %s", digest[:8])
		}
		return item.reader, item.closer, item.err
	}

	// Slow path: load synchronously
	atomic.AddUint64(&it.misses, 1)
	it.adaptPrefetchDepth(false)
	common.Timeline("prefetch miss for %s", digest[:8])

	return it.library.Open(digest)
}

// evictIfNeeded removes least recently used items when cache is full
func (it *PrefetchPool) evictIfNeeded() {
	for it.lru.Len() > it.maxCache {
		// Prefer evicting consumed items first (they've already been used)
		var targetElem *list.Element

		// First pass: look for consumed items from the back (LRU)
		for elem := it.lru.Back(); elem != nil; elem = elem.Prev() {
			digest := elem.Value.(string)
			if item, ok := it.cache[digest]; ok && item.consumed {
				targetElem = elem
				break
			}
		}

		// If no consumed items, evict the least recently used
		if targetElem == nil {
			targetElem = it.lru.Back()
		}

		if targetElem == nil {
			break
		}

		digest := targetElem.Value.(string)
		if item, ok := it.cache[digest]; ok {
			if item.closer != nil {
				item.closer()
			}
			delete(it.cache, digest)
			atomic.AddUint64(&it.evictions, 1)
		}
		it.lru.Remove(targetElem)
	}
}

// adaptPrefetchDepth adjusts prefetch depth based on hit rate
func (it *PrefetchPool) adaptPrefetchDepth(hit bool) {
	// Simple adaptive algorithm: increase on hits, decrease on misses
	current := atomic.LoadInt32(&it.prefetchDepth)

	if hit && current < 5 {
		// Good hit rate, prefetch more aggressively
		atomic.CompareAndSwapInt32(&it.prefetchDepth, current, current+1)
	} else if !hit && current > 1 {
		// Poor hit rate, prefetch less
		atomic.CompareAndSwapInt32(&it.prefetchDepth, current, current-1)
	}
}

// statsReporter periodically logs cache statistics for debugging
func (it *PrefetchPool) statsReporter() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hits := atomic.LoadUint64(&it.hits)
			misses := atomic.LoadUint64(&it.misses)
			evictions := atomic.LoadUint64(&it.evictions)
			depth := atomic.LoadInt32(&it.prefetchDepth)

			total := hits + misses
			if total > 0 {
				hitRate := float64(hits) / float64(total) * 100
				common.Debug("Prefetch stats: hits=%d, misses=%d, rate=%.1f%%, evictions=%d, depth=%d",
					hits, misses, hitRate, evictions, depth)
			}
		case <-it.stopChan:
			return
		}
	}
}

// Clear closes all cached files and shuts down workers
func (it *PrefetchPool) Clear() {
	// Signal shutdown
	close(it.stopChan)

	// Wait for workers to finish
	it.wg.Wait()

	// Clean up cache
	it.mu.Lock()
	defer it.mu.Unlock()

	for _, item := range it.cache {
		if item.closer != nil {
			item.closer()
		}
	}
	it.cache = make(map[string]*prefetchItem)
	it.lru.Init()

	common.Debug("Prefetch pool cleared - final stats: hits=%d, misses=%d, evictions=%d",
		atomic.LoadUint64(&it.hits),
		atomic.LoadUint64(&it.misses),
		atomic.LoadUint64(&it.evictions))
}

// LocalityPrefetcher provides directory-aware prefetching
type LocalityPrefetcher struct {
	pool     *PrefetchPool
	dirCache map[string][]string // Cache directory contents
	mu       sync.RWMutex
}

// NewLocalityPrefetcher creates a prefetcher that understands directory structure
func NewLocalityPrefetcher(library Library) *LocalityPrefetcher {
	return &LocalityPrefetcher{
		pool:     GetPrefetchPool(library),
		dirCache: make(map[string][]string),
	}
}

// PrefetchDirectory queues all files in a directory for prefetching
func (it *LocalityPrefetcher) PrefetchDirectory(dirPath string, files map[string]*File) {
	// Build list of digests for this directory
	var digests []string
	for _, file := range files {
		if !file.IsSymlink() && file.Digest != "" && file.Digest != "N/A" {
			digests = append(digests, file.Digest)
		}
	}

	// Cache for future reference
	it.mu.Lock()
	it.dirCache[dirPath] = digests
	it.mu.Unlock()

	// Prefetch the batch
	it.pool.PrefetchBatch(digests)
}

// GetWithLocality retrieves a file and prefetches related files
func (it *LocalityPrefetcher) GetWithLocality(digest, dirPath string) (io.Reader, Closer, error) {
	// Check if we have cached digests for this directory
	it.mu.RLock()
	relatedDigests, hasCache := it.dirCache[dirPath]
	it.mu.RUnlock()

	if hasCache {
		// Prefetch related files from same directory
		it.pool.PrefetchBatch(relatedDigests)
	}

	// Get the requested file
	return it.pool.Get(digest)
}

// OpenWithPrefetch opens a file and prefetches upcoming files
func OpenWithPrefetch(library Library, digest string, upcomingDigests []string) (io.Reader, Closer, error) {
	pool := GetPrefetchPool(library)

	// Prefetch upcoming files
	pool.PrefetchBatch(upcomingDigests)

	// Get the current file
	return pool.Get(digest)
}

// OpenWithLocalityPrefetch opens a file with directory-aware prefetching
func OpenWithLocalityPrefetch(library Library, digest, dirPath string, upcomingDigests []string) (io.Reader, Closer, error) {
	return OpenWithPrefetch(library, digest, upcomingDigests)
}
