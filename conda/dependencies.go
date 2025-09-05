package conda

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
	"gopkg.in/yaml.v2"
)

type dependency struct {
	Name    string `yaml:"name"    json:"name"`
	Version string `yaml:"version" json:"version"`
	Origin  string `yaml:"origin"  json:"channel"`
}

func (it *dependency) AsKey() string {
	return fmt.Sprintf("%-50s  %-20s", it.Name, it.Origin)
}

type dependencies []*dependency

func (it dependencies) sorted() dependencies {
	sort.SliceStable(it, func(left, right int) bool {
		lefty := strings.ToLower(it[left].Name)
		righty := strings.ToLower(it[right].Name)
		if lefty == righty {
			return it[left].Origin < it[right].Origin
		}
		return lefty < righty
	})
	return it
}

func (it dependencies) WarnVulnerability(url, severity, name string, versions ...string) {
	found, ok := it.Lookup(name, false)
	if !ok {
		found, ok = it.Lookup(name, true)
	}
	if !ok {
		return
	}
	for _, version := range versions {
		if found.Version == version {
			pretty.Highlight("Dependency with %s severity vulnerability detected: %s %s. For more information see %s", severity, name, found.Version, url)
		}
	}
}

func (it dependencies) Lookup(name string, pypi bool) (*dependency, bool) {
	for _, entry := range it {
		if pypi && entry.Origin != "pypi" {
			continue
		}
		if !pypi && entry.Origin == "pypi" {
			continue
		}
		if entry.Name == name {
			return entry, true
		}
	}
	return nil, false
}

func parseDependencies(origin string, output []byte) (dependencies, error) {
	result := make(dependencies, 0, 100)
	err := json.Unmarshal(output, &result)
	if err != nil {
		return nil, err
	}
	if len(origin) == 0 {
		return result, nil
	}
	for _, here := range result {
		if len(here.Origin) == 0 {
			here.Origin = origin
		}
	}
	return result, nil
}

func fillDependencies(context, targetFolder string, seen map[string]string, collector dependencies, command ...string) (_ dependencies, err error) {
	defer fail.Around(&err)

	task, err := livePrepare(targetFolder, command...)
	fail.On(err != nil, "%v", err)
	out, _, err := task.CaptureOutput()
	fail.On(err != nil, "%v", err)
	listing, err := parseDependencies(context, []byte(out))
	fail.On(err != nil, "%v", err)
	for _, entry := range listing {
		found, ok := seen[strings.ToLower(entry.Name)]
		if ok && found == entry.Version {
			continue
		}
		collector = append(collector, entry)
		seen[strings.ToLower(entry.Name)] = entry.Version
	}
	return collector, nil
}

func GoldenMasterFilename(targetFolder string) string {
	return filepath.Join(targetFolder, "golden-ee.yaml")
}

func goldenMaster(targetFolder string, pipUsed bool) (err error) {
	defer fail.Around(&err)

	seen := make(map[string]string)
	collector := make(dependencies, 0, 100)
	collector, err = fillDependencies("mamba", targetFolder, seen, collector, BinMicromamba(), "list", "--json")
	fail.On(err != nil, "Failed to list micromamba dependencies, reason: %v", err)
	if pipUsed {
		collector, err = fillDependencies("pypi", targetFolder, seen, collector, "pip", "list", "--isolated", "--local", "--format", "json")
		fail.On(err != nil, "Failed to list pip dependencies, reason: %v", err)
	}
	body, err := yaml.Marshal(collector.sorted())
	fail.On(err != nil, "Failed to make yaml, reason: %v", err)
	goldenfile := GoldenMasterFilename(targetFolder)
	common.Debug("%sGolden EE file at: %v%s", pretty.Yellow, goldenfile, pretty.Reset)
	return pathlib.WriteFile(goldenfile, body, 0644)
}

func LoadWantedDependencies(filename string) dependencies {
	body, err := os.ReadFile(filename)
	if err != nil {
		return dependencies{}
	}
	result := make(dependencies, 0, 100)
	err = yaml.Unmarshal(body, &result)
	if err != nil {
		return dependencies{}
	}
	return result.sorted()
}

func SideBySideViewOfDependencies(goldenfile, wantedfile string) (err error) {
	defer fail.Around(&err)

	gold := LoadWantedDependencies(goldenfile)
	want := LoadWantedDependencies(wantedfile)

	if len(gold) == 0 && len(want) == 0 {
		return fmt.Errorf("Running against old environment, and no dependencies.yaml.")
	}

	diffmap := make(map[string][2]int)
	injectDiffmap(diffmap, want, 0)
	injectDiffmap(diffmap, gold, 1)
	keyset := make([]string, 0, len(diffmap))
	for key, _ := range diffmap {
		keyset = append(keyset, key)
	}
	sort.Strings(keyset)

	common.WaitLogs()
	hasgold := false
	unknown := fmt.Sprintf("%sUnknown%s", pretty.Grey, pretty.Reset)
	same := fmt.Sprintf("%sSame%s", pretty.Cyan, pretty.Reset)
	drifted := fmt.Sprintf("%sDrifted%s", pretty.Yellow, pretty.Reset)
	missing := fmt.Sprintf("%sN/A%s", pretty.Grey, pretty.Reset)
	tabbed := tabwriter.NewWriter(os.Stderr, 2, 4, 2, ' ', 0)
	tabbed.Write([]byte("Wanted\tVersion\tOrigin\t|\tNo.\t|\tAvailable\tVersion\tOrigin\t|\tStatus\n"))
	tabbed.Write([]byte("------\t-------\t------\t+\t---\t+\t---------\t-------\t------\t+\t------\n"))
	for at, key := range keyset {
		left, right, status := "-\t-\t-\t", "\t-\t-\t-", unknown
		sides := diffmap[key]
		if sides[0] < 0 || sides[1] < 0 {
			status = missing
		} else {
			left, right := want[sides[0]], gold[sides[1]]
			if left.Version == right.Version {
				status = same
			} else {
				status = drifted
			}
		}
		if sides[0] > -1 {
			entry := want[sides[0]]
			left = fmt.Sprintf("%s\t%s\t%s\t", entry.Name, entry.Version, entry.Origin)
		}
		if sides[1] > -1 {
			entry := gold[sides[1]]
			right = fmt.Sprintf("\t%s\t%s\t%s", entry.Name, entry.Version, entry.Origin)
			hasgold = true
		}
		data := fmt.Sprintf("%s|\t%3d\t|%s\t|\t%s\n", left, at+1, right, status)
		tabbed.Write([]byte(data))
	}
	tabbed.Write([]byte("------\t-------\t------\t+\t---\t+\t---------\t-------\t------\t+\t------\n"))
	tabbed.Write([]byte("Wanted\tVersion\tOrigin\t|\tNo.\t|\tAvailable\tVersion\tOrigin\t|\tStatus\n"))
	tabbed.Write([]byte("\n"))
	tabbed.Flush()
	if !hasgold {
		return fmt.Errorf("Running against old environment, which does not have 'golden-ee.yaml' file.")
	}
	return nil
}

func injectDiffmap(diffmap map[string][2]int, deps dependencies, side int) {
	for at, entry := range deps {
		key := entry.AsKey()
		found, ok := diffmap[key]
		if !ok {
			found = [2]int{-1, -1}
		}
		found[side] = at
		diffmap[key] = found
	}
}
