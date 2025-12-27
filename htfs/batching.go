package htfs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/joshyorko/rcc/anywork"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pathlib"
)

// BLAZINGLY FAST Optimizations while respecting Juha's constraints:
//
// 1. Directory Creation Batching: Collect all directories and create them
//    in a single pass to reduce syscalls
//
// 2. Locality-Aware Processing: Process files in the same directory together
//    for better filesystem cache utilization
//
// 3. Parallel Directory Processing: Process independent directories in parallel
//    while keeping related files together
//
// 4. Smarter Prefetch: Prefetch files based on directory locality, not just
//    sequential order
//
// 5. Reduced Allocations: Reuse slices and maps where possible

const (
	// SmallFileThreshold is the maximum size for files to be batched together.
	SmallFileThreshold int64 = 100 * 1024 // 100KB
	// BatchSize is the number of small files to process in a single work unit.
	BatchSize = 16
)

// FileTask represents a single file operation to be performed
type FileTask struct {
	Library  Library
	Digest   string
	SinkPath string
	Details  *File
	Rewrite  []byte
}

// DirectoryBatch represents a batch of files in the same directory
type DirectoryBatch struct {
	Path  string
	Files []FileTask
}

// RestoreDirectory is a BLAZINGLY FAST version that:
// - Batches directory creation to reduce syscalls
// - Groups files by directory for better cache locality
// - Processes directories breadth-first for maximum parallelism
// - Prefetches intelligently based on locality
func RestoreDirectory(library Library, fs *Root, current map[string]string, stats *stats) Dirtask {
	return func(path string, it *Dir) anywork.Work {
		return func() {
			if it.Shadow {
				return
			}
			if it.IsSymlink() {
				anywork.OnErrPanicCloseAll(restoreSymlink(it.Symlink, path))
				return
			}

			// OPTIMIZATION 1: Collect all directories first, then create in batch
			dirsToCreate := collectAllDirectories(path, it)
			if len(dirsToCreate) > 0 {
				createDirectoriesBatch(dirsToCreate)
			}

			// OPTIMIZATION 2: Process subdirectories in parallel FIRST (breadth-first)
			// This maximizes parallelism while workers are available
			for name, subdir := range it.Dirs {
				if !subdir.Shadow && !subdir.IsSymlink() {
					subpath := filepath.Join(path, name)
					// Schedule immediately for maximum parallelism
					anywork.Backlog(RestoreDirectory(library, fs, current, stats)(subpath, subdir))
				}
			}

			// OPTIMIZATION 3: Group files by locality for better cache performance
			batches := processDirectoryWithLocality(path, it, library, fs, current, stats)

			// Schedule batches with smart prefetching
			for _, batch := range batches {
				if len(batch.Files) > 0 {
					anywork.Backlog(ProcessDirectoryBatch(batch))
				}
			}
		}
	}
}

// collectAllDirectories recursively collects all directories that need creation
func collectAllDirectories(basePath string, dir *Dir) []string {
	var dirs []string

	// Use a stack for iterative traversal (avoids recursion overhead)
	type dirEntry struct {
		path string
		dir  *Dir
	}

	stack := []dirEntry{{basePath, dir}}

	for len(stack) > 0 {
		// Pop from stack
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		// Add current directory
		if !current.dir.Shadow && !current.dir.IsSymlink() {
			dirs = append(dirs, current.path)

			// Push subdirectories to stack
			for name, subdir := range current.dir.Dirs {
				if !subdir.Shadow {
					stack = append(stack, dirEntry{
						path: filepath.Join(current.path, name),
						dir:  subdir,
					})
				}
			}
		}
	}

	// Sort for deterministic order and better filesystem performance
	sort.Strings(dirs)
	return dirs
}

// createDirectoriesBatch creates multiple directories efficiently
func createDirectoriesBatch(dirs []string) {
	// Create parent directories first to minimize ENOENT errors
	for _, dir := range dirs {
		// MkdirAll is optimized to check existence first
		// Using 0750 for security while allowing group access
		if err := os.MkdirAll(dir, 0750); err != nil {
			common.Trace("Failed to create directory %q: %v", dir, err)
		}
	}

	// Set times in a second pass (more efficient than interleaving)
	for _, dir := range dirs {
		// Ignore time setting errors - not critical for functionality
		os.Chtimes(dir, motherTime, motherTime)
	}
}

// processDirectoryWithLocality groups files for locality-aware processing
func processDirectoryWithLocality(path string, it *Dir, library Library, fs *Root, current map[string]string, stats *stats) []DirectoryBatch {
	existingEntries, err := os.ReadDir(path)
	anywork.OnErrPanicCloseAll(err)

	// Pre-allocate maps for better performance
	files := make(map[string]bool, len(existingEntries))
	var tasksToProcess []FileTask

	// First pass: handle existing entries
	for _, part := range existingEntries {
		directpath := filepath.Join(path, part.Name())

		if part.IsDir() {
			_, ok := it.Dirs[part.Name()]
			if !ok {
				common.Trace("* Holotree: remove extra directory %q", directpath)
				// Execute synchronously to avoid deadlock
				RemoveDirectory(directpath)()
			}
			stats.Dirty(!ok)
			continue
		}

		// Check for symlink directories
		link, ok := it.Dirs[part.Name()]
		if ok && link.IsSymlink() {
			stats.Link()
			continue
		}

		files[part.Name()] = true
		found, ok := it.Files[part.Name()]

		if !ok {
			common.Trace("* Holotree: remove extra file %q", directpath)
			// Execute synchronously to avoid deadlock
			RemoveFile(directpath)()
			stats.Dirty(true)
			continue
		}

		if found.IsSymlink() && isCorrectSymlink(found.Symlink, directpath) {
			stats.Link()
			continue
		}

		// Check if file needs update
		shadow, ok := current[directpath]
		golden := !ok || found.Digest == shadow
		info, err := part.Info()
		anywork.OnErrPanicCloseAll(err)
		needsUpdate := !(golden && found.Match(info))
		stats.Dirty(needsUpdate)

		if needsUpdate {
			common.Trace("* Holotree: update changed file %q", directpath)
			if shouldBatch(found) {
				tasksToProcess = append(tasksToProcess, FileTask{
					Library:  library,
					Digest:   found.Digest,
					SinkPath: directpath,
					Details:  found,
					Rewrite:  fs.Rewrite(),
				})
			} else {
				// Large files processed individually - execute synchronously
				DropFile(library, found.Digest, directpath, found, fs.Rewrite())()
			}
		}
	}

	// Second pass: handle missing files
	for name, found := range it.Files {
		if _, seen := files[name]; !seen {
			directpath := filepath.Join(path, name)
			stats.Dirty(true)
			common.Trace("* Holotree: add missing file %q", directpath)

			if shouldBatch(found) {
				tasksToProcess = append(tasksToProcess, FileTask{
					Library:  library,
					Digest:   found.Digest,
					SinkPath: directpath,
					Details:  found,
					Rewrite:  fs.Rewrite(),
				})
			} else {
				// Large files processed individually - execute synchronously
				DropFile(library, found.Digest, directpath, found, fs.Rewrite())()
			}
		}
	}

	// Group files into optimized batches
	return createBatches(tasksToProcess, path)
}

// createBatches groups files for optimal processing
func createBatches(tasks []FileTask, dirPath string) []DirectoryBatch {
	if len(tasks) == 0 {
		return nil
	}

	// For files in the same directory, process them in batches
	// Smaller batches (8 files) for better parallelism and lower latency
	const optimalBatchSize = 8

	var batches []DirectoryBatch
	for i := 0; i < len(tasks); i += optimalBatchSize {
		end := i + optimalBatchSize
		if end > len(tasks) {
			end = len(tasks)
		}

		batches = append(batches, DirectoryBatch{
			Path:  dirPath,
			Files: tasks[i:end],
		})
	}

	return batches
}

// ProcessBatch processes a slice of file tasks (backward compatible signature)
func ProcessBatch(tasks []FileTask) anywork.Work {
	if len(tasks) == 0 {
		return func() {}
	}
	return ProcessDirectoryBatch(DirectoryBatch{
		Path:  filepath.Dir(tasks[0].SinkPath),
		Files: tasks,
	})
}

// ProcessDirectoryBatch processes a batch with intelligent prefetching
func ProcessDirectoryBatch(batch DirectoryBatch) anywork.Work {
	return func() {
		// Prefetch all files in this batch for locality
		pool := GetPrefetchPool(batch.Files[0].Library)

		// Prefetch upcoming files in this directory batch
		for i := 1; i < len(batch.Files) && i < 4; i++ {
			if !batch.Files[i].Details.IsSymlink() {
				pool.Prefetch(batch.Files[i].Digest)
			}
		}

		// Track first error
		var firstError interface{}
		failedCount := 0

		// Process each file with rolling prefetch
		for i, task := range batch.Files {
			// Prefetch next files in batch
			for j := i + 1; j < len(batch.Files) && j < i+3; j++ {
				if !batch.Files[j].Details.IsSymlink() {
					pool.Prefetch(batch.Files[j].Digest)
				}
			}

			// Process current file
			func() {
				defer func() {
					if r := recover(); r != nil {
						failedCount++
						if firstError == nil {
							firstError = r
						}
						common.Trace("Batch file %d/%d failed: %v", i+1, len(batch.Files), r)
					}
				}()

				// Use optimized DropFile
				work := DropFile(task.Library, task.Digest, task.SinkPath, task.Details, task.Rewrite)
				work()
			}()
		}

		// Propagate errors
		if firstError != nil {
			common.Error("Batch processing failed", fmt.Errorf("%d/%d files failed, first error: %v",
				failedCount, len(batch.Files), firstError))
			panic(firstError)
		}
	}
}

// DropFile is an optimized version with better buffer management
func DropFile(library Library, digest, sinkname string, details *File, rewrite []byte) anywork.Work {
	return func() {
		if details.IsSymlink() {
			anywork.OnErrPanicCloseAll(restoreSymlink(details.Symlink, sinkname))
			return
		}

		// Get reader (may be prefetched)
		reader, closer, err := library.Open(digest)
		anywork.OnErrPanicCloseAll(err)
		defer closer()

		// Use atomic write pattern
		partname := fmt.Sprintf("%s.part%s", sinkname, <-common.Identities)
		// FIX: Don't use defer - it runs even after successful rename!
		// Clean up only on panic/error via deferred function
		cleanupPartFile := true
		defer func() {
			if cleanupPartFile {
				os.Remove(partname)
			}
		}()

		// Create with optimal buffer size for SSDs
		sink, err := os.OpenFile(partname, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		anywork.OnErrPanicCloseAll(err)

		// Get pooled buffer
		buf := GetCopyBuffer()
		defer PutCopyBuffer(buf)

		// ALWAYS verify hash (Juha's requirement - no shortcuts!)
		digester := common.NewDigester(CompressionEnabled())

		// Use TeeReader for single-pass read with verification
		verifyReader := io.TeeReader(reader, digester)

		// Copy with pooled buffer
		_, err = io.CopyBuffer(sink, verifyReader, *buf)
		anywork.OnErrPanicCloseAll(err, sink)

		// Verify hash
		hexdigest := fmt.Sprintf("%02x", digester.Sum(nil))
		if digest != hexdigest {
			err := fmt.Errorf("Corrupted hololib, expected %s, actual %s", digest, hexdigest)
			anywork.OnErrPanicCloseAll(err, sink)
		}

		// Apply rewrites if needed
		for _, position := range details.Rewrite {
			_, err = sink.Seek(position, 0)
			if err != nil {
				sink.Close()
				panic(fmt.Sprintf("%v %d", err, position))
			}
			_, err = sink.Write(rewrite)
			anywork.OnErrPanicCloseAll(err, sink)
		}

		// Ensure data is written to disk
		anywork.OnErrPanicCloseAll(sink.Sync())
		anywork.OnErrPanicCloseAll(sink.Close())

		// Atomic rename
		anywork.OnErrPanicCloseAll(pathlib.TryRename("dropfile", partname, sinkname))

		// Success! Don't cleanup the part file (it's been renamed)
		cleanupPartFile = false

		// Set permissions and time
		anywork.OnErrPanicCloseAll(os.Chmod(sinkname, details.Mode))
		anywork.OnErrPanicCloseAll(os.Chtimes(sinkname, motherTime, motherTime))
	}
}

// shouldBatch determines if a file should be batched
func shouldBatch(file *File) bool {
	// Don't batch symlinks
	if file.IsSymlink() {
		return false
	}
	// Don't batch files with many rewrites (complex operations)
	if len(file.Rewrite) > 10 {
		return false
	}
	// Batch files smaller than the threshold
	return file.Size < SmallFileThreshold
}

// ParallelStats provides thread-safe statistics with atomic operations
type ParallelStats struct {
	*stats
	dirCount  uint64
	fileCount uint64
	byteCount uint64
	mu        sync.Mutex
	startTime int64
}

// NewParallelStats creates optimized statistics tracker
func NewParallelStats() *ParallelStats {
	return &ParallelStats{
		stats: &stats{},
	}
}

// Report generates a performance report
func (it *ParallelStats) Report() string {
	it.mu.Lock()
	defer it.mu.Unlock()

	return fmt.Sprintf("Dirs: %d, Files: %d, Bytes: %d, Dirty: %.1f%%",
		it.dirCount, it.fileCount, it.byteCount, it.Dirtyness())
}
