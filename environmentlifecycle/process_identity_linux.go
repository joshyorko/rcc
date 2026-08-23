//go:build linux

package environmentlifecycle

import (
	"fmt"
	"os"
	"strings"
)

func lookupProcessIdentity(pid int) (string, error) {
	content, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	closing := strings.LastIndexByte(string(content), ')')
	if closing < 0 {
		return "", fmt.Errorf("process start identity is unavailable")
	}
	fields := strings.Fields(string(content[closing+1:]))
	if len(fields) <= 19 || fields[19] == "" {
		return "", fmt.Errorf("process start identity is unavailable")
	}
	return fields[19], nil
}
