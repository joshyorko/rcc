package operations

import (
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/hamlet"
)

func TestZipEntryTargetAcceptsTemplatePath(t *testing.T) {
	must, _ := hamlet.Specifications(t)

	base := filepath.Join("tmp", "robot")
	target, err := zipEntryTarget(base, "folder/file.txt")

	must.Nil(err)
	absolute, err := filepath.Abs(filepath.Join(base, "folder", "file.txt"))
	must.Nil(err)
	must.Equal(filepath.Clean(absolute), target)
}

func TestZipEntryTargetRejectsTemplateTraversalPath(t *testing.T) {
	_, wont := hamlet.Specifications(t)

	base := filepath.Join("tmp", "robot")
	_, err := zipEntryTarget(base, "../outside.txt")

	wont.Nil(err)
}
