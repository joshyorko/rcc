package htfs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pathlib"
)

// BenchmarkResults tracks performance metrics
type BenchmarkResults struct {
	Name         string
	Duration     time.Duration
	FilesPerSec  float64
	BytesPerSec  float64
	TotalFiles   int
	TotalBytes   int64
	Allocations  int64
	AllocBytes   int64
	DirtyPercent float64
}

// String formats benchmark results for display
func (it *BenchmarkResults) String() string {
	return fmt.Sprintf(`
Benchmark: %s
Duration: %v
Files/sec: %.0f
MB/sec: %.2f
Total Files: %d
Total MB: %.2f
Dirty: %.1f%%
`,
		it.Name,
		it.Duration,
		it.FilesPerSec,
		it.BytesPerSec/(1024*1024),
		it.TotalFiles,
		float64(it.TotalBytes)/(1024*1024),
		it.DirtyPercent,
	)
}

// BenchmarkRestoreDirectory compares different restoration strategies
func BenchmarkRestoreDirectory(b *testing.B) {
	// Skip if not in CI or explicit benchmark mode
	if os.Getenv("RUN_BENCHMARKS") != "1" {
		b.Skip("Skipping benchmark - set RUN_BENCHMARKS=1 to run")
	}

	// Setup test environment
	testDir := filepath.Join(common.ProductTemp(), "benchmark")
	os.MkdirAll(testDir, 0755)
	defer os.RemoveAll(testDir)

	// Create test library
	library, err := New()
	if err != nil {
		b.Fatal(err)
	}

	// Load a test catalog (you'll need to provide this)
	catalogPath := findTestCatalog()
	if catalogPath == "" {
		b.Skip("No test catalog found")
	}

	fs, err := NewRoot(testDir)
	if err != nil {
		b.Fatal(err)
	}

	err = fs.LoadFrom(catalogPath)
	if err != nil {
		b.Fatal(err)
	}

	// Count files for statistics
	fileCount, byteCount := countFiles(fs)
	b.Logf("Test catalog: %d files, %.2f MB", fileCount, float64(byteCount)/(1024*1024))

	// Benchmark configurations
	benchmarks := []struct {
		name string
		fn   Dirtask
	}{
		{
			name: "Simple",
			fn:   RestoreDirectorySimple(library, fs, make(map[string]string), &stats{}),
		},
		{
			name: "Batched",
			fn:   RestoreDirectory(library, fs, make(map[string]string), &stats{}),
		},
		{
			name: "WithHardlinks",
			fn:   RestoreDirectoryWithHardlinks(library, fs, make(map[string]string), &stats{}),
		},
	}

	// Run benchmarks
	for _, bench := range benchmarks {
		b.Run(bench.name, func(b *testing.B) {
			// Reset timer before actual benchmark
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				// Clean target directory
				targetDir := filepath.Join(testDir, fmt.Sprintf("restore_%d", i))
				os.RemoveAll(targetDir)
				os.MkdirAll(targetDir, 0755)

				// Create fresh stats
				benchStats := &stats{}

				// Time the restoration
				start := time.Now()

				// Run restoration
				work := bench.fn(targetDir, fs.Tree)
				work()

				duration := time.Since(start)

				// Calculate metrics
				filesPerSec := float64(fileCount) / duration.Seconds()
				bytesPerSec := float64(byteCount) / duration.Seconds()

				// Report to benchmark
				b.ReportMetric(filesPerSec, "files/sec")
				b.ReportMetric(bytesPerSec/(1024*1024), "MB/sec")
				b.ReportMetric(benchStats.Dirtyness(), "dirty%")

				// Log detailed results
				if testing.Verbose() {
					b.Logf("Run %d: %v (%.0f files/sec, %.2f MB/sec, %.1f%% dirty)",
						i, duration, filesPerSec, bytesPerSec/(1024*1024), benchStats.Dirtyness())
				}
			}
		})
	}
}

// BenchmarkPrefetch compares prefetch strategies
func BenchmarkPrefetch(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "1" {
		b.Skip("Skipping benchmark - set RUN_BENCHMARKS=1 to run")
	}

	// Create test library
	library, err := New()
	if err != nil {
		b.Fatal(err)
	}

	// Generate test digests
	testDigests := generateTestDigests(100)

	benchmarks := []struct {
		name string
		fn   func([]string)
	}{
		{
			name: "NoPrefetch",
			fn: func(digests []string) {
				for _, digest := range digests {
					reader, closer, err := library.Open(digest)
					if err == nil {
						io.Copy(io.Discard, reader)
						closer()
					}
				}
			},
		},
		{
			name: "BasicPrefetch",
			fn: func(digests []string) {
				pool := GetPrefetchPool(library)
				for i, digest := range digests {
					// Prefetch next 3
					for j := i + 1; j < len(digests) && j < i+4; j++ {
						pool.Prefetch(digests[j])
					}
					reader, closer, err := pool.Get(digest)
					if err == nil {
						io.Copy(io.Discard, reader)
						closer()
					}
				}
			},
		},
		{
			name: "OptimizedPrefetch",
			fn: func(digests []string) {
				pool := GetPrefetchPool(library)
				pool.PrefetchBatch(digests[:10]) // Prefetch first batch

				for i, digest := range digests {
					// Prefetch next batch
					if i%10 == 0 && i+10 < len(digests) {
						pool.PrefetchBatch(digests[i+10 : min(i+20, len(digests))])
					}
					reader, closer, err := pool.Get(digest)
					if err == nil {
						io.Copy(io.Discard, reader)
						closer()
					}
				}
			},
		},
	}

	for _, bench := range benchmarks {
		b.Run(bench.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bench.fn(testDigests)
			}
		})
	}
}

// BenchmarkBufferPool tests buffer pooling effectiveness
func BenchmarkBufferPool(b *testing.B) {
	benchmarks := []struct {
		name string
		fn   func() []byte
	}{
		{
			name: "NewBuffer",
			fn: func() []byte {
				return make([]byte, copyBufferSize)
			},
		},
		{
			name: "PooledBuffer",
			fn: func() []byte {
				buf := GetCopyBuffer()
				defer PutCopyBuffer(buf)
				return *buf
			},
		},
	}

	for _, bench := range benchmarks {
		b.Run(bench.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buf := bench.fn()
				// Simulate some work
				for j := 0; j < 100; j++ {
					buf[j] = byte(j)
				}
			}
		})
	}
}

// Helper functions

func findTestCatalog() string {
	// Look for test catalogs in common locations
	candidates := []string{
		filepath.Join(common.HololibCatalogLocation(), "test.catalog"),
		filepath.Join("testdata", "test.catalog"),
		filepath.Join("..", "testdata", "test.catalog"),
	}

	for _, path := range candidates {
		if pathlib.IsFile(path) {
			return path
		}
	}

	// Try to find any catalog
	catalogs := CatalogNames()
	if len(catalogs) > 0 {
		return filepath.Join(common.HololibCatalogLocation(), catalogs[0])
	}

	return ""
}

func countFiles(fs *Root) (int, int64) {
	var count int
	var bytes int64

	var counter func(*Dir)
	counter = func(dir *Dir) {
		count += len(dir.Files)
		for _, file := range dir.Files {
			bytes += file.Size
		}
		for _, subdir := range dir.Dirs {
			counter(subdir)
		}
	}

	counter(fs.Tree)
	return count, bytes
}

func generateTestDigests(n int) []string {
	digests := make([]string, n)
	for i := 0; i < n; i++ {
		digests[i] = fmt.Sprintf("%064x", i)
	}
	return digests
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// BenchmarkDirectoryBatching tests the overhead of directory batching
func BenchmarkDirectoryBatching(b *testing.B) {
	// Create test directory structure
	testDirs := make([]string, 100)
	for i := range testDirs {
		testDirs[i] = fmt.Sprintf("/tmp/test/dir%d/subdir%d/deep%d", i, i, i)
	}

	b.Run("Sequential", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, dir := range testDirs {
				// Simulate mkdir operations
				_ = filepath.Dir(dir)
			}
		}
	})

	b.Run("Batched", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Simulate batched operations
			batch := make(map[string]bool)
			for _, dir := range testDirs {
				batch[filepath.Dir(dir)] = true
			}
			// Process batch
			for dir := range batch {
				_ = dir
			}
		}
	})
}

// BenchmarkLocalityAwareness tests locality-based processing
func BenchmarkLocalityAwareness(b *testing.B) {
	// Create test file list with mixed sizes
	type testFile struct {
		path string
		size int64
	}

	files := make([]testFile, 1000)
	for i := range files {
		files[i] = testFile{
			path: fmt.Sprintf("/data/dir%d/file%d.dat", i%10, i),
			size: int64(i%100) * 1024,
		}
	}

	b.Run("RandomOrder", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, f := range files {
				// Simulate processing
				_ = f.size
			}
		}
	})

	b.Run("LocalityAware", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Group by directory
			dirMap := make(map[string][]testFile)
			for _, f := range files {
				dir := filepath.Dir(f.path)
				dirMap[dir] = append(dirMap[dir], f)
			}
			// Process by directory
			for _, dirFiles := range dirMap {
				for _, f := range dirFiles {
					_ = f.size
				}
			}
		}
	})
}

// BenchmarkParallelHardlinks tests hardlink creation performance
func BenchmarkParallelHardlinks(b *testing.B) {
	if runtime.GOOS == "windows" {
		b.Skip("Hardlink benchmark requires Unix-like OS")
	}

	// Create test hardlink pairs
	pairs := make([]struct{ src, dst string }, 100)
	for i := range pairs {
		pairs[i].src = fmt.Sprintf("/tmp/src%d", i)
		pairs[i].dst = fmt.Sprintf("/tmp/dst%d", i)
	}

	b.Run("Sequential", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, p := range pairs {
				// Simulate hardlink creation
				_ = p.src + p.dst
			}
		}
	})

	b.Run("Parallel", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var wg sync.WaitGroup
			sem := make(chan struct{}, 8) // Limit parallelism

			for _, p := range pairs {
				wg.Add(1)
				sem <- struct{}{}
				go func(src, dst string) {
					defer wg.Done()
					defer func() { <-sem }()
					// Simulate hardlink creation
					_ = src + dst
				}(p.src, p.dst)
			}
			wg.Wait()
		}
	})
}

// BenchmarkSyncPool tests sync.Pool effectiveness
func BenchmarkSyncPool(b *testing.B) {
	b.Run("Allocations", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf := make([]byte, copyBufferSize)
			// Simulate some work
			buf[0] = byte(i)
		}
	})

	b.Run("PooledAllocations", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf := GetCopyBuffer()
			// Simulate some work
			(*buf)[0] = byte(i)
			PutCopyBuffer(buf)
		}
	})
}
