package operations

import (
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/hamlet"
)

func TestSafeTemplateTargetAcceptsNormalPath(t *testing.T) {
	must, _ := hamlet.Specifications(t)

	base := filepath.Join("tmp", "robot")
	target, err := safeTemplateTarget(base, "folder/file.txt")

	must.Nil(err)
	must.Equal(filepath.Join(base, "folder", "file.txt"), target)
}

func TestSafeTemplateTargetRejectsTraversalPath(t *testing.T) {
	_, wont := hamlet.Specifications(t)

	base := filepath.Join("tmp", "robot")
	_, err := safeTemplateTarget(base, "../outside.txt")

	wont.Nil(err)
}
