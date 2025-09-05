package operations

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/robot"
	"github.com/robocorp/rcc/set"
)

const (
	backslash = `\`
	slash     = `/`
)

var (
	libraryPattern = regexp.MustCompile("(?i)^library[/\\\\]{1,2}[0-9a-f]{2}[/\\\\]{1,2}[0-9a-f]{2}[/\\\\]{1,2}[0-9a-f]{2}[/\\\\]{1,2}[0-9a-f]{64}$")
	catalogPattern = regexp.MustCompile("(?i)^catalog[/\\\\]{1,2}[0-9a-f]{16}v[0-9a-f]{2}\\.(?:windows|darwin|linux)_(?:amd64|arm64)")
)

type (
	Verifier func(file *zip.File) error

	WriteTarget struct {
		Source *zip.File
		Target string
	}

	Command interface {
		Execute() bool
	}

	CommandChannel   chan Command
	CompletedChannel chan bool
)

func slashed(text string) string {
	return strings.Replace(text, backslash, slash, -1)
}

func HololibZipShape(file *zip.File) error {
	library := libraryPattern.MatchString(file.Name)
	catalog := catalogPattern.MatchString(file.Name)
	if !library && !catalog {
		return fmt.Errorf("filename %q does not match Holotree catalog or library entry pattern.", file.Name)
	}
	return nil
}

func (it *WriteTarget) Execute() bool {
	err := it.execute()
	if err != nil {
		common.Error("zip extract", err)
		common.Debug("  - failure with %q, reason: %v", it.Target, err)
	}
	return err == nil
}

func (it *WriteTarget) execute() error {
	source, err := it.Source.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := pathlib.Create(it.Target)
	if err != nil {
		return err
	}
	defer target.Close()
	common.Trace("- %v", it.Target)
	_, err = io.Copy(target, source)
	if err != nil {
		return err
	}
	os.Chtimes(it.Target, it.Source.Modified, it.Source.Modified)
	return nil
}

type unzipper struct {
	reader  *zip.Reader
	closer  io.Closer
	flatten bool
}

func (it *unzipper) Close() {
	it.closer.Close()
}

func newPayloadUnzipper(filename string) (*unzipper, error) {
	payloader, err := PayloadReaderAt(filename)
	if err != nil {
		return nil, err
	}
	reader, err := zip.NewReader(payloader, payloader.Limit())
	if err != nil {
		return nil, err
	}
	return &unzipper{
		reader:  reader,
		closer:  payloader,
		flatten: false,
	}, nil
}

func newUnzipper(filename string, flatten bool) (*unzipper, error) {
	reader, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	return &unzipper{
		reader:  &reader.Reader,
		closer:  reader,
		flatten: flatten,
	}, nil
}

func loopExecutor(work CommandChannel, done CompletedChannel) {
	// This is PoC code, for parallel extraction
	for {
		task, ok := <-work
		if !ok {
			break
		}
		task.Execute()
	}
	done <- true
}

func (it *unzipper) VerifyShape(verifier Verifier) []error {
	errors := []error{}
	for _, entry := range it.reader.File {
		err := verifier(entry)
		if err != nil {
			errors = append(errors, err)
		}
	}
	return errors
}

func (it *unzipper) Explode(workers int, directory string) error {
	// This is PoC code, for parallel extraction
	common.Debug("Exploding:")

	todo := make(CommandChannel)
	done := make(CompletedChannel)

	for step := 0; step < workers; step++ {
		go loopExecutor(todo, done)
	}

	for _, entry := range it.reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		todo <- &WriteTarget{
			Source: entry,
			Target: filepath.Join(directory, slashed(entry.Name)),
		}
	}

	close(todo)

	for step := 0; step < workers; step++ {
		<-done
	}

	common.Debug("Done.")

	return nil
}

func (it *unzipper) Asset(name string) ([]byte, error) {
	stream, err := it.reader.Open(name)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	stat, err := stream.Stat()
	if err != nil {
		return nil, err
	}
	payload := bytes.NewBuffer(nil)
	total, err := io.Copy(payload, stream)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if int64(total) != stat.Size() {
		pretty.Warning("Asset %q read partially!", name)
	}
	return payload.Bytes(), nil
}

func (it *unzipper) ExtraDirectoryPrefixLength() (int, string) {
	prefixes := make([]string, 0, 1)
	for _, entry := range it.reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		basename := filepath.Base(entry.Name)
		if strings.ToLower(basename) != "robot.yaml" {
			continue
		}
		dirname := filepath.Dir(entry.Name)
		if len(dirname) > 0 && dirname != "." {
			prefixes = append(prefixes, dirname)
		}
	}
	prefixes = set.Set(prefixes)
	if len(prefixes) != 1 {
		return 0, ""
	}
	prefix := prefixes[0]
	if len(prefix) == 0 {
		return 0, ""
	}
	for _, entry := range it.reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		if !strings.HasPrefix(entry.Name, prefix) {
			return 0, ""
		}
	}
	return len(prefix), prefix
}

func (it *unzipper) Extract(directory string) error {
	common.Trace("Extracting:")
	limit, prefix := 0, ""
	if it.flatten {
		limit, prefix = it.ExtraDirectoryPrefixLength()
	}
	if limit > 0 {
		pretty.Note("Flattening path %q out from extracted files.", prefix)
	}
	for _, entry := range it.reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		target := filepath.Join(directory, slashed(entry.Name)[limit:])
		todo := WriteTarget{
			Source: entry,
			Target: target,
		}
		err := todo.execute()
		if err != nil {
			return fmt.Errorf("Problem while extracting zip, reason: %v", err)
		}
	}
	common.Trace("Done.")
	return nil
}

type zipper struct {
	handle   *os.File
	writer   *zip.Writer
	failures []error
}

func newZipper(filename string) (*zipper, error) {
	handle, err := pathlib.Create(filename)
	if err != nil {
		return nil, err
	}
	writer := zip.NewWriter(handle)
	return &zipper{
		handle:   handle,
		writer:   writer,
		failures: make([]error, 2),
	}, nil
}

func (it *zipper) Note(err error) {
	it.failures = append(it.failures, err)
	common.Debug("Warning! %v", err)
}

func ZipAppend(writer *zip.Writer, fullpath, relativepath string) error {
	source, err := os.Open(fullpath)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := writer.Create(slashed(relativepath))
	if err != nil {
		return err
	}
	_, err = io.Copy(target, source)
	return err
}

func (it *zipper) Add(fullpath, relativepath string, details os.FileInfo) {
	if details != nil {
		common.Debug("- %v size %v", relativepath, details.Size())
	} else {
		common.Debug("- %v", relativepath)
	}
	err := ZipAppend(it.writer, fullpath, relativepath)
	if err != nil {
		it.Note(err)
	}
}

func (it *zipper) AddBlob(relativepath string, blob []byte) {
	target, err := it.writer.Create(slashed(relativepath))
	if err != nil {
		it.Note(err)
		return
	}
	_, err = target.Write(blob)
	if err != nil {
		it.Note(err)
	}
}

func (it *zipper) Close() {
	err := it.writer.Close()
	if err != nil {
		common.Log("Problem closing zip writer: %v", err)
	}
	err = it.handle.Close()
	if err != nil {
		common.Log("Problem closing zipfile: %v", err)
	}
}

func defaultIgnores(selfie string) pathlib.Ignore {
	result := make([]pathlib.Ignore, 0, 10)
	result = append(result, pathlib.IgnorePattern(selfie))
	result = append(result, pathlib.IgnorePattern(".git"))
	result = append(result, pathlib.IgnorePattern(".rpa"))
	result = append(result, pathlib.IgnorePattern("rcc"))
	result = append(result, pathlib.IgnorePattern("output/"))
	result = append(result, pathlib.IgnorePattern("temp/"))
	result = append(result, pathlib.IgnorePattern("tmp/"))
	result = append(result, pathlib.IgnorePattern("__pycache__"))
	result = append(result, pathlib.IgnorePattern("__MACOSX"))
	return pathlib.CompositeIgnore(result...)
}

func CarrierUnzip(directory, carrier string, force, temporary bool) error {
	fullpath, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	if force {
		err = pathlib.EnsureDirectoryExists(fullpath)
	} else {
		err = pathlib.EnsureEmptyDirectory(fullpath)
	}
	if err != nil {
		return err
	}
	unzip, err := newPayloadUnzipper(carrier)
	if err != nil {
		return err
	}
	defer unzip.Close()
	err = unzip.Extract(fullpath)
	if err != nil {
		return err
	}
	if temporary {
		return nil
	}
	return FixDirectory(fullpath)
}

func VerifyZip(zipfile string, verifier Verifier) []error {
	common.TimelineBegin("zip verify %q [size: %s]", zipfile, pathlib.HumaneSize(zipfile))
	defer common.TimelineEnd()

	unzip, err := newUnzipper(zipfile, false)
	if err != nil {
		return []error{err}
	}
	defer unzip.Close()

	return unzip.VerifyShape(verifier)
}

func Unzip(directory, zipfile string, force, temporary, flatten bool) error {
	common.TimelineBegin("unzip %q [size: %s] to %q", zipfile, pathlib.HumaneSize(zipfile), directory)
	defer common.TimelineEnd()

	fullpath, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	if force {
		err = pathlib.EnsureDirectoryExists(fullpath)
	} else {
		err = pathlib.EnsureEmptyDirectory(fullpath)
	}
	if err != nil {
		return err
	}
	unzip, err := newUnzipper(zipfile, flatten)
	if err != nil {
		return err
	}
	defer unzip.Close()
	err = unzip.Extract(fullpath)
	if err != nil {
		return err
	}
	if temporary {
		return nil
	}
	return FixDirectory(fullpath)
}

func Zip(directory, zipfile string, ignores []string) error {
	common.Timeline("zip %q to %q", directory, zipfile)
	defer common.Timeline("zip done")
	common.Debug("Wrapping %v into %v ...", directory, zipfile)
	config, err := robot.LoadRobotYaml(robot.DetectConfigurationName(directory), false)
	if err != nil {
		return err
	}
	ignores = append(ignores, config.IgnoreFiles()...)
	zipper, err := newZipper(zipfile)
	if err != nil {
		return err
	}
	defer zipper.Close()
	ignored, err := pathlib.LoadIgnoreFiles(ignores)
	if err != nil {
		return err
	}
	defaults := defaultIgnores(zipfile)
	pathlib.ForceWalk(directory, pathlib.ForceFilename("hololib.zip"), pathlib.CompositeIgnore(defaults, ignored), zipper.Add)
	return nil
}
