package htfs

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/robocorp/rcc/anywork"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/set"
)

var (
	killfile map[string]bool
)

func init() {
	killfile = make(map[string]bool)
	killfile["__MACOSX"] = true
	killfile["__pycache__"] = true
	killfile[".pyc"] = true
	killfile[".git"] = true
	killfile[".hg"] = true
	killfile[".svn"] = true
	killfile[".gitignore"] = true

	if !common.WarrantyVoided() {
		pathlib.MakeSharedDir(common.Product.HoloLocation())
		pathlib.MakeSharedDir(common.HololibCatalogLocation())
		pathlib.MakeSharedDir(common.HololibLibraryLocation())
		pathlib.MakeSharedDir(common.HololibUsageLocation())
		pathlib.MakeSharedDir(common.HololibPids())
	}
}

type (
	Filetask func(string, *File) anywork.Work
	Dirtask  func(string, *Dir) anywork.Work
	Treetop  func(string, *Dir) error

	Info struct {
		RccVersion string `json:"rcc"`
		Identity   string `json:"identity"`
		Path       string `json:"path"`
		Controller string `json:"controller"`
		Space      string `json:"space"`
		Platform   string `json:"platform"`
		Blueprint  string `json:"blueprint"`
	}

	Root struct {
		*Info
		Lifted bool `json:"lifted"`
		Tree   *Dir `json:"tree"`
		source string
	}

	Roots []*Root
	Dir   struct {
		Name    string           `json:"name"`
		Symlink string           `json:"symlink,omitempty"`
		Mode    fs.FileMode      `json:"mode"`
		Dirs    map[string]*Dir  `json:"subdirs"`
		Files   map[string]*File `json:"files"`
		Shadow  bool             `json:"shadow,omitempty"`
	}

	File struct {
		Name    string      `json:"name"`
		Symlink string      `json:"symlink,omitempty"`
		Size    int64       `json:"size"`
		Mode    fs.FileMode `json:"mode"`
		Digest  string      `json:"digest"`
		Rewrite []int64     `json:"rewrite"`
	}
)

func (it *Info) AsJson() ([]byte, error) {
	return json.MarshalIndent(it, "", "  ")
}

func (it *Info) saveAs(filename string) error {
	content, err := it.AsJson()
	if err != nil {
		return err
	}
	sink, err := pathlib.Create(filename)
	if err != nil {
		return err
	}
	defer sink.Close()
	defer sink.Sync()
	_, err = sink.Write(content)
	if err != nil {
		return err
	}
	return nil
}

func (it Roots) BaseFolders() []string {
	result := []string{}
	for _, root := range it {
		result = append(result, filepath.Dir(root.Path))
	}
	return set.Set(result)
}

func (it Roots) Spaces() Roots {
	roots := make(Roots, 0, 20)
	for directory, metafile := range it.Spacemap() {
		root, err := NewRoot(directory)
		if err != nil {
			continue
		}
		err = root.LoadFrom(metafile)
		if err != nil {
			continue
		}
		roots = append(roots, root)
	}
	return roots
}

func (it Roots) Spacemap() map[string]string {
	result := make(map[string]string)
	for _, basedir := range it.BaseFolders() {
		for _, metafile := range pathlib.Glob(basedir, "*.meta") {
			result[metafile[:len(metafile)-5]] = metafile
		}
	}
	return result
}

func (it Roots) FindEnvironments(fragments []string) []string {
	result := make([]string, 0, 10)
	for directory, _ := range it.Spacemap() {
		name := filepath.Base(directory)
		for _, fragment := range fragments {
			if strings.Contains(name, fragment) {
				result = append(result, name)
			}
		}
	}
	return set.Set(result)
}

func (it Roots) InstallationPlan(hash string) (string, bool) {
	for _, directory := range it.BaseFolders() {
		finalplan := filepath.Join(directory, hash, "rcc_plan.log")
		if pathlib.IsFile(finalplan) {
			return finalplan, true
		}
	}
	return "", false
}

func (it Roots) RemoveHolotreeSpace(label string) (err error) {
	defer fail.Around(&err)

	for directory, metafile := range it.Spacemap() {
		name := filepath.Base(directory)
		if name != label {
			continue
		}
		pathlib.TryRemove("metafile", metafile)
		pathlib.TryRemove("lockfile", directory+".lck")
		err = pathlib.TryRemoveAll("space", directory)
		fail.On(err != nil, "Problem removing %q, reason: %v.", directory, err)
		common.Timeline("removed holotree space %q", directory)
	}
	return nil
}

func NewInfo(path string) (*Info, error) {
	fullpath, err := pathlib.Abs(path)
	if err != nil {
		return nil, err
	}
	return &Info{
		RccVersion: common.Version,
		Identity:   filepath.Base(fullpath),
		Path:       fullpath,
		Platform:   common.Platform(),
	}, nil
}

func NewRoot(path string) (*Root, error) {
	info, err := NewInfo(path)
	if err != nil {
		return nil, err
	}
	return &Root{
		Info:   info,
		Lifted: false,
		Tree:   newDir("", "", false),
		source: info.Path,
	}, nil
}

func (it *Root) Top(count int) map[string]int64 {
	target := make(map[string]int64)
	it.Tree.fillSizes("", target)
	sizes := set.Values(target)
	total := len(sizes)
	if total > count {
		sizes = sizes[total-count:]
	}
	members := set.Membership(sizes)
	result := make(map[string]int64)
	for filename, size := range target {
		if members[size] {
			result[filename] = size
		}
	}
	return result
}

func (it *Root) Show(filename string) ([]byte, error) {
	return it.Tree.Show(filepath.SplitList(filename), filename)
}

func (it *Root) Source() string {
	return it.source
}

func (it *Root) Touch() {
	touchUsedHash(it.Blueprint)
}

func (it *Root) HolotreeBase() string {
	return filepath.Dir(it.Path)
}

func (it *Root) Signature() uint64 {
	return common.Sipit([]byte(strings.ToLower(fmt.Sprintf("%s %q", it.Platform, it.Path))))
}

func (it *Root) Rewrite() []byte {
	return []byte(it.Identity)
}

func (it *Root) Relocate(target string) error {
	locate := filepath.Dir(target)
	if it.HolotreeBase() != locate {
		return fmt.Errorf("Base directory mismatch: %q vs %q.", it.HolotreeBase(), locate)
	}
	basename := filepath.Base(target)
	if len(it.Identity) != len(basename) {
		return fmt.Errorf("Base name length mismatch: %q vs %q.", it.Identity, basename)
	}
	if len(it.Path) != len(target) {
		return fmt.Errorf("Path length mismatch: %q vs %q.", it.Path, target)
	}
	it.Path = target
	it.Identity = basename
	return nil
}

func (it *Root) Lift() error {
	if it.Lifted {
		return nil
	}
	it.Lifted = true
	return it.Tree.Lift(it.Path)
}

func (it *Root) Treetop(task Treetop) error {
	common.TimelineBegin("holotree treetop sync start")
	defer common.TimelineEnd()
	err := task(it.Path, it.Tree)
	if err != nil {
		return err
	}
	return anywork.Sync()
}

func (it *Root) Stats() (*TreeStats, error) {
	task, stats := CalculateTreeStats()
	err := it.AllDirs(task)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

func (it *Root) AllDirs(task Dirtask) error {
	common.TimelineBegin("holotree dirs sync start")
	defer common.TimelineEnd()
	it.Tree.AllDirs(it.Path, task)
	return anywork.Sync()
}

func (it *Root) AllFiles(task Filetask) error {
	common.TimelineBegin("holotree files sync start")
	defer common.TimelineEnd()
	it.Tree.AllFiles(it.Path, task)
	return anywork.Sync()
}

func (it *Root) AsJson() ([]byte, error) {
	return json.MarshalIndent(it, "", "  ")
}

func (it *Root) SaveAs(filename string) error {
	content, err := it.AsJson()
	if err != nil {
		return err
	}
	sink, err := pathlib.Create(filename)
	if err != nil {
		return err
	}
	defer sink.Close()
	defer sink.Sync()
	writer, err := gzip.NewWriterLevel(sink, gzip.BestSpeed)
	if err != nil {
		return err
	}
	defer writer.Close()
	_, err = writer.Write(content)
	if err != nil {
		return err
	}
	return it.Info.saveAs(filename + ".info")
}

func (it *Root) ReadFrom(source io.Reader) error {
	decoder := json.NewDecoder(source)
	return decoder.Decode(&it)
}

func (it *Root) LoadFrom(filename string) error {
	source, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer source.Close()
	reader, err := gzip.NewReader(source)
	if err != nil {
		return err
	}
	it.source = filename
	defer common.Timeline("holotree catalog %q loaded", filename)
	defer reader.Close()
	return it.ReadFrom(reader)
}

func showFile(filename string) (content []byte, err error) {
	defer fail.Around(&err)

	reader, closer, err := gzDelegateOpen(filename, true)
	fail.On(err != nil, "Failed to open %q, reason: %v", filename, err)
	defer closer()

	sink := bytes.NewBuffer(nil)
	_, err = io.Copy(sink, reader)
	fail.On(err != nil, "Failed to read %q, reason: %v", filename, err)
	return sink.Bytes(), nil
}

func (it *Dir) fillSizes(prefix string, target map[string]int64) {
	for filename, file := range it.Files {
		target[filepath.Join(prefix, filename)] = file.Size
	}
	for dirname, dir := range it.Dirs {
		dir.fillSizes(filepath.Join(prefix, dirname), target)
	}
}

func (it *Dir) Show(path []string, fullpath string) ([]byte, error) {
	if len(path) > 1 {
		subtree, ok := it.Dirs[path[0]]
		if !ok {
			return nil, fmt.Errorf("Not found: %s", fullpath)
		}
		return subtree.Show(path[1:], fullpath)
	}
	file, ok := it.Files[path[0]]
	if !ok {
		return nil, fmt.Errorf("Not found: %s", fullpath)
	}
	location := guessLocation(file.Digest)
	rawfile := filepath.Join(common.HololibLibraryLocation(), location)
	return showFile(rawfile)
}

func (it *Dir) IsSymlink() bool {
	return len(it.Symlink) > 0
}

func (it *Dir) AllDirs(path string, task Dirtask) {
	for name, dir := range it.Dirs {
		fullpath := filepath.Join(path, name)
		dir.AllDirs(fullpath, task)
	}
	anywork.Backlog(task(path, it))
}

func (it *Dir) AllFiles(path string, task Filetask) {
	for name, dir := range it.Dirs {
		fullpath := filepath.Join(path, name)
		dir.AllFiles(fullpath, task)
	}
	for name, file := range it.Files {
		fullpath := filepath.Join(path, name)
		anywork.Backlog(task(fullpath, file))
	}
}

func (it *Dir) Lift(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	it.Mode = stat.Mode()
	content, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	shadow := it.Shadow || it.IsSymlink()
	for _, part := range content {
		if killfile[part.Name()] || killfile[filepath.Ext(part.Name())] {
			continue
		}
		fullpath := filepath.Join(path, part.Name())
		// following must be done to get by symbolic links
		info, err := os.Stat(fullpath)
		if err != nil {
			return err
		}
		symlink, _ := pathlib.Symlink(fullpath)
		if info.IsDir() {
			it.Dirs[part.Name()] = newDir(info.Name(), symlink, shadow)
			continue
		}
		it.Files[part.Name()] = newFile(info, symlink)
	}
	for name, dir := range it.Dirs {
		err = dir.Lift(filepath.Join(path, name))
		if err != nil {
			return err
		}
	}
	return nil
}

func (it *File) IsSymlink() bool {
	return len(it.Symlink) > 0
}

func (it *File) Match(info fs.FileInfo) bool {
	name := it.Name == info.Name()
	size := it.Size == info.Size()
	mode := it.Mode == info.Mode()
	return name && size && mode
}

func newDir(name, symlink string, shadow bool) *Dir {
	return &Dir{
		Name:    name,
		Symlink: symlink,
		Dirs:    make(map[string]*Dir),
		Files:   make(map[string]*File),
		Shadow:  shadow,
	}
}

func newFile(info fs.FileInfo, symlink string) *File {
	return &File{
		Name:    info.Name(),
		Symlink: symlink,
		Mode:    info.Mode(),
		Size:    info.Size(),
		Digest:  "N/A",
		Rewrite: make([]int64, 0),
	}
}
