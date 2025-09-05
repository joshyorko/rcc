package operations

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/robocorp/rcc/cloud"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/conda"
	"github.com/robocorp/rcc/fail"
	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/settings"
)

type (
	rccVersions struct {
		Tested []*versionInfo `json:"tested"`
	}
	versionInfo struct {
		Version string `json:"version"`
		When    string `json:"when"`
	}
)

func rccReleaseInfoURL() string {
	// https://downloads.robocorp.com/rcc/releases/index.json
	return settings.Global.DownloadsLink("/rcc/releases/index.json")
}

func rccVersionsJsonPart() string {
	return filepath.Join(common.TemplateLocation(), "rcc.json.part")
}

func rccVersionsJson() string {
	return filepath.Join(common.TemplateLocation(), "rcc.json")
}

func updateRccVersionInfo() (err error) {
	defer fail.Around(&err)

	if !needNewRccInfo() {
		return nil
	}
	return downloadVersionsJson()
}

func needNewRccInfo() bool {
	stat, err := os.Stat(rccVersionsJson())
	return err != nil || common.DayCountSince(stat.ModTime()) > 2
}

func downloadVersionsJson() (err error) {
	defer fail.Around(&err)

	sourceURL := rccReleaseInfoURL()
	partfile := rccVersionsJsonPart()
	err = cloud.Download(sourceURL, partfile)
	fail.On(err != nil, "Failure loading %q, reason: %s", sourceURL, err)
	finaltarget := rccVersionsJson()
	os.Remove(finaltarget)
	return os.Rename(partfile, finaltarget)
}

func loadVersionsInfo() (versions *rccVersions, err error) {
	defer fail.Around(&err)

	blob, err := os.ReadFile(rccVersionsJson())
	fail.Fast(err)
	versions = &rccVersions{}
	err = json.Unmarshal(blob, versions)
	fail.Fast(err)
	return versions, nil
}

func pickLatestTestedVersion(versions *rccVersions) (uint64, string, string) {
	highest, text, when := uint64(0), "unknown", "unkown"
	for _, version := range versions.Tested {
		number, _ := conda.AsVersion(version.Version)
		if number > highest {
			text = version.Version
			when = version.When
			highest = number
		}
	}
	return highest, text, when
}

func RccVersionCheck() func() {
	if common.IsBundled() {
		common.Debug("Did not check newer version existence, since this is bundled case.")
		return nil
	}
	updateRccVersionInfo()
	versions, err := loadVersionsInfo()
	if err != nil || versions == nil {
		return nil
	}
	tested, textual, when := pickLatestTestedVersion(versions)
	current, _ := conda.AsVersion(common.Version)
	if tested == 0 || current == 0 || current >= tested {
		return nil
	}
	return func() {
		pretty.Note("Now running rcc %s. There is newer rcc version %s available, released at %s.", common.Version, textual, when)
	}
}
