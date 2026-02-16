package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultLegacyLocation = "$HOME/.robocorp"
	defaultRccLocation    = "$HOME/.rcc"
	defaultHoloLocation   = "/Users/Shared/rcc/ht"
)

func ExpandPath(entry string) string {
	intermediate := os.ExpandEnv(entry)
	result, err := filepath.Abs(intermediate)
	if err != nil {
		return intermediate
	}
	return result
}

func GenerateKillCommand(keys []int) string {
	command := []string{"kill -9"}
	for _, key := range keys {
		command = append(command, fmt.Sprintf("%d", key))
	}
	return strings.Join(command, " ")
}

func PlatformSyncDelay() {
	time.Sleep(3 * time.Millisecond)
}
