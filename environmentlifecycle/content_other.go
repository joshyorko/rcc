//go:build !linux

package environmentlifecycle

import (
	"fmt"

	"github.com/joshyorko/rcc/environmentartifact"
)

func installLegacyImmutable(string, []string, environmentartifact.Descriptor, []byte) error {
	return fmt.Errorf("environment artifact v1 legacy installation requires Linux no-follow primitives")
}
