//go:build !darwin && !linux && !windows

package environmentlifecycle

import "fmt"

func removeNonRegularLocalContentEntry(string, []string) error {
	return fmt.Errorf("non-regular local content removal is unsupported on this platform")
}
