//go:build !linux && !darwin && !windows

package environmentlifecycle

import "fmt"

func lookupProcessIdentity(int) (string, error) {
	return "", fmt.Errorf("strong process start identity is unavailable on this platform")
}
