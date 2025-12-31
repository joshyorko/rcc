package htfs

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/joshyorko/rcc/anywork"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/fail"
	"github.com/joshyorko/rcc/pathlib"
)

func justFileExistCheck(location string, path, name, digest string) anywork.Work {
	return func() {
		if !pathlib.IsFile(location) {
			fullpath := filepath.Join(path, name)
			panic(fmt.Errorf("Content for %q [%s] is missing; hololib is broken, requires check!", fullpath, digest))
		}
	}
}

func CatalogCheck(library MutableLibrary, fs *Root) Treetop {
	var tool Treetop
	scheduled := make(map[string]bool)
	tool = func(path string, it *Dir) error {
		for name, file := range it.Files {
			if !scheduled[file.Digest] {
				anywork.Backlog(justFileExistCheck(library.ExactLocation(file.Digest), path, name, file.Digest))
				scheduled[file.Digest] = true
			}
		}
		for name, subdir := range it.Dirs {
			err := tool(filepath.Join(path, name), subdir)
			if err != nil {
				return err
			}
		}
		return nil
	}
	return tool
}

func DigestMapper(target map[string]string) Treetop {
	var tool Treetop
	tool = func(path string, it *Dir) error {
		for name, subdir := range it.Dirs {
			tool(filepath.Join(path, name), subdir)
		}
		for name, file := range it.Files {
			target[file.Digest] = filepath.Join(path, name)
		}
		return nil
	}
	return tool
}

func DigestRecorder(target map[string]string) Treetop {
	var tool Treetop
	tool = func(path string, it *Dir) error {
		for name, subdir := range it.Dirs {
			tool(filepath.Join(path, name), subdir)
		}
		for name, file := range it.Files {
			target[filepath.Join(path, name)] = file.Digest
		}
		return nil
	}
	return tool
}

func IntegrityCheck(result map[string]string, needed map[string]map[string]bool) Treetop {
	var tool Treetop
	tool = func(path string, it *Dir) error {
		for name, subdir := range it.Dirs {
			tool(filepath.Join(path, name), subdir)
		}
		for name, file := range it.Files {
			if file.Name != file.Digest {
				result[filepath.Join(path, name)] = file.Digest
			} else {
				delete(needed, file.Digest)
			}
		}
		return nil
	}
	return tool
}

func CheckHasher(known map[string]map[string]bool) Filetask {
	return func(fullpath string, details *File) anywork.Work {
		return func() {
			_, ok := known[details.Name]
			if !ok {
				defer anywork.Backlog(RemoveFile(fullpath))
			}
			source, err := os.Open(fullpath)
			if err != nil {
				anywork.Backlog(RemoveFile(fullpath))
				panic(fmt.Sprintf("Open[check] %q, reason: %v", fullpath, err))
			}
			defer source.Close()

			// Use dual-format detection for reading
			format, err := detectFormat(source)
			if err != nil {
				anywork.Backlog(RemoveFile(fullpath))
				panic(fmt.Sprintf("Format[check] %q, reason: %v", fullpath, err))
			}

			var reader io.Reader
			switch format {
			case "zstd":
				zr, cleanup, zErr := getPooledDecoder(source)
				if zErr != nil {
					anywork.Backlog(RemoveFile(fullpath))
					panic(fmt.Sprintf("Zstd[check] %q, reason: %v", fullpath, zErr))
				}
				defer cleanup() // Return decoder to pool
				reader = zr
			case "gzip":
				gr, gErr := gzip.NewReader(source)
				if gErr != nil {
					anywork.Backlog(RemoveFile(fullpath))
					panic(fmt.Sprintf("Gzip[check] %q, reason: %v", fullpath, gErr))
				}
				defer gr.Close()
				reader = gr
			default:
				reader = source
			}

			digest := common.NewDigester(CompressionEnabled())
			_, err = io.Copy(digest, reader)
			if err != nil {
				anywork.Backlog(RemoveFile(fullpath))
				panic(fmt.Sprintf("Copy[check] %q, reason: %v", fullpath, err))
			}
			details.Digest = fmt.Sprintf("%02x", digest.Sum(nil))
		}
	}
}

func Locator(seek string) Filetask {
	return func(fullpath string, details *File) anywork.Work {
		return func() {
			source, err := os.Open(fullpath)
			if err != nil {
				panic(fmt.Sprintf("Open[Locator] %q, reason: %v", fullpath, err))
			}
			defer source.Close()
			digest := common.NewDigester(CompressionEnabled())
			locator := RelocateWriter(digest, seek)
			_, err = io.Copy(locator, source)
			if err != nil {
				panic(fmt.Sprintf("Copy[Locator] %q, reason: %v", fullpath, err))
			}
			details.Rewrite = locator.Locations()
			details.Digest = fmt.Sprintf("%02x", digest.Sum(nil))
		}
	}
}

func MakeBranches(path string, it *Dir) error {
	if it.Shadow || it.IsSymlink() {
		return nil
	}
	if _, ok := pathlib.Symlink(path); ok {
		os.Remove(path)
	}
	hasSymlinks := false
detector:
	for _, subdir := range it.Dirs {
		if subdir.IsSymlink() {
			hasSymlinks = true
			break detector
		}
	}
	if hasSymlinks {
		err := os.MkdirAll(path, 0o750)
		if err != nil {
			return err
		}
	}
	for _, subdir := range it.Dirs {
		err := MakeBranches(filepath.Join(path, subdir.Name), subdir)
		if err != nil {
			return err
		}
	}
	if len(it.Dirs) == 0 {
		err := os.MkdirAll(path, 0o750)
		if err != nil {
			return err
		}
	}
	return os.Chtimes(path, motherTime, motherTime)
}

func ScheduleLifters(library MutableLibrary, stats *stats) Treetop {
	var scheduler Treetop
	compress := CompressionEnabled()
	seen := make(map[string]bool)
	scheduler = func(path string, it *Dir) error {
		if it.IsSymlink() {
			return nil
		}
		for name, subdir := range it.Dirs {
			scheduler(filepath.Join(path, name), subdir)
		}
		for name, file := range it.Files {
			if file.IsSymlink() {
				stats.Link()
				continue
			}
			if seen[file.Digest] {
				common.Trace("LiftFile %s %q already scheduled.", file.Digest, name)
				stats.Duplicate()
				continue
			}
			seen[file.Digest] = true
			directory := library.Location(file.Digest)
			if !seen[directory] && !pathlib.IsDir(directory) {
				pathlib.MakeSharedDir(directory)
			}
			seen[directory] = true
			sinkpath := filepath.Join(directory, file.Digest)
			ok := pathlib.IsFile(sinkpath)
			stats.Dirty(!ok)
			if ok {
				continue
			}
			sourcepath := filepath.Join(path, name)
			anywork.Backlog(LiftFile(sourcepath, sinkpath, compress))
		}
		return nil
	}
	return scheduler
}

func LiftFile(sourcename, sinkname string, compress bool) anywork.Work {
	return func() {
		source, err := os.Open(sourcename)
		anywork.OnErrPanicCloseAll(err)

		defer source.Close()
		partname := fmt.Sprintf("%s.part%s", sinkname, <-common.Identities)
		defer os.Remove(partname)
		sink, err := os.Create(partname)
		anywork.OnErrPanicCloseAll(err)

		defer sink.Close()

		var writer io.WriteCloser
		var encoderCleanup func()
		writer = sink
		if compress {
			if runtime.GOOS == "windows" {
				// Windows: use gzip for faster compression (zstd encoder is slow on Windows)
				// Decompression still handles both formats via magic byte detection
				gzWriter := gzip.NewWriter(sink)
				writer = gzWriter
				encoderCleanup = func() {} // gzip has no pool
			} else {
				// Linux/macOS: use pooled zstd encoder for better compression
				encoder, cleanup, err := GetPooledEncoder(sink)
				anywork.OnErrPanicCloseAll(err, sink)
				writer = encoder
				encoderCleanup = cleanup
			}
			defer encoderCleanup()
		}

		// Use pooled 256KB buffer for better SSD performance
		buf := GetCopyBuffer()
		defer PutCopyBuffer(buf)

		_, err = io.CopyBuffer(writer, source, *buf)
		anywork.OnErrPanicCloseAll(err, sink)

		if compress {
			anywork.OnErrPanicCloseAll(writer.Close(), sink)
		}

		anywork.OnErrPanicCloseAll(sink.Close())

		// Removed runtime.Gosched() - unnecessary scheduling hint that hurts performance
		// The OS scheduler is smart enough to handle this without hints

		anywork.OnErrPanicCloseAll(pathlib.TryRename("liftfile", partname, sinkname))
		pathlib.MakeSharedFile(sinkname)
	}
}

// DropFileWithPrefetch is an optimized version of DropFile that prefetches upcoming files
func DropFileWithPrefetch(library Library, digest, sinkname string, details *File, rewrite []byte, upcomingDigests []string) anywork.Work {
	return func() {
		if details.IsSymlink() {
			anywork.OnErrPanicCloseAll(restoreSymlink(details.Symlink, sinkname))
			return
		}

		// Use prefetching for better I/O throughput
		reader, closer, err := OpenWithPrefetch(library, digest, upcomingDigests)
		anywork.OnErrPanicCloseAll(err)

		defer closer()

		// DEFENSIVE: Ensure parent directory exists before creating file
		parentDir := filepath.Dir(sinkname)
		if err := os.MkdirAll(parentDir, 0750); err != nil {
			common.Trace("Failed to ensure parent directory %s: %v", parentDir, err)
		}

		partname := fmt.Sprintf("%s.part%s", sinkname, <-common.Identities)
		// FIX: Don't use defer - it runs even after successful rename!
		// Clean up only on panic/error via deferred function
		cleanupPartFile := true
		defer func() {
			if cleanupPartFile {
				os.Remove(partname)
			}
		}()

		sink, err := os.Create(partname)
		anywork.OnErrPanicCloseAll(err)

		// Get pooled 256KB buffer for better SSD performance
		buf := GetCopyBuffer()
		defer PutCopyBuffer(buf)

		// ALWAYS verify hash - bit rot is real, small files are attack vectors
		// Juha was right: catalogs tell what SHOULD be there, not what IS there
		digester := common.NewDigester(CompressionEnabled())
		many := io.MultiWriter(sink, digester)
		_, err = io.CopyBuffer(many, reader, *buf)
		anywork.OnErrPanicCloseAll(err, sink)
		hexdigest := fmt.Sprintf("%02x", digester.Sum(nil))
		if digest != hexdigest {
			err := fmt.Errorf("Corrupted hololib, expected %s, actual %s", digest, hexdigest)
			anywork.OnErrPanicCloseAll(err, sink)
		}

		for _, position := range details.Rewrite {
			_, err = sink.Seek(position, 0)
			if err != nil {
				sink.Close()
				panic(fmt.Sprintf("%v %d", err, position))
			}
			_, err = sink.Write(rewrite)
			anywork.OnErrPanicCloseAll(err, sink)
		}

		anywork.OnErrPanicCloseAll(sink.Close())

		// Atomic rename with retry on directory deletion race
		err = pathlib.TryRename("dropfile", partname, sinkname)
		if err != nil && os.IsNotExist(err) {
			// Directory was deleted by parallel cleanup - recreate and retry
			common.Trace("Directory deleted during file write, recreating: %s", parentDir)
			if mkErr := os.MkdirAll(parentDir, 0750); mkErr == nil {
				err = pathlib.TryRename("dropfile", partname, sinkname)
			}
		}
		anywork.OnErrPanicCloseAll(err)

		// Success! Don't cleanup the part file (it's been renamed)
		cleanupPartFile = false

		anywork.OnErrPanicCloseAll(os.Chmod(sinkname, details.Mode))
		anywork.OnErrPanicCloseAll(os.Chtimes(sinkname, motherTime, motherTime))
	}
}

func DropFileSimple(library Library, digest, sinkname string, details *File, rewrite []byte) anywork.Work {
	return func() {
		if details.IsSymlink() {
			anywork.OnErrPanicCloseAll(restoreSymlink(details.Symlink, sinkname))
			return
		}
		reader, closer, err := library.Open(digest)
		anywork.OnErrPanicCloseAll(err)

		defer closer()

		// DEFENSIVE: Ensure parent directory exists before creating file
		parentDir := filepath.Dir(sinkname)
		if err := os.MkdirAll(parentDir, 0750); err != nil {
			common.Trace("Failed to ensure parent directory %s: %v", parentDir, err)
		}

		partname := fmt.Sprintf("%s.part%s", sinkname, <-common.Identities)
		// FIX: Don't use defer - it runs even after successful rename!
		// Clean up only on panic/error via deferred function
		cleanupPartFile := true
		defer func() {
			if cleanupPartFile {
				os.Remove(partname)
			}
		}()

		sink, err := os.Create(partname)
		anywork.OnErrPanicCloseAll(err)

		// Get pooled 256KB buffer for better SSD performance
		buf := GetCopyBuffer()
		defer PutCopyBuffer(buf)

		// ALWAYS verify hash - bit rot is real, small files are attack vectors
		// Juha was right: catalogs tell what SHOULD be there, not what IS there
		digester := common.NewDigester(CompressionEnabled())
		many := io.MultiWriter(sink, digester)
		_, err = io.CopyBuffer(many, reader, *buf)
		anywork.OnErrPanicCloseAll(err, sink)
		hexdigest := fmt.Sprintf("%02x", digester.Sum(nil))
		if digest != hexdigest {
			err := fmt.Errorf("Corrupted hololib, expected %s, actual %s", digest, hexdigest)
			anywork.OnErrPanicCloseAll(err, sink)
		}

		for _, position := range details.Rewrite {
			_, err = sink.Seek(position, 0)
			if err != nil {
				sink.Close()
				panic(fmt.Sprintf("%v %d", err, position))
			}
			_, err = sink.Write(rewrite)
			anywork.OnErrPanicCloseAll(err, sink)
		}

		anywork.OnErrPanicCloseAll(sink.Close())

		// Atomic rename with retry on directory deletion race
		err = pathlib.TryRename("dropfile", partname, sinkname)
		if err != nil && os.IsNotExist(err) {
			// Directory was deleted by parallel cleanup - recreate and retry
			common.Trace("Directory deleted during file write, recreating: %s", parentDir)
			if mkErr := os.MkdirAll(parentDir, 0750); mkErr == nil {
				err = pathlib.TryRename("dropfile", partname, sinkname)
			}
		}
		anywork.OnErrPanicCloseAll(err)

		// Success! Don't cleanup the part file (it's been renamed)
		cleanupPartFile = false

		anywork.OnErrPanicCloseAll(os.Chmod(sinkname, details.Mode))
		anywork.OnErrPanicCloseAll(os.Chtimes(sinkname, motherTime, motherTime))
	}
}

func RemoveFile(filename string) anywork.Work {
	return func() {
		anywork.OnErrPanicCloseAll(pathlib.TryRemove("file", filename))
	}
}

func RemoveDirectory(dirname string) anywork.Work {
	return func() {
		anywork.OnErrPanicCloseAll(pathlib.TryRemoveAll("directory", dirname))
	}
}

// isTemporaryPartFile checks if a filename is a temporary .part#N file
// created during atomic file write operations. These files should be
// skipped during directory cleanup to avoid race conditions with
// concurrent file write operations.
func isTemporaryPartFile(name string) bool {
	// Pattern: filename.part#N where N is a number
	// Generated by: fmt.Sprintf("%s.part%s", sinkname, <-common.Identities)
	// where Identities returns "#N"
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '#' {
			// Check if prefix ends with ".part"
			if i >= 5 && name[i-5:i] == ".part" {
				// Verify suffix is all digits
				for j := i + 1; j < len(name); j++ {
					if name[j] < '0' || name[j] > '9' {
						return false
					}
				}
				return true
			}
			return false
		}
	}
	return false
}

type TreeStats struct {
	sync.Mutex
	Directories uint64
	Files       uint64
	Bytes       uint64
	Identity    string
	Relocations uint64
}

func guessLocation(digest string) string {
	return filepath.Join(digest[:2], digest[2:4], digest[4:6], digest)
}

func CalculateTreeStats() (Dirtask, *TreeStats) {
	result := &TreeStats{}
	return func(path string, it *Dir) anywork.Work {
		return func() {
			result.Lock()
			defer result.Unlock()
			result.Directories += 1
			result.Files += uint64(len(it.Files))
			for _, file := range it.Files {
				result.Bytes += uint64(file.Size)
				if file.Name == "identity.yaml" {
					result.Identity = guessLocation(file.Digest)
				}
				if len(file.Rewrite) > 0 {
					result.Relocations += 1
				}
			}
		}
	}, result
}

func isCorrectSymlink(source, target string) bool {
	old, ok := pathlib.Symlink(target)
	return ok && old == source
}

func restoreSymlink(source, target string) error {
	// Fast path: symlink already exists and is correct
	if isCorrectSymlink(source, target) {
		return nil
	}

	// Try to create symlink - handles the common case where target doesn't exist
	err := os.Symlink(source, target)
	if err == nil {
		return nil
	}

	// If creation failed with "file exists", another worker may have created it
	// or there's a stale file. Check if it's now correct (race condition handling).
	if os.IsExist(err) {
		if isCorrectSymlink(source, target) {
			return nil // Another worker created it correctly
		}
		// Stale file/symlink exists - remove and retry
		os.RemoveAll(target)
		return os.Symlink(source, target)
	}

	// For other errors (e.g., parent dir doesn't exist), try remove + create
	os.RemoveAll(target)
	return os.Symlink(source, target)
}

func RestoreDirectorySimple(library Library, fs *Root, current map[string]string, stats *stats) Dirtask {
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
			files := make(map[string]bool)
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
					anywork.Backlog(DropFileSimple(library, found.Digest, directpath, found, fs.Rewrite()))
				}
			}
			for name, found := range it.Files {
				directpath := filepath.Join(path, name)
				_, seen := files[name]
				if !seen {
					stats.Dirty(true)
					common.Trace("* Holotree: add missing file       %q", directpath)
					anywork.Backlog(DropFileSimple(library, found.Digest, directpath, found, fs.Rewrite()))
				}
			}
		}
	}
}

type Zipper interface {
	Ignore(relativepath string)
	Add(fullpath, relativepath string) error
}

func ZipIgnore(library MutableLibrary, fs *Root, sink Zipper) Treetop {
	var tool Treetop
	baseline := common.HololibLocation()
	tool = func(path string, it *Dir) (err error) {
		defer fail.Around(&err)

		for _, file := range it.Files {
			location := library.ExactLocation(file.Digest)
			relative, err := filepath.Rel(baseline, location)
			if err == nil {
				sink.Ignore(relative)
			}
		}
		for name, subdir := range it.Dirs {
			err := tool(filepath.Join(path, name), subdir)
			fail.On(err != nil, "%v", err)
		}
		return nil
	}
	return tool
}

func ZipRoot(library MutableLibrary, fs *Root, sink Zipper) Treetop {
	var tool Treetop
	baseline := common.HololibLocation()
	tool = func(path string, it *Dir) (err error) {
		defer fail.Around(&err)

		for _, file := range it.Files {
			location := library.ExactLocation(file.Digest)
			relative, err := filepath.Rel(baseline, location)
			fail.On(err != nil, "Relative path error: %s -> %s -> %v", baseline, location, err)
			err = sink.Add(location, relative)
			fail.On(err != nil, "%v", err)
		}
		for name, subdir := range it.Dirs {
			err := tool(filepath.Join(path, name), subdir)
			fail.On(err != nil, "%v", err)
		}
		return nil
	}
	return tool
}

func LoadHololibHashes() (map[string]map[string]bool, map[string]map[string]bool) {
	catalogs, roots := LoadCatalogs()
	slots := make([]map[string]string, len(roots))
	for at, root := range roots {
		anywork.Backlog(DigestLoader(root, at, slots))
	}
	result := make(map[string]map[string]bool)
	needed := make(map[string]map[string]bool)
	runtime.Gosched()
	anywork.Sync()
	for at, slot := range slots {
		catalog := catalogs[at]
		for k := range slot {
			who, ok := needed[k]
			if !ok {
				who = make(map[string]bool)
				needed[k] = who
			}
			who[catalog] = true
			found, ok := result[k]
			if !ok {
				found = make(map[string]bool)
				result[k] = found
			}
			found[catalog] = true
		}
	}
	return result, needed
}

func DigestLoader(root *Root, at int, slots []map[string]string) anywork.Work {
	return func() {
		collector := make(map[string]string)
		task := DigestMapper(collector)
		err := task(root.Path, root.Tree)
		if err != nil {
			panic(fmt.Sprintf("Collecting dir %q, reason: %v", root.Path, err))
		}
		slots[at] = collector
		common.Trace("Root %q loaded.", root.Path)
	}
}

func ignoreFailedCatalogs(suspects Roots) Roots {
	roots := make(Roots, 0, len(suspects))
	for _, root := range suspects {
		if root != nil {
			roots = append(roots, root)
		}
	}
	return roots
}

func LoadCatalogs() ([]string, Roots) {
	common.TimelineBegin("catalog load start")
	defer common.TimelineEnd()
	catalogs := CatalogNames()
	roots := make(Roots, len(catalogs))
	for at, catalog := range catalogs {
		fullpath := filepath.Join(common.HololibCatalogLocation(), catalog)
		anywork.Backlog(CatalogLoader(fullpath, at, roots))
		catalogs[at] = fullpath
	}
	runtime.Gosched()
	anywork.Sync()
	return catalogs, ignoreFailedCatalogs(roots)
}

func CatalogLoader(catalog string, at int, roots Roots) anywork.Work {
	return func() {
		tempdir := filepath.Join(common.ProductTemp(), "shadow")
		shadow, err := NewRoot(tempdir)
		if err != nil {
			panic(fmt.Sprintf("Temp dir %q, reason: %v", tempdir, err))
		}
		err = shadow.LoadFrom(catalog)
		if err != nil {
			panic(fmt.Sprintf("Load %q, reason: %v", catalog, err))
		}
		roots[at] = shadow
		common.Trace("Catalog %q loaded.", catalog)
	}
}
