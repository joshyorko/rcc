package pathlib_test

import (
	"os"
	"testing"

	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/pathlib"
)

type FileInfoMock struct {
	os.FileInfo
	name  string
	isdir bool
}

func (it FileInfoMock) Name() string {
	return it.name
}

func (it FileInfoMock) IsDir() bool {
	return it.isdir
}

func TestCanJustWalkCurrentDirectory(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	err := pathlib.Walk(".", pathlib.IgnoreNothing, pathlib.NoReporting)
	must_be.Nil(err)
}

func TestCanUseCompositeIgnores(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	composite := pathlib.CompositeIgnore(pathlib.IgnoreNothing, pathlib.IgnoreNothing)
	wont_be.Nil(composite)

	err := pathlib.Walk(".", composite, pathlib.NoReporting)
	must_be.Nil(err)
}

func TestCanCreateIgnorePattern(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut := pathlib.IgnorePattern("*.json")
	wont_be.Nil(sut)

	must_be.True(sut(FileInfoMock{name: "hello.json"}))
	wont_be.True(sut(FileInfoMock{name: "hello.yaml"}))
}

func TestIgnorePatternCanBeFullFilename(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut := pathlib.IgnorePattern(".git")
	wont_be.Nil(sut)

	must_be.True(sut(FileInfoMock{name: ".git"}))
	wont_be.True(sut(FileInfoMock{name: ".hg"}))
}

func TestIgnorePatternCanBeFolderForm(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut := pathlib.IgnorePattern(".git/")
	wont_be.Nil(sut)

	must_be.True(sut(FileInfoMock{name: ".git", isdir: true}))
	wont_be.True(sut(FileInfoMock{name: ".hg"}))
}

func TestUseCompositeIgnorePattern(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut := pathlib.CompositeIgnore(pathlib.IgnorePattern(".git"), pathlib.IgnorePattern(".hg"))
	wont_be.Nil(sut)

	must_be.True(sut(FileInfoMock{name: ".git"}))
	must_be.True(sut(FileInfoMock{name: ".hg"}))
	wont_be.True(sut(FileInfoMock{name: ".svn"}))
}

func TestCanLoadIgnoreFile(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut, err := pathlib.LoadIgnoreFile("testdata/missing", true)
	must_be.Nil(sut)
	wont_be.Nil(err)
}

func TestCanLoadEmptyIgnoreFile(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut, err := pathlib.LoadIgnoreFile("testdata/empty", true)
	must_be.Nil(sut)
	wont_be.Nil(err)
}

func TestCanLoadCommentOnlyIgnoreFile(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut, err := pathlib.LoadIgnoreFile("testdata/commented_ignores", true)
	must_be.Nil(sut)
	wont_be.Nil(err)
}

func TestCanLoadValidIgnoreFile(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut, err := pathlib.LoadIgnoreFile("testdata/valid_ignores", true)
	wont_be.Nil(sut)
	must_be.Nil(err)
}
