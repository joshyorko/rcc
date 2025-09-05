package operations

import (
	"testing"

	"github.com/robocorp/rcc/hamlet"
)

const (
	wintestpath = `a\b`
	nixtestpath = `a/b`
)

func TestCanConvertSlashes(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	wont.Equal(wintestpath, nixtestpath)
	must.Equal(3, len(wintestpath))
	must.Equal(slashed(wintestpath), nixtestpath)
}
