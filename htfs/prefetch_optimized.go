package htfs

import (
	"container/list"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joshyorko/rcc/common"
)

// OptimizedPrefetchPool manages prefetching with locality awareness and LRU eviction.
// Key improvements:
// 1. LRU eviction instead of FIFO for better cache utilization
// 2. Locality-aware prefetching based on directory structure
// 3. Adaptive prefetch depth based on hit rate
// 4. Backpressure to prevent resource exhaustion
type OptimizedPrefetchPool struct {
	library      Library
	cache        map[string]*prefetchItem
	lru          *list.List // LRU tracking
	mu           sync.RWMutex
	maxCache     int
	prefetchChan chan string      // Bounded channel for prefetch requests
	stopChan     chan struct{}    // Shutdown signal
	wg           sync.WaitGroup   // Track prefetch goroutines

	// Statistics for adaptive behavior
	hits         uint64
	misses       uint64
	evictions    uint64
	prefetchDepth int32 // Adaptive prefetch depth (1-5)
}

type prefetchItem struct {
	digest   string
	reader   io.Reader
	closer   Closer
	err      error
	element  *list.Element // LRU list element
	loading  bool         // Currently being loaded
	ready    chan struct{} // Signals when loading complete
	consumed bool         // Has been retrieved via Get() - for eviction decisions
}

var (
	optimizedPool     *OptimizedPrefetchPool
	optimizedPoolOnce sync.Once
)

// GetOptimizedPrefetchPool returns the global optimized prefetch pool
func GetOptimizedPrefetchPool(library Library) *OptimizedPrefetchPool {
	optimizedPoolOnce.Do(func() {
		pool := &OptimizedPrefetchPool{
			library:       library,
			cache:         make(map[string]*prefetchItem),
			lru:           list.New(),
			maxCache:      24, // Slightly larger cache for better hit rate
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

		optimizedPool = pool
	})
	return optimizedPool
}

// prefetchWorker processes prefetch requests from the queue
func (p *OptimizedPrefetchPool) prefetchWorker() {
	defer p.wg.Done()

	for {
		select {
		case digest := <-p.prefetchChan:
			p.loadFile(digest)
		case <-p.stopChan:
			return
		}
	}
}

// loadFile actually loads a file into the cache
func (p *OptimizedPrefetchPool) loadFile(digest string) {
	// Check if already loading or loaded
	p.mu.RLock()
	item, exists := p.cache[digest]
	p.mu.RUnlock()

	if exists && (item.loading || item.reader != nil) {
		return // Already handled
	}

	// Mark as loading
	p.mu.Lock()
	item = &prefetchItem{
		digest:  digest,
		loading: true,
		ready:   make(chan struct{}),
	}
	p.cache[digest] = item
	p.mu.Unlock()

	// Load the file
	reader, closer, err := p.library.Open(digest)

	// Update cache with result
	p.mu.Lock()
	defer p.mu.Unlock()

	item.reader = reader
	item.closer = closer
	item.err = err
	item.loading = false

	// Add to LRU if successful
	if err == nil {
		item.element = p.lru.PushFront(digest)
		p.evictIfNeeded()
	}

	close(item.ready)
}

// PrefetchBatch queues multiple files for prefetching with backpressure
func (p *OptimizedPrefetchPool) PrefetchBatch(digests []string) {
	depth := atomic.LoadInt32(&p.prefetchDepth)

	for i, digest := range digests {
		if i >= int(depth) {
			break // Respect adaptive depth
		}

		// Non-blocking send with backpressure
		select {
		case p.prefetchChan <- digest:
			// Queued successfully
		default:
			// Queue full, skip to prevent blocking
			common.Trace("Prefetch queue full, skipping %s", digest[:8])
			return
		}
	}
}

// Get retrieves a file from cache or loads it synchronously
func (p *OptimizedPrefetchPool) Get(digest string) (io.Reader, Closer, error) {
	// Fast path: check cache
	p.mu.RLock()
	item, exists := p.cache[digest]
	p.mu.RUnlock()

	if exists {
		// Wait for loading if in progress
		if item.loading {
			<-item.ready
		}

		// Move to front of LRU
		p.mu.Lock()
		if item.element != nil && item.err == nil {
			p.lru.MoveToFront(item.element)
			atomic.AddUint64(&p.hits, 1)
			p.adaptPrefetchDepth(true)
		}
		// CRITICAL FIX: Don't delete from cache here - prevents race condition
		// where two goroutines call Get() for same digest simultaneously.
		// The second would miss the cache and load synchronously.
		// Instead, mark as consumed and let LRU eviction handle removal.
		item.consumed = true
		p.mu.Unlock()

		if item.err == nil {
			common.Timeline("prefetch hit for %s", digest[:8])
		}
		return item.reader, item.closer, item.err
	}

	// Slow path: load synchronously
	atomic.AddUint64(&p.misses, 1)
	p.adaptPrefetchDepth(false)
	common.Timeline("prefetch miss for %s", digest[:8])

	return p.library.Open(digest)
}

// evictIfNeeded removes least recently used items when cache is full
func (p *OptimizedPrefetchPool) evictIfNeeded() {
	for p.lru.Len() > p.maxCache {
		// Prefer evicting consumed items first (they've already been used)
		var targetElem *list.Element

		// First pass: look for consumed items from the back (LRU)
		for elem := p.lru.Back(); elem != nil; elem = elem.Prev() {
			digest := elem.Value.(string)
			if item, ok := p.cache[digest]; ok && item.consumed {
				targetElem = elem
				break
			}
		}

		// If no consumed items, evict the least recently used
		if targetElem == nil {
			targetElem = p.lru.Back()
		}

		if targetElem == nil {
			break
		}

		digest := targetElem.Value.(string)
		if item, ok := p.cache[digest]; ok {
			if item.closer != nil {
				item.closer()
			}
			delete(p.cache, digest)
			atomic.AddUint64(&p.evictions, 1)
		}
		p.lru.Remove(targetElem)
	}
}

// adaptPrefetchDepth adjusts prefetch depth based on hit rate
func (p *OptimizedPrefetchPool) adaptPrefetchDepth(hit bool) {
	// Simple adaptive algorithm: increase on hits, decrease on misses
	current := atomic.LoadInt32(&p.prefetchDepth)

	if hit && current < 5 {
		// Good hit rate, prefetch more aggressively
		atomic.CompareAndSwapInt32(&p.prefetchDepth, current, current+1)
	} else if !hit && current > 1 {
		// Poor hit rate, prefetch less
		atomic.CompareAndSwapInt32(&p.prefetchDepth, current, current-1)
	}
}

// statsReporter periodically logs cache statistics for debugging
func (p *OptimizedPrefetchPool) statsReporter() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hits := atomic.LoadUint64(&p.hits)
			misses := atomic.LoadUint64(&p.misses)
			evictions := atomic.LoadUint64(&p.evictions)
			depth := atomic.LoadInt32(&p.prefetchDepth)

			total := hits + misses
			if total > 0 {
				hitRate := float64(hits) / float64(total) * 100
				common.Debug("Prefetch stats: hits=%d, misses=%d, rate=%.1f%%, evictions=%d, depth=%d",
					hits, misses, hitRate, evictions, depth)
			}
		case <-p.stopChan:
			return
		}
	}
}

// Clear closes all cached files and shuts down workers
func (p *OptimizedPrefetchPool) Clear() {
	// Signal shutdown
	close(p.stopChan)

	// Wait for workers to finish
	p.wg.Wait()

	// Clean up cache
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, item := range p.cache {
		if item.closer != nil {
			item.closer()
		}
	}
	p.cache = make(map[string]*prefetchItem)
	p.lru.Init()

	common.Debug("Prefetch pool cleared - final stats: hits=%d, misses=%d, evictions=%d",
		atomic.LoadUint64(&p.hits),
		atomic.LoadUint64(&p.misses),
		atomic.LoadUint64(&p.evictions))
}

// LocalityPrefetcher provides directory-aware prefetching
type LocalityPrefetcher struct {
	pool      *OptimizedPrefetchPool
	dirCache  map[string][]string // Cache directory contents
	mu        sync.RWMutex
}

// NewLocalityPrefetcher creates a prefetcher that understands directory structure
func NewLocalityPrefetcher(library Library) *LocalityPrefetcher {
	return &LocalityPrefetcher{
		pool:     GetOptimizedPrefetchPool(library),
		dirCache: make(map[string][]string),
	}
}

// PrefetchDirectory queues all files in a directory for prefetching
func (lp *LocalityPrefetcher) PrefetchDirectory(dirPath string, files map[string]*File) {
	// Build list of digests for this directory
	var digests []string
	for _, file := range files {
		if !file.IsSymlink() && file.Digest != "" && file.Digest != "N/A" {
			digests = append(digests, file.Digest)
		}
	}

	// Cache for future reference
	lp.mu.Lock()
	lp.dirCache[dirPath] = digests
	lp.mu.Unlock()

	// Prefetch the batch
	lp.pool.PrefetchBatch(digests)
}

// GetWithLocality retrieves a file and prefetches related files
func (lp *LocalityPrefetcher) GetWithLocality(digest, dirPath string) (io.Reader, Closer, error) {
	// Check if we have cached digests for this directory
	lp.mu.RLock()
	relatedDigests, hasCache := lp.dirCache[dirPath]
	lp.mu.RUnlock()

	if hasCache {
		// Prefetch related files from same directory
		lp.pool.PrefetchBatch(relatedDigests)
	}

	// Get the requested file
	return lp.pool.Get(digest)
}

// OpenWithLocalityPrefetch opens a file with directory-aware prefetching
func OpenWithLocalityPrefetch(library Library, digest, dirPath string, upcomingDigests []string) (io.Reader, Closer, error) {
	pool := GetOptimizedPrefetchPool(library)

	// Prefetch upcoming files
	pool.PrefetchBatch(upcomingDigests)

	// Get the current file
	return pool.Get(digest)
}