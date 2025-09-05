package conda

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/robocorp/rcc/common"
)

func MakeRelativeMap(root string, entries map[string]string) map[string]string {
	result := make(map[string]string)
	for key, value := range entries {
		if !strings.HasPrefix(key, root) {
			result[key] = value
			continue
		}
		short, err := filepath.Rel(root, key)
		if err == nil {
			key = short
		}
		result[key] = value
	}
	return result
}

func DirhashDiff(history, future map[string]string, warning bool) {
	removed := []string{}
	added := []string{}
	changed := []string{}
	for key, value := range history {
		next, ok := future[key]
		if !ok {
			removed = append(removed, key)
			continue
		}
		if value != next {
			changed = append(changed, key)
		}
	}
	for key, _ := range future {
		_, ok := history[key]
		if !ok {
			added = append(added, key)
		}
	}
	if len(removed)+len(added)+len(changed) == 0 {
		return
	}
	common.Log("----  rcc env diff  ----")
	sort.Strings(removed)
	sort.Strings(added)
	sort.Strings(changed)
	separate := false
	for _, folder := range removed {
		common.Trace("- diff: removed %q", folder)
		separate = true
	}
	if len(changed) > 0 {
		if separate {
			common.Trace("-------")
			separate = false
		}
		for _, folder := range changed {
			common.Trace("- diff: changed %q", folder)
			separate = true
		}
	}
	if len(added) > 0 {
		if separate {
			common.Trace("-------")
			separate = false
		}
		for _, folder := range added {
			common.Trace("- diff: added %q", folder)
			separate = true
		}
	}
	if warning {
		if separate {
			common.Trace("-------")
			separate = false
		}
		common.Log("Notice: Robot run modified the environment which will slow down the next run.")
		common.Log("        Please inform the robot developer about this. Use --trace for details.")
	}
	common.Log("----  rcc env diff  ----")
}

func DiagnoseDirty(beforeLabel, afterLabel string, beforeHash, afterHash []byte, beforeErr, afterErr error, beforeDetails, afterDetails map[string]string, warning bool) {
	if beforeErr != nil || afterErr != nil {
		common.Debug("live %q diagnosis failed, before: %v, after: %v", afterLabel, beforeErr, afterErr)
		return
	}
	beforeSummary := fmt.Sprintf("%02x", beforeHash)
	afterSummary := fmt.Sprintf("%02x", afterHash)
	if beforeSummary == afterSummary {
		common.Debug("live %q diagnosis: did not change during run [%s]", afterLabel, afterSummary)
		return
	}
	common.Debug("live %q diagnosis: corrupted [%s] => [%s]", afterLabel, beforeSummary, afterSummary)
	beforeDetails = MakeRelativeMap(beforeLabel, beforeDetails)
	afterDetails = MakeRelativeMap(afterLabel, afterDetails)
	DirhashDiff(beforeDetails, afterDetails, warning)
}
