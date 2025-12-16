package htfs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/joshyorko/rcc/anywork"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pathlib"
)

// HardlinkBatch represents a batch of hardlinks to create
type HardlinkBatch struct {
	Source string
	Targets []string
}

// HardlinkManager manages parallel hardlink creation with safety limits
type HardlinkManager struct {
	batches    []HardlinkBatch
	mu         sync.Mutex
	maxWorkers int
	stats      *HardlinkStats
}

// HardlinkStats tracks hardlink creation performance
type HardlinkStats struct {
	created   uint64
	failed    uint64
	skipped   uint64
	totalTime int64 // nanoseconds
}

// NewHardlinkManager creates a manager for parallel hardlink operations
func NewHardlinkManager() *HardlinkManager {
	// Conservative limit for hardlink workers
	// Hardlinks are fast syscalls, but we don't want to overwhelm the filesystem
	maxWorkers := runtime.NumCPU()
	if maxWorkers > 8 {
		maxWorkers = 8 // Cap at 8 for safety
	}

	return &HardlinkManager{
		batches:    make([]HardlinkBatch, 0, 100),
		maxWorkers: maxWorkers,
		stats:      &HardlinkStats{},
	}
}

// AddHardlink queues a hardlink for batch creation
func (hm *HardlinkManager) AddHardlink(source, target string) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	// Check if we can add to existing batch
	for i := range hm.batches {
		if hm.batches[i].Source == source {
			hm.batches[i].Targets = append(hm.batches[i].Targets, target)
			return
		}
	}

	// Create new batch
	hm.batches = append(hm.batches, HardlinkBatch{
		Source:  source,
		Targets: []string{target},
	})
}

// CreateAll creates all queued hardlinks in parallel
func (hm *HardlinkManager) CreateAll() error {
	if len(hm.batches) == 0 {
		return nil
	}

	common.Timeline("Creating %d hardlink batches", len(hm.batches))

	// Use a semaphore to limit concurrent hardlink operations
	sem := make(chan struct{}, hm.maxWorkers)
	var wg sync.WaitGroup
	errors := make(chan error, len(hm.batches))

	for _, batch := range hm.batches {
		wg.Add(1)
		go func(b HardlinkBatch) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Create hardlinks for this batch
			if err := hm.createBatch(b); err != nil {
				errors <- err
			}
		}(batch)
	}

	// Wait for all batches to complete
	wg.Wait()
	close(errors)

	// Collect errors
	var firstError error
	errorCount := 0
	for err := range errors {
		if firstError == nil {
			firstError = err
		}
		errorCount++
	}

	// Report statistics
	created := atomic.LoadUint64(&hm.stats.created)
	failed := atomic.LoadUint64(&hm.stats.failed)
	skipped := atomic.LoadUint64(&hm.stats.skipped)

	common.Debug("Hardlink stats: created=%d, failed=%d, skipped=%d",
		created, failed, skipped)

	if errorCount > 0 {
		return fmt.Errorf("hardlink creation had %d errors, first: %v", errorCount, firstError)
	}

	return nil
}

// createBatch creates all hardlinks in a batch
func (hm *HardlinkManager) createBatch(batch HardlinkBatch) error {
	// Verify source exists
	if !pathlib.IsFile(batch.Source) {
		atomic.AddUint64(&hm.stats.failed, uint64(len(batch.Targets)))
		return fmt.Errorf("hardlink source does not exist: %s", batch.Source)
	}

	// Create hardlinks to all targets
	for _, target := range batch.Targets {
		// Check if target already exists
		if pathlib.IsFile(target) {
			// Verify it's already a hardlink to the same source
			if isSameFile(batch.Source, target) {
				atomic.AddUint64(&hm.stats.skipped, 1)
				continue
			}
			// Different file, remove it
			os.Remove(target)
		}

		// Ensure target directory exists
		targetDir := filepath.Dir(target)
		if err := os.MkdirAll(targetDir, 0750); err != nil {
			atomic.AddUint64(&hm.stats.failed, 1)
			common.Trace("Failed to create directory for hardlink: %v", err)
			continue
		}

		// Create the hardlink
		if err := os.Link(batch.Source, target); err != nil {
			atomic.AddUint64(&hm.stats.failed, 1)
			common.Trace("Failed to create hardlink %s -> %s: %v", batch.Source, target, err)
			continue
		}

		atomic.AddUint64(&hm.stats.created, 1)
	}

	return nil
}

// isSameFile checks if two paths refer to the same file (via hardlink or same inode)
func isSameFile(path1, path2 string) bool {
	stat1, err1 := os.Stat(path1)
	stat2, err2 := os.Stat(path2)

	if err1 != nil || err2 != nil {
		return false
	}

	// Use os.SameFile to check if they're the same file
	return os.SameFile(stat1, stat2)
}

// RestoreDirectoryWithHardlinks is an optimized version that batches hardlink creation
func RestoreDirectoryWithHardlinks(library Library, fs *Root, current map[string]string, stats *stats) Dirtask {
	// Track files that could be hardlinked
	hardlinkManager := NewHardlinkManager()

	return func(path string, it *Dir) anywork.Work {
		return func() {
			if it.Shadow {
				return
			}
			if it.IsSymlink() {
				anywork.OnErrPanicCloseAll(restoreSymlink(it.Symlink, path))
				return
			}

			// Process subdirectories first
			for name, subdir := range it.Dirs {
				if !subdir.Shadow && !subdir.IsSymlink() {
					subpath := filepath.Join(path, name)
					anywork.Backlog(RestoreDirectoryWithHardlinks(library, fs, current, stats)(subpath, subdir))
				}
			}

			existingEntries, err := os.ReadDir(path)
			anywork.OnErrPanicCloseAll(err)

			files := make(map[string]bool)
			var filesToRestore []FileTask

			// Check existing files
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
					common.Trace("* Holotree: remove extra file %q", directpath)
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

					// Check if this could be a hardlink candidate
					if isHardlinkCandidate(found) {
						// Safe type assertion with fallback
						if hl, ok := library.(MutableLibrary); ok {
							sourceFile := hl.Location(found.Digest)
							sourceFilePath := filepath.Join(sourceFile, found.Digest)

							// CRITICAL: Verify hash before creating hardlink (Juha's rule: "Always verify hash. No shortcuts.")
							if pathlib.IsFile(sourceFilePath) {
								// Verify the source file hash before using it
								hasher := common.NewDigester(Compress())
								file, err := os.Open(sourceFilePath)
								if err == nil {
									defer file.Close()
									_, err = io.Copy(hasher, file)
									hexdigest := fmt.Sprintf("%02x", hasher.Sum(nil))
									if err == nil && hexdigest == found.Digest {
										// Hash verified - safe to create hardlink
										hardlinkManager.AddHardlink(sourceFilePath, directpath)
									} else {
										// Hash mismatch or read error - restore normally
										common.Trace("Hash verification failed for %s, restoring normally", found.Digest[:8])
										filesToRestore = append(filesToRestore, FileTask{
											Library:  library,
											Digest:   found.Digest,
											SinkPath: directpath,
											Details:  found,
											Rewrite:  fs.Rewrite(),
										})
									}
								} else {
									// Could not open source - restore normally
									filesToRestore = append(filesToRestore, FileTask{
										Library:  library,
										Digest:   found.Digest,
										SinkPath: directpath,
										Details:  found,
										Rewrite:  fs.Rewrite(),
									})
								}
							} else {
								// Source doesn't exist - restore normally
								filesToRestore = append(filesToRestore, FileTask{
									Library:  library,
									Digest:   found.Digest,
									SinkPath: directpath,
									Details:  found,
									Rewrite:  fs.Rewrite(),
								})
							}
						} else {
							// Not a MutableLibrary - fall back to regular restoration
							filesToRestore = append(filesToRestore, FileTask{
								Library:  library,
								Digest:   found.Digest,
								SinkPath: directpath,
								Details:  found,
								Rewrite:  fs.Rewrite(),
							})
						}
					} else {
						// Not a hardlink candidate, restore normally
						anywork.Backlog(DropFile(library, found.Digest, directpath, found, fs.Rewrite()))
					}
				}
			}

			// Check for missing files
			for name, found := range it.Files {
				if _, seen := files[name]; !seen {
					directpath := filepath.Join(path, name)
					stats.Dirty(true)
					common.Trace("* Holotree: add missing file %q", directpath)

					// Check if this could be a hardlink candidate
					if isHardlinkCandidate(found) {
						// Safe type assertion with fallback
						if hl, ok := library.(MutableLibrary); ok {
							sourceFile := hl.Location(found.Digest)
							sourceFilePath := filepath.Join(sourceFile, found.Digest)

							// CRITICAL: Verify hash before creating hardlink (Juha's rule: "Always verify hash. No shortcuts.")
							if pathlib.IsFile(sourceFilePath) {
								// Verify the source file hash before using it
								hasher := common.NewDigester(Compress())
								file, err := os.Open(sourceFilePath)
								if err == nil {
									defer file.Close()
									_, err = io.Copy(hasher, file)
									hexdigest := fmt.Sprintf("%02x", hasher.Sum(nil))
									if err == nil && hexdigest == found.Digest {
										// Hash verified - safe to create hardlink
										hardlinkManager.AddHardlink(sourceFilePath, directpath)
									} else {
										// Hash mismatch or read error - restore normally
										common.Trace("Hash verification failed for %s, restoring normally", found.Digest[:8])
										filesToRestore = append(filesToRestore, FileTask{
											Library:  library,
											Digest:   found.Digest,
											SinkPath: directpath,
											Details:  found,
											Rewrite:  fs.Rewrite(),
										})
									}
								} else {
									// Could not open source - restore normally
									filesToRestore = append(filesToRestore, FileTask{
										Library:  library,
										Digest:   found.Digest,
										SinkPath: directpath,
										Details:  found,
										Rewrite:  fs.Rewrite(),
									})
								}
							} else {
								// Source doesn't exist - restore normally
								filesToRestore = append(filesToRestore, FileTask{
									Library:  library,
									Digest:   found.Digest,
									SinkPath: directpath,
									Details:  found,
									Rewrite:  fs.Rewrite(),
								})
							}
						} else {
							// Not a MutableLibrary - fall back to regular restoration
							filesToRestore = append(filesToRestore, FileTask{
								Library:  library,
								Digest:   found.Digest,
								SinkPath: directpath,
								Details:  found,
								Rewrite:  fs.Rewrite(),
							})
						}
					} else {
						// Not a hardlink candidate, restore normally
						anywork.Backlog(DropFile(library, found.Digest, directpath, found, fs.Rewrite()))
					}
				}
			}

			// Create all hardlinks in parallel
			if err := hardlinkManager.CreateAll(); err != nil {
				common.Trace("Hardlink creation had errors: %v", err)
			}

			// Process remaining files that couldn't be hardlinked
			for i := 0; i < len(filesToRestore); i += BatchSize {
				end := i + BatchSize
				if end > len(filesToRestore) {
					end = len(filesToRestore)
				}
				batch := filesToRestore[i:end]
				anywork.Backlog(ProcessBatch(batch))
			}
		}
	}
}

// isHardlinkCandidate determines if a file is suitable for hardlinking
func isHardlinkCandidate(file *File) bool {
	// Don't hardlink symlinks
	if file.IsSymlink() {
		return false
	}

	// Don't hardlink files with rewrites (they need modification)
	if len(file.Rewrite) > 0 {
		return false
	}

	// Don't hardlink executable files (may need special handling)
	if file.Mode&0111 != 0 {
		return false
	}

	// Good candidate for hardlinking
	return true
}

// HardlinkCache tracks which files can be hardlinked
type HardlinkCache struct {
	eligible map[string]bool // digest -> can hardlink
	mu       sync.RWMutex
}

// NewHardlinkCache creates a cache for hardlink eligibility
func NewHardlinkCache() *HardlinkCache {
	return &HardlinkCache{
		eligible: make(map[string]bool),
	}
}

// IsEligible checks if a digest can be hardlinked
func (hc *HardlinkCache) IsEligible(digest string) bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.eligible[digest]
}

// SetEligible marks a digest as eligible for hardlinking
func (hc *HardlinkCache) SetEligible(digest string, eligible bool) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.eligible[digest] = eligible
}