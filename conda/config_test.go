package conda_test

import (
	"os"
	"testing"

	"github.com/robocorp/rcc/conda"
	"github.com/robocorp/rcc/hamlet"
)

func TestReadingMissingFileProducesError(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)
	text, err := conda.ReadConfig("some.yaml")
	wont_be.Nil(err)
	must_be.Text("", text)
	must_be.True(true)
	cwd, err := os.Getwd()
	wont_be.Nil(cwd)
	must_be.Nil(err)
}

func TestReadingCorrectFileProducesText(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)
	text, err := conda.ReadConfig("testdata/conda.yaml")
	must_be.Nil(err)
	wont_be.Text("", text)
	must_be.Equal(169, len(text))
}

func TestUnifyLineWorksCorrectly(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	must_be.Text("foo", conda.UnifyLine("foo"))
	must_be.Text("foo", conda.UnifyLine(" foo"))
	must_be.Text("foo", conda.UnifyLine(" foo "))
	must_be.Text("foo", conda.UnifyLine(" foo \t"))
	must_be.Text("foo", conda.UnifyLine(" \tfoo \r\n \r\n"))
}

func TestSplitsLinesCorrectly(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	must_be.Equal(1, len(conda.SplitLines("")))
	must_be.Equal(2, len(conda.SplitLines("\r\n")))
	must_be.Equal(2, len(conda.SplitLines("\n")))
	must_be.Equal([]string{"", ""}, conda.SplitLines("\r\n"))
	must_be.Equal([]string{"a", "b", "c", "d"}, conda.SplitLines("a\r\nb\r\nc\r\nd"))
}

func TestGetsLinesAsUnifiedCorrectly(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	must_be.Equal(0, len(conda.AsUnifiedLines("")))
	must_be.Equal([]string{"a"}, conda.AsUnifiedLines("a\r\n\r\n"))
	must_be.Equal([]string{"a", "b"}, conda.AsUnifiedLines(" \r\n\tb \r\na\r\n\ta\t\r\n"))
}
