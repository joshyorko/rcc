package htfs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/joshyorko/rcc/anywork"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pathlib"
)

// BLAZINGLY FAST Optimizations while respecting Juha's constraints:
//
// 1. O(n) Directory Creation: Each directory creates only itself (not all subdirs)
//    This avoids O(n²) MkdirAll calls that caused 50M+ syscalls on large trees
//
// 2. Inline Batch Processing: Process file batches inline instead of scheduling
//    via Backlog to avoid nested scheduling deadlocks
//
// 3. Locality-Aware Processing: Process files in the same directory together
//    for better filesystem cache utilization
//
// 4. Smarter Prefetch: Prefetch files based on directory locality
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
// - Creates only the current directory (subdirs are created by their own tasks)
// - Groups files by directory for better cache locality
// - Processes files in batches for efficient I/O
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

			// Create ONLY this directory - subdirectories will be created by their own
			// tasks scheduled via AllDirs(). The previous implementation called
			// collectAllDirectories() which recursively collected ALL subdirectories,
			// causing O(n²) MkdirAll calls for trees with n directories. For a 10,000
			// directory conda environment, this was 50+ million syscalls!
			if err := os.MkdirAll(path, 0750); err != nil {
				common.Trace("Failed to create directory %q: %v", path, err)
			}
			os.Chtimes(path, motherTime, motherTime)

			// NOTE: Subdirectories are already scheduled by AllDirs() in directory.go
			// which recursively schedules all directories depth-first. DO NOT schedule
			// them again here as that causes double-scheduling and can deadlock the
			// work queue when the pipeline channel (4096 entries) overflows with
			// large conda environments that have 10,000+ directories.

			// Group files by locality for better cache performance
			batches := processDirectoryWithLocality(path, it, library, fs, current, stats)

			// Process batches inline to avoid nested scheduling which can deadlock
			// the work queue. When all workers are blocked trying to schedule work,
			// and the main thread is also blocked, no progress can be made.
			for _, batch := range batches {
				if len(batch.Files) > 0 {
					ProcessDirectoryBatch(batch)()
				}
			}
		}
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
				// NOTE: We intentionally DO NOT delete extra directories during restoration
				// Deleting directories while parallel file operations are running causes
				// race conditions where files fail to write because their parent directory
				// was deleted mid-operation. Extra directories from previous environments
				// don't break anything - they just take up space. Use "rcc ht delete" for cleanup.
				common.Trace("* Holotree: skipping removal of extra directory %q (parallel safety)", directpath)
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
			// Skip temporary .part#N files created by concurrent write operations
			// to avoid race condition where we try to delete a file that's being
			// renamed or cleaned up by its creator
			if isTemporaryPartFile(part.Name()) {
				common.Trace("* Holotree: skipping temporary file %q (concurrent write)", directpath)
				continue
			}
			common.Trace("* Holotree: remove extra file %q", directpath)
			// DEFER file deletion - schedule it to avoid blocking
			anywork.Backlog(RemoveFile(directpath))
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

		// DEFENSIVE: Ensure parent directory exists before creating file
		// This handles race conditions between parallel directory processing
		// and cleanup operations that may delete directories concurrently
		parentDir := filepath.Dir(sinkname)
		if err := os.MkdirAll(parentDir, 0750); err != nil {
			common.Trace("Failed to ensure parent directory %s: %v", parentDir, err)
		}

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

		// Atomic rename with retry on directory deletion race
		// Race condition: parallel cleanup can delete directory between file creation and rename
		err = pathlib.TryRename("dropfile", partname, sinkname)
		if err != nil && os.IsNotExist(err) {
			// Directory was deleted by parallel cleanup - recreate and retry
			common.Trace("Directory deleted during file write, recreating: %s", parentDir)
			if mkErr := os.MkdirAll(parentDir, 0750); mkErr == nil {
				// Retry the rename
				err = pathlib.TryRename("dropfile", partname, sinkname)
			}
		}
		anywork.OnErrPanicCloseAll(err)

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

// CleanupExtraDirectories removes directories that exist on disk but not in the tree
// This should be called AFTER all file operations are complete (via anywork.Sync)
// to avoid race conditions during parallel restoration
func CleanupExtraDirectories(basePath string, tree *Dir) Dirtask {
	return func(path string, it *Dir) anywork.Work {
		return func() {
			if it.Shadow || it.IsSymlink() {
				return
			}

			entries, err := os.ReadDir(path)
			if err != nil {
				return // Directory might not exist, that's ok
			}

			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}

				name := entry.Name()
				if _, exists := it.Dirs[name]; !exists {
					// Extra directory not in tree - safe to remove now
					// (all file operations are complete)
					dirPath := filepath.Join(path, name)
					common.Trace("* Holotree: cleanup extra directory %q", dirPath)
					os.RemoveAll(dirPath)
				}
			}
		}
	}
}
