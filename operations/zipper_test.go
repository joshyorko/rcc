package operations

import (
	"testing"

	"github.com/joshyorko/rcc/hamlet"
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

func TestZipEntryTargetAcceptsNestedPath(t *testing.T) {
	must, _ := hamlet.Specifications(t)
	target, err := zipEntryTarget("/tmp/destination", "repo/robot.yaml")
	must.Equal(nil, err)
	must.Equal("/tmp/destination/repo/robot.yaml", target)
}

func TestZipEntryTargetRejectsTraversalPath(t *testing.T) {
	_, wont := hamlet.Specifications(t)
	_, err := zipEntryTarget("/tmp/destination", "../../outside.txt")
	wont.Equal(nil, err)
}
