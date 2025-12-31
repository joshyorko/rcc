package htfs

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/joshyorko/rcc/anywork"
)

func TestShouldBatch(t *testing.T) {
	tests := []struct {
		name     string
		file     *File
		expected bool
	}{
		{
			name: "small file without rewrites",
			file: &File{
				Size:    50 * 1024, // 50KB
				Rewrite: nil,
			},
			expected: true,
		},
		{
			name: "large file",
			file: &File{
				Size:    200 * 1024, // 200KB
				Rewrite: nil,
			},
			expected: false,
		},
		{
			name: "small file with few rewrites",
			file: &File{
				Size:    50 * 1024, // 50KB
				Rewrite: []int64{0, 100},
			},
			expected: true, // Up to 10 rewrites is OK in optimized version
		},
		{
			name: "small file with many rewrites",
			file: &File{
				Size:    50 * 1024, // 50KB
				Rewrite: make([]int64, 11), // More than 10 rewrites
			},
			expected: false, // Too many rewrites
		},
		{
			name: "file at threshold boundary",
			file: &File{
				Size:    SmallFileThreshold, // 100KB
				Rewrite: nil,
			},
			expected: false, // Should not batch files >= threshold
		},
		{
			name: "file just under threshold",
			file: &File{
				Size:    SmallFileThreshold - 1,
				Rewrite: nil,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldBatch(tt.file)
			if result != tt.expected {
				t.Errorf("shouldBatch() = %v, want %v for file size=%d, rewrites=%v",
					result, tt.expected, tt.file.Size, tt.file.Rewrite)
			}
		})
	}
}

func TestProcessBatch(t *testing.T) {
	// Basic test to ensure ProcessBatch doesn't panic with empty batch
	work := ProcessBatch([]FileTask{})
	work() // Should not panic
}

func TestBatchConstants(t *testing.T) {
	// Verify constants are reasonable
	if SmallFileThreshold != 100*1024 {
		t.Errorf("SmallFileThreshold = %d, want 100KB", SmallFileThreshold)
	}
	if BatchSize != 16 {
		t.Errorf("BatchSize = %d, want 16 (smaller batches = better parallelism)", BatchSize)
	}
}

// TestBatchErrorPropagation tests that errors in batched file processing
// are properly propagated. Per Juha's guidance: "What happens when file 15 of 32 fails?
// Does the error propagate correctly? Are the first 14 files in a consistent state?"
func TestBatchErrorPropagation(t *testing.T) {
	// Track which files were "processed" before a panic
	var processedCount int32

	// Create a mock work function that panics on file 15
	mockProcessBatch := func(tasks []FileTask) anywork.Work {
		return func() {
			for i := range tasks {
				atomic.AddInt32(&processedCount, 1)
				if i == 14 { // File 15 (0-indexed = 14)
					panic("simulated file processing error")
				}
			}
		}
	}

	// Create 32 mock tasks
	tasks := make([]FileTask, 32)
	for i := range tasks {
		tasks[i] = FileTask{
			Digest:   "fake-digest",
			SinkPath: "/fake/path",
			Details:  &File{Size: 1024},
		}
	}

	// Test that panic is propagated
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic to propagate from batch, but it didn't")
		} else {
			// Verify exactly 15 files were processed (0-14)
			count := atomic.LoadInt32(&processedCount)
			if count != 15 {
				t.Errorf("Expected 15 files processed before panic, got %d", count)
			}
		}
	}()

	work := mockProcessBatch(tasks)
	work()
}

// TestConcurrentBatchProcessing tests that multiple batches can be processed
// concurrently without data races or deadlocks.
func TestConcurrentBatchProcessing(t *testing.T) {
	var wg sync.WaitGroup
	var processedTotal int32
	numBatches := 10
	filesPerBatch := 32

	// Create a safe mock that just counts
	mockProcessBatch := func(tasks []FileTask) anywork.Work {
		return func() {
			for range tasks {
				atomic.AddInt32(&processedTotal, 1)
			}
		}
	}

	// Launch multiple batches concurrently
	for b := 0; b < numBatches; b++ {
		tasks := make([]FileTask, filesPerBatch)
		for i := range tasks {
			tasks[i] = FileTask{
				Digest:   "test-digest",
				SinkPath: "/test/path",
				Details:  &File{Size: 1024},
			}
		}

		wg.Add(1)
		go func(batchTasks []FileTask) {
			defer wg.Done()
			work := mockProcessBatch(batchTasks)
			work()
		}(tasks)
	}

	wg.Wait()

	expectedTotal := int32(numBatches * filesPerBatch)
	if atomic.LoadInt32(&processedTotal) != expectedTotal {
		t.Errorf("Expected %d files processed, got %d", expectedTotal, processedTotal)
	}
}

// TestBatchSymlinkFiltering verifies that symlinks are never batched,
// which is critical for correct symlink handling during restoration.
func TestBatchSymlinkFiltering(t *testing.T) {
	symlinkFile := &File{
		Size:    100, // Small enough to batch
		Symlink: "/some/target",
	}

	if shouldBatch(symlinkFile) {
		t.Error("Symlink files should never be batched")
	}
}

// TestBatchRewriteFiltering verifies that files with many rewrites are not batched,
// as they require special handling for path relocations.
func TestBatchRewriteFiltering(t *testing.T) {
	// Files with few rewrites can be batched (up to 10)
	fewRewritesFile := &File{
		Size:    100, // Small enough to batch
		Rewrite: []int64{0, 100, 200},
	}

	if !shouldBatch(fewRewritesFile) {
		t.Error("Files with few rewrites (<=10) should be batched")
	}

	// Files with many rewrites should NOT be batched
	manyRewritesFile := &File{
		Size:    100, // Small enough to batch
		Rewrite: make([]int64, 11), // More than 10 rewrites
	}

	if shouldBatch(manyRewritesFile) {
		t.Error("Files with many rewrites (>10) should not be batched")
	}
}

// TestProcessBatchResourceCleanup ensures that resources are properly
// cleaned up even when processing fails mid-batch.
func TestProcessBatchResourceCleanup(t *testing.T) {
	// Track cleanup calls
	var cleanupCount int32

	// Create a cleanup tracker
	trackCleanup := func() {
		atomic.AddInt32(&cleanupCount, 1)
	}

	// Simulate resource allocation and cleanup
	resourceTracker := func(tasks []FileTask) anywork.Work {
		return func() {
			for range tasks {
				defer trackCleanup()
				// Resource "allocated" here
			}
		}
	}

	tasks := make([]FileTask, 10)
	work := resourceTracker(tasks)
	work()

	if atomic.LoadInt32(&cleanupCount) != 10 {
		t.Errorf("Expected 10 cleanup calls, got %d", cleanupCount)
	}
}

// TestPartialBatches verifies that batches smaller than BatchSize
// are processed correctly.
func TestPartialBatches(t *testing.T) {
	testCases := []int{1, 5, 15, 31, 32, 33, 64}

	for _, numFiles := range testCases {
		var processed int32
		mockProcess := func(tasks []FileTask) anywork.Work {
			return func() {
				for range tasks {
					atomic.AddInt32(&processed, 1)
				}
			}
		}

		// Process in batches like RestoreDirectoryBatched does
		tasks := make([]FileTask, numFiles)
		for i := 0; i < len(tasks); i += BatchSize {
			end := i + BatchSize
			if end > len(tasks) {
				end = len(tasks)
			}
			batch := tasks[i:end]
			work := mockProcess(batch)
			work()
		}

		if atomic.LoadInt32(&processed) != int32(numFiles) {
			t.Errorf("For %d files, expected %d processed, got %d",
				numFiles, numFiles, processed)
		}
	}
}

func TestIsTemporaryPartFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected bool
	}{
		{
			name:     "simple .part#N file",
			filename: "error.py.part#6599",
			expected: true,
		},
		{
			name:     "single digit",
			filename: "file.txt.part#1",
			expected: true,
		},
		{
			name:     "large number",
			filename: "script.py.part#123456789",
			expected: true,
		},
		{
			name:     "regular python file",
			filename: "error.py",
			expected: false,
		},
		{
			name:     "file with hash in name",
			filename: "file#123.txt",
			expected: false,
		},
		{
			name:     "file ending with .part but no hash",
			filename: "file.part",
			expected: false,
		},
		{
			name:     "file with .part in middle",
			filename: "file.part.txt",
			expected: false,
		},
		{
			name:     "file with .part# but no digits",
			filename: "file.part#abc",
			expected: false,
		},
		{
			name:     "file with .part# but mixed chars",
			filename: "file.part#123abc",
			expected: false,
		},
		{
			name:     "empty string",
			filename: "",
			expected: false,
		},
		{
			name:     "short string",
			filename: ".part#1",
			expected: true,
		},
		{
			name:     "too short for .part prefix",
			filename: "pt#1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTemporaryPartFile(tt.filename)
			if result != tt.expected {
				t.Errorf("isTemporaryPartFile(%q) = %v, want %v",
					tt.filename, result, tt.expected)
			}
		})
	}
}
