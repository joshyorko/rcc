//go:build !linux

package environmentlifecycle

import (
	"fmt"

	"github.com/joshyorko/rcc/environmentartifact"
)

func installLegacyImmutable(string, []string, environmentartifact.Descriptor, []byte) error {
	return fmt.Errorf("environment artifact v1 legacy installation requires Linux no-follow primitives")
}

func writeAtomicMutable(string, []string, []byte) error {
	return fmt.Errorf("environment artifact v1 records require Linux no-follow primitives")
}

func readRegularNoFollow(string, []string, int64) ([]byte, error) {
	return nil, fmt.Errorf("environment artifact v1 records require Linux no-follow primitives")
}

func removeRegularNoFollow(string, []string) error {
	return fmt.Errorf("environment artifact v1 records require Linux no-follow primitives")
}

func executableNoFollow(string, []string) (string, error) {
	return "", fmt.Errorf("environment artifact v1 execution requires Linux no-follow primitives")
}
