package htfs

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/joshyorko/rcc/anywork"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/fail"
)

// Note: SmallFileThreshold and FileTask are defined in batching.go
// We use PlanFileTask here to avoid conflicts while providing a simpler struct for planning

// PlanFileTask represents a file operation to be performed during restoration planning
type PlanFileTask struct {
	Digest  string
	Path    string
	Details *File
}

// SymlinkTask represents a symlink operation to be performed
type SymlinkTask struct {
	Source string
	Target string
}

// RestorationPlan represents a pre-computed plan for restoring a holotree
// All decisions are made upfront to avoid filesystem queries during execution
type RestorationPlan struct {
	// Directory operations
	DirsToCreate []string
	DirsToRemove []string

	// File operations
	FilesToCreate []PlanFileTask // New files that don't exist
	FilesToUpdate []PlanFileTask // Files that exist but need updating
	FilesToRemove []string       // Files that exist but shouldn't

	// Symlink operations
	SymlinksToCreate []SymlinkTask

	// Pre-sorted file lists for batching optimization
	SmallFiles []PlanFileTask // Files < 100KB
	LargeFiles []PlanFileTask // Files >= 100KB

	// Statistics for reporting
	TotalFiles    int
	TotalDirs     int
	TotalSymlinks int
	DirtyFiles    int
	DirtyDirs     int
}

// PlanRestoration walks the filesystem tree once and collects all restoration tasks
// This eliminates the need for filesystem queries during the actual restoration
func PlanRestoration(library Library, fs *Root, targetPath string, current map[string]string) (*RestorationPlan, error) {
	plan := &RestorationPlan{
		DirsToCreate:     make([]string, 0),
		DirsToRemove:     make([]string, 0),
		FilesToCreate:    make([]PlanFileTask, 0),
		FilesToUpdate:    make([]PlanFileTask, 0),
		FilesToRemove:    make([]string, 0),
		SymlinksToCreate: make([]SymlinkTask, 0),
		SmallFiles:       make([]PlanFileTask, 0),
		LargeFiles:       make([]PlanFileTask, 0),
	}

	// Walk the tree and collect all decisions
	err := planDirectory(library, fs, targetPath, fs.Tree, current, plan)
	if err != nil {
		return nil, err
	}

	// Categorize files by size for optimal batching
	allFiles := append(plan.FilesToCreate, plan.FilesToUpdate...)
	for _, task := range allFiles {
		if task.Details.Size < SmallFileThreshold {
			plan.SmallFiles = append(plan.SmallFiles, task)
		} else {
			plan.LargeFiles = append(plan.LargeFiles, task)
		}
	}

	// Sort for deterministic execution and better cache locality
	sort.Slice(plan.SmallFiles, func(i, j int) bool {
		return plan.SmallFiles[i].Path < plan.SmallFiles[j].Path
	})
	sort.Slice(plan.LargeFiles, func(i, j int) bool {
		return plan.LargeFiles[i].Path < plan.LargeFiles[j].Path
	})

	// Update statistics
	plan.TotalFiles = len(plan.FilesToCreate) + len(plan.FilesToUpdate)
	plan.TotalDirs = len(plan.DirsToCreate)
	plan.TotalSymlinks = len(plan.SymlinksToCreate)
	plan.DirtyFiles = len(plan.FilesToCreate) + len(plan.FilesToUpdate) + len(plan.FilesToRemove)
	plan.DirtyDirs = len(plan.DirsToCreate) + len(plan.DirsToRemove)

	return plan, nil
}

// planDirectory recursively plans directory restoration operations
func planDirectory(library Library, fs *Root, path string, dir *Dir, current map[string]string, plan *RestorationPlan) error {
	// Skip shadow and symlink directories
	if dir.Shadow {
		return nil
	}

	if dir.IsSymlink() {
		plan.SymlinksToCreate = append(plan.SymlinksToCreate, SymlinkTask{
			Source: dir.Symlink,
			Target: path,
		})
		return nil
	}

	// Check existing entries in the directory
	existingEntries, err := os.ReadDir(path)
	if err != nil {
		// Directory doesn't exist, will be created by MakeBranches
		return nil
	}

	existingFiles := make(map[string]bool)

	for _, entry := range existingEntries {
		entryPath := filepath.Join(path, entry.Name())

		// Handle directories
		if entry.IsDir() {
			if _, ok := dir.Dirs[entry.Name()]; !ok {
				// Extra directory that shouldn't exist
				plan.DirsToRemove = append(plan.DirsToRemove, entryPath)
			}
			continue
		}

		// Check if it's a symlink directory in the tree
		link, ok := dir.Dirs[entry.Name()]
		if ok && link.IsSymlink() {
			// This is handled as a symlink, not a file
			plan.SymlinksToCreate = append(plan.SymlinksToCreate, SymlinkTask{
				Source: link.Symlink,
				Target: entryPath,
			})
			continue
		}

		// Handle files
		existingFiles[entry.Name()] = true
		found, ok := dir.Files[entry.Name()]
		if !ok {
			// Skip temporary .part#N files created by concurrent write operations
			if isTemporaryPartFile(entry.Name()) {
				continue
			}
			// Extra file that shouldn't exist
			plan.FilesToRemove = append(plan.FilesToRemove, entryPath)
			continue
		}

		// Check if file is a symlink
		if found.IsSymlink() {
			if !isCorrectSymlink(found.Symlink, entryPath) {
				plan.SymlinksToCreate = append(plan.SymlinksToCreate, SymlinkTask{
					Source: found.Symlink,
					Target: entryPath,
				})
			}
			continue
		}

		// Check if file needs updating
		shadow, ok := current[entryPath]
		golden := !ok || found.Digest == shadow

		info, err := entry.Info()
		if err != nil {
			// If we can't stat it, schedule for update
			plan.FilesToUpdate = append(plan.FilesToUpdate, PlanFileTask{
				Digest:  found.Digest,
				Path:    entryPath,
				Details: found,
			})
			continue
		}

		needsUpdate := !(golden && found.Match(info))
		if needsUpdate {
			plan.FilesToUpdate = append(plan.FilesToUpdate, PlanFileTask{
				Digest:  found.Digest,
				Path:    entryPath,
				Details: found,
			})
		}
	}

	// Check for missing files that need to be created
	for name, file := range dir.Files {
		if !existingFiles[name] {
			entryPath := filepath.Join(path, name)
			plan.FilesToCreate = append(plan.FilesToCreate, PlanFileTask{
				Digest:  file.Digest,
				Path:    entryPath,
				Details: file,
			})
		}
	}

	// Recursively plan subdirectories
	for name, subdir := range dir.Dirs {
		subPath := filepath.Join(path, name)
		err := planDirectory(library, fs, subPath, subdir, current, plan)
		if err != nil {
			return err
		}
	}

	return nil
}

// ExecuteAsync runs the restoration plan using the anywork infrastructure for parallel file operations
func (p *RestorationPlan) ExecuteAsync(library Library, rewrite []byte, stats *stats) (err error) {
	defer fail.Around(&err)

	// Remove extra directories (must be synchronous before file operations)
	for _, dir := range p.DirsToRemove {
		common.Trace("* Holotree: remove extra directory %q", dir)
		stats.Dirty(true)
		anywork.Backlog(RemoveDirectory(dir))
	}

	// Remove extra files (can be async)
	for _, file := range p.FilesToRemove {
		common.Trace("* Holotree: remove extra file      %q", file)
		stats.Dirty(true)
		anywork.Backlog(RemoveFile(file))
	}

	// Schedule large files for async processing
	for _, task := range p.LargeFiles {
		stats.Dirty(true)
		if fileExists(task.Path) {
			common.Trace("* Holotree: update changed file    %q", task.Path)
		} else {
			common.Trace("* Holotree: add missing file       %q", task.Path)
		}
		anywork.Backlog(DropFile(library, task.Digest, task.Path, task.Details, rewrite))
	}

	// Batch small files using the batching infrastructure
	var smallBatch []FileTask
	for _, task := range p.SmallFiles {
		stats.Dirty(true)
		if fileExists(task.Path) {
			common.Trace("* Holotree: update changed file    %q", task.Path)
		} else {
			common.Trace("* Holotree: add missing file       %q", task.Path)
		}
		smallBatch = append(smallBatch, FileTask{
			Library:  library,
			Digest:   task.Digest,
			SinkPath: task.Path,
			Details:  task.Details,
			Rewrite:  rewrite,
		})
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

	// Create symlinks (can be async)
	for _, task := range p.SymlinksToCreate {
		stats.Link()
		// Capture task in local variable to avoid closure bug
		t := task
		anywork.Backlog(func() {
			anywork.OnErrPanicCloseAll(restoreSymlink(t.Source, t.Target))
		})
	}

	return nil
}

// fileExists checks if a file exists (not a directory)
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
