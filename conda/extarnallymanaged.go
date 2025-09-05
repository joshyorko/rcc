package conda

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/robocorp/rcc/blobs"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
)

const (
	EXTERNALLY_MANAGED = "EXTERNALLY-MANAGED"
)

type (
	SysconfigPaths struct {
		Stdlib  string `json:"stdlib"`
		Purelib string `json:"purelib"`
		Platlib string `json:"platlib"`
	}
)

func FindSysconfigPaths(path string) (paths *SysconfigPaths, err error) {
	defer fail.Around(&err)

	capture, code, err := LiveCapture(path, "python", "-c", "import json, sysconfig; print(json.dumps(sysconfig.get_paths()))")
	if err != nil {
		common.Fatal(fmt.Sprintf("EXTERNALLY-MANAGED failure [%d/%x]", code, code), err)
		return nil, err
	}
	mappings := &SysconfigPaths{}
	err = json.Unmarshal([]byte(capture), mappings)
	fail.Fast(err)
	return mappings, nil
}

func ApplyExternallyManaged(path string) (label string, err error) {
	defer fail.Around(&err)

	if !common.ExternallyManaged {
		return "", nil
	}
	common.Debug("Applying EXTERNALLY-MANAGED (PEP 668) to environment.")
	paths, err := FindSysconfigPaths(path)
	fail.Fast(err)
	location := filepath.Join(paths.Stdlib, EXTERNALLY_MANAGED)
	blob, err := blobs.Asset("assets/externally_managed.txt")
	fail.Fast(err)
	fail.Fast(os.WriteFile(location, blob, 0o644))
	common.Timeline("applied EXTERNALLY-MANAGED (PEP 668) to this holotree space")
	return fmt.Sprintf("%s (PEP 668) ", EXTERNALLY_MANAGED), nil
}
