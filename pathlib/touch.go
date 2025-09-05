package pathlib

import (
	"os"
	"time"

	"github.com/robocorp/rcc/common"
)

func TouchWhen(location string, when time.Time) {
	err := os.Chtimes(location, when, when)
	if err != nil {
		common.Debug("Touching file %q failed, reason: %v ... ignored!", location, err)
	}
}

func ForceTouchWhen(location string, when time.Time) {
	if !Exists(location) {
		err := WriteFile(location, []byte{}, 0o644)
		if err != nil {
			common.Debug("Touch/creating file %q failed, reason: %v ... ignored!", location, err)
		}
	}
	TouchWhen(location, when)
}
