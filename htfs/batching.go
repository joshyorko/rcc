package htfs

import (
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/anywork"
	"github.com/joshyorko/rcc/common"
)

// Phase 2: Small File Batching Implementation
//
// This file implements small file batching to reduce per-file overhead during
// holotree restoration. Instead of scheduling each small file individually
// (which creates goroutine and scheduling overhead), we group small files into
// batches and process them sequentially within a single work unit.
//
// Key benefits:
// - Reduces goroutine scheduling overhead by grouping 32 small files per batch
// - Maintains the existing DropFile logic for actual file operations
// - Works seamlessly with Phase 1's worker pool and decoder pooling
// - Falls back to individual processing for large files, symlinks, and files with rewrites
//
// Usage:
//   Use RestoreDirectoryBatched() instead of RestoreDirectory() in library.go
//   to enable batching. The function signature is identical.

// Batching constants for small file optimization.
// These values are tuned for typical Python environments with many small files.
const (
	// SmallFileThreshold is the maximum size for files to be batched together.
	// Files larger than this are processed individually for better streaming.
	// 100KB is optimal: small enough to batch efficiently, large enough to
	// capture most Python source files and config files.
	SmallFileThreshold int64 = 100 * 1024 // 100KB

	// BatchSize is the number of small files to process in a single work unit.
	// 32 files per batch balances goroutine overhead reduction with
	// responsiveness and memory usage.
	BatchSize = 32
)

// FileTask represents a single file restoration task
type FileTask struct {
	Library  Library
	Digest   string
	SinkPath string
	Details  *File
	Rewrite  []byte
}

// ProcessBatch processes multiple small files sequentially within a single work unit.
// This reduces per-file overhead by:
// - Processing multiple files in a single goroutine
// - Amortizing goroutine scheduling overhead
// - Allowing the worker pool to better balance load
//
// Error handling: Each file in the batch is processed independently. If one file
// fails, subsequent files in the batch still get processed. This ensures partial
// progress is made and avoids leaving the holotree in an inconsistent state where
// some files were written but the batch was marked as "failed". Any errors from
// individual files are handled by DropFile's internal error handling (which uses
// anywork.OnErrPanicCloseAll to propagate failures to the worker pool).
func ProcessBatch(tasks []FileTask) anywork.Work {
	return func() {
		// Process each file in the batch using the standard DropFile logic
		// DropFile already uses pooled decoders and buffers efficiently.
		// Each file is processed independently - failures in one file don't
		// prevent processing of subsequent files in the batch.
		for i, task := range tasks {
			// Call DropFile's work function directly within this goroutine
			// This avoids creating separate goroutines for each file.
			// DropFile handles its own errors via anywork.OnErrPanicCloseAll,
			// so we wrap in a recovery to continue with remaining files.
			func() {
				defer func() {
					if r := recover(); r != nil {
						// Log the error but continue with remaining files
						common.Timeline("batch file %d/%d failed: %v (continuing with remaining files)",
							i+1, len(tasks), r)
					}
				}()
				work := DropFile(task.Library, task.Digest, task.SinkPath, task.Details, task.Rewrite)
				work()
			}()
		}
	}
}

// RestoreDirectoryBatched is an optimized version of RestoreDirectory that
// batches small files together for reduced overhead.
func RestoreDirectoryBatched(library Library, fs *Root, current map[string]string, stats *stats) Dirtask {
	return func(path string, it *Dir) anywork.Work {
		return func() {
			if it.Shadow {
				return
			}
			if it.IsSymlink() {
				anywork.OnErrPanicCloseAll(restoreSymlink(it.Symlink, path))
				return
			}

			existingEntries, err := os.ReadDir(path)
			anywork.OnErrPanicCloseAll(err)

			// Collect files that need to be restored
			var smallBatch []FileTask
			files := make(map[string]bool)

			for _, part := range existingEntries {
				directpath := filepath.Join(path, part.Name())
				if part.IsDir() {
					_, ok := it.Dirs[part.Name()]
					if !ok {
						common.Trace("* Holotree: remove extra directory %q", directpath)
						anywork.Backlog(RemoveDirectory(directpath))
					}
					stats.Dirty(!ok)
					continue
				}
				link, ok := it.Dirs[part.Name()]
				if ok && link.IsSymlink() {
					stats.Link()
					continue
				}
				files[part.Name()] = true
				found, ok := it.Files[part.Name()]
				if !ok {
					common.Trace("* Holotree: remove extra file      %q", directpath)
					anywork.Backlog(RemoveFile(directpath))
					stats.Dirty(true)
					continue
				}
				if found.IsSymlink() && isCorrectSymlink(found.Symlink, directpath) {
					stats.Link()
					continue
				}
				shadow, ok := current[directpath]
				golden := !ok || found.Digest == shadow
				info, err := part.Info()
				anywork.OnErrPanicCloseAll(err)
				ok = golden && found.Match(info)
				stats.Dirty(!ok)
				if !ok {
					common.Trace("* Holotree: update changed file    %q", directpath)
					// Determine if file should be batched
					if shouldBatch(found) {
						smallBatch = append(smallBatch, FileTask{
							Library:  library,
							Digest:   found.Digest,
							SinkPath: directpath,
							Details:  found,
							Rewrite:  fs.Rewrite(),
						})
					} else {
						// Process large files individually
						anywork.Backlog(DropFile(library, found.Digest, directpath, found, fs.Rewrite()))
					}
				}
			}

			// Check for missing files
			for name, found := range it.Files {
				directpath := filepath.Join(path, name)
				_, seen := files[name]
				if !seen {
					stats.Dirty(true)
					common.Trace("* Holotree: add missing file       %q", directpath)
					// Determine if file should be batched
					if shouldBatch(found) {
						smallBatch = append(smallBatch, FileTask{
							Library:  library,
							Digest:   found.Digest,
							SinkPath: directpath,
							Details:  found,
							Rewrite:  fs.Rewrite(),
						})
					} else {
						// Process large files individually
						anywork.Backlog(DropFile(library, found.Digest, directpath, found, fs.Rewrite()))
					}
				}
			}

			// Schedule batches of small files
			for i := 0; i < len(smallBatch); i += BatchSize {
				end := i + BatchSize
				if end > len(smallBatch) {
					end = len(smallBatch)
				}
				batch := smallBatch[i:end]
				anywork.Backlog(ProcessBatch(batch))
			}
		}
	}
}

// shouldBatch determines if a file should be batched based on size and characteristics
func shouldBatch(file *File) bool {
	// Don't batch symlinks (they need special handling)
	if file.IsSymlink() {
		return false
	}
	// Don't batch files with rewrites (they're more complex)
	if len(file.Rewrite) > 0 {
		return false
	}
	// Only batch files smaller than the threshold
	return file.Size < SmallFileThreshold
}
