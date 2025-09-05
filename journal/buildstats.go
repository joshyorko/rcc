package journal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
)

type (
	acceptor func(*BuildEvent) bool
	picker   func(*BuildEvent) float64
	flagger  func(*BuildEvent) bool
	prettify func(float64) string

	Numbers     []float64
	BuildEvents []*BuildEvent
	BuildEvent  struct {
		Version       string `json:"version"`
		When          int64  `json:"when"`
		What          string `json:"what"`
		Force         bool   `json:"force"`
		Build         bool   `json:"build"`
		Success       bool   `json:"success"`
		Retry         bool   `json:"retry"`
		Run           bool   `json:"run"`
		Controller    string `json:"controller"`
		Space         string `json:"space"`
		BlueprintHash string `json:"blueprint"`

		Started         float64 `json:"started"`
		Prepared        float64 `json:"prepared"`
		MicromambaDone  float64 `json:"micromamba"`
		PipDone         float64 `json:"pip"`
		PostInstallDone float64 `json:"postinstall"`
		RecordDone      float64 `json:"record"`
		RestoreDone     float64 `json:"restore"`
		PreRunDone      float64 `json:"prerun"`
		RobotStart      float64 `json:"robotstart"`
		RobotEnd        float64 `json:"robotend"`
		Finished        float64 `json:"finished"`
		Dirtyness       float64 `json:"dirtyness"`
	}
)

const (
	assistantKey = "assistant"
	prepareKey   = "prepare"
	robotKey     = "robot"
	variableKey  = "variables"
)

var (
	buildevent *BuildEvent
)

func init() {
	buildevent = NewBuildEvent()
}

func CurrentBuildEvent() *BuildEvent {
	return buildevent
}

func BuildEventStats(label string) {
	err := serialize(buildevent.finished(label))
	if err != nil {
		pretty.Warning("build stats for %q failed, reason: %v", label, err)
	}
}

func asPercent(value float64) string {
	return fmt.Sprintf("%5.1f%%", value)
}

func asSecond(value float64) string {
	return fmt.Sprintf("%7.3fs", value)
}

func asCount(value int) string {
	return fmt.Sprintf("%d", value)
}

func forced(the *BuildEvent) bool {
	return the.Force
}

func retried(the *BuildEvent) bool {
	return the.Retry
}

func failed(the *BuildEvent) bool {
	return the.Build && !the.Success
}

func build(the *BuildEvent) bool {
	return the.Build
}

func started(the *BuildEvent) float64 {
	return the.Started
}

func prepared(the *BuildEvent) float64 {
	return the.Prepared
}

func micromamba(the *BuildEvent) float64 {
	return the.MicromambaDone
}

func pip(the *BuildEvent) float64 {
	return the.PipDone
}

func postinstall(the *BuildEvent) float64 {
	return the.PostInstallDone
}

func record(the *BuildEvent) float64 {
	return the.RecordDone
}

func restore(the *BuildEvent) float64 {
	return the.RestoreDone
}

func prerun(the *BuildEvent) float64 {
	return the.PreRunDone
}

func robotstarts(the *BuildEvent) float64 {
	return the.RobotStart
}

func robotends(the *BuildEvent) float64 {
	return the.RobotEnd
}

func finished(the *BuildEvent) float64 {
	return the.Finished
}

func anyOf(flags ...bool) bool {
	for _, flag := range flags {
		if flag {
			return true
		}
	}
	return false
}

func ShowStatistics(weeks uint, assistants, robots, prepares, variables bool) {
	_, body := MakeStatistics(weeks, assistants, robots, prepares, variables)
	os.Stderr.Write(body)
	os.Stderr.Sync()
}

func MakeStatistics(weeks uint, assistants, robots, prepares, variables bool) (int, []byte) {
	sink := bytes.NewBuffer(nil)
	stats, err := Stats(weeks)
	selected := []string{"all"}
	if err != nil {
		pretty.Warning("Loading statistics failed, reason: %v", err)
		return 0, sink.Bytes()
	}
	if anyOf(assistants, robots, prepares, variables) {
		selectors := make(map[string]bool)
		selectors[assistantKey] = assistants
		selectors[robotKey] = robots
		selectors[prepareKey] = prepares
		selectors[variableKey] = variables
		stats = stats.filter(func(the *BuildEvent) bool {
			return selectors[the.What]
		})
		selected = []string{}
		for key, value := range selectors {
			if value {
				selected = append(selected, key)
			}
		}
		sort.Strings(selected)
	}
	tabbed := tabwriter.NewWriter(sink, 2, 4, 2, ' ', tabwriter.AlignRight)
	tabbed.Write(sprint("Selected (%s) statistics: %d samples [%d full weeks]\t\n", strings.Join(selected, ", "), len(stats), weeks))
	tabbed.Write([]byte("\n"))
	tabbed.Write([]byte("Name \tAverage\t10%\tMedian\t90%\tMAX\t\n"))
	stats.Statsline(tabbed, "Dirty", asPercent, func(the *BuildEvent) float64 {
		return the.Dirtyness
	})
	tabbed.Write([]byte("\n"))
	tabbed.Write([]byte("Name                  \tAverage\t10%\tMedian\t90%\tMAX\t\n"))
	stats.Statsline(tabbed, "Lead time             ", asSecond, func(the *BuildEvent) float64 {
		return the.Started
	})
	stats.Statsline(tabbed, "Setup time            ", asSecond, func(the *BuildEvent) float64 {
		if the.RobotStart > 0 {
			return the.RobotStart - the.Started
		}
		return the.Finished - the.Started
	})
	stats.Statsline(tabbed, "Holospace restore time", asSecond, func(the *BuildEvent) float64 {
		if the.RestoreDone > 0 {
			return the.RestoreDone - the.Started
		}
		return the.Finished - the.Started
	})
	stats.Statsline(tabbed, "Pre-run               ", asSecond, func(the *BuildEvent) float64 {
		if the.PreRunDone > 0 {
			return the.PreRunDone - the.RestoreDone
		}
		return 0
	})
	stats.Statsline(tabbed, "Robot startup delay   ", asSecond, func(the *BuildEvent) float64 {
		return the.RobotStart
	})
	stats.Statsline(tabbed, "Robot execution time  ", asSecond, func(the *BuildEvent) float64 {
		return the.RobotEnd - the.RobotStart
	})
	stats.Statsline(tabbed, "Total execution time  ", asSecond, func(the *BuildEvent) float64 {
		return the.Finished
	})
	onlyBuilds := stats.filter(func(the *BuildEvent) bool {
		return the.Build
	})
	tabbed.Write([]byte("\n\n"))
	statCount := len(stats)
	percentage := 100.0 * float64(len(onlyBuilds)) / float64(statCount)
	tabbed.Write(sprint("%d\tsamples with environment builds\t(%3.1f%% from selected)\t\n", len(onlyBuilds), percentage))
	tabbed.Write([]byte("\n"))
	tabbed.Write([]byte("Name                  \tAverage\t10%\tMedian\t90%\tMAX\t\n"))
	onlyBuilds.Statsline(tabbed, "Phase: prepare        ", asSecond, func(the *BuildEvent) float64 {
		if the.Prepared > 0 {
			return the.Prepared - the.Started
		}
		return 0.0
	})
	onlyBuilds.Statsline(tabbed, "Phase: micromamba     ", asSecond, func(the *BuildEvent) float64 {
		if the.MicromambaDone > 0 {
			return the.MicromambaDone - the.Prepared
		}
		return 0.0
	})
	onlyBuilds.Statsline(tabbed, "Phase: pip            ", asSecond, func(the *BuildEvent) float64 {
		if the.PipDone > 0 {
			return the.PipDone - the.MicromambaDone
		}
		return 0.0
	})
	onlyBuilds.Statsline(tabbed, "Phase: post install   ", asSecond, func(the *BuildEvent) float64 {
		if the.PostInstallDone > 0 {
			return the.PostInstallDone - the.first(pip, micromamba)
		}
		return 0.0
	})
	onlyBuilds.Statsline(tabbed, "Phase: record         ", asSecond, func(the *BuildEvent) float64 {
		if the.RecordDone > 0 {
			return the.RecordDone - the.first(postinstall, pip, micromamba)
		}
		return 0.0
	})
	onlyBuilds.Statsline(tabbed, "To hololib            ", asSecond, func(the *BuildEvent) float64 {
		if the.RecordDone > 0 {
			return the.RecordDone - the.Started
		}
		return 0.0
	})

	assistantStats := selectStats(stats, assistantKey)
	prepareStats := selectStats(stats, prepareKey)
	robotStats := selectStats(stats, robotKey)
	variableStats := selectStats(stats, variableKey)

	tabbed.Write([]byte("\n\n"))
	tabbed.Write([]byte("Cumulative    \tAssistants\tPrepares\tRobots\tVariables\tTotal\t\n"))
	tabbed.Write(tabs("Run counts    ", theSize(assistantStats), theSize(prepareStats), theSize(robotStats), theSize(variableStats), theSize(stats)))
	tabbed.Write(tabs("Build counts  ",
		theCounts(assistantStats, build),
		theCounts(prepareStats, build),
		theCounts(robotStats, build),
		theCounts(variableStats, build),
		theCounts(stats, build)))
	tabbed.Write(tabs("Forced builds ",
		theCounts(assistantStats, forced),
		theCounts(prepareStats, forced),
		theCounts(robotStats, forced),
		theCounts(variableStats, forced),
		theCounts(stats, forced)))
	tabbed.Write(tabs("Retried builds",
		theCounts(assistantStats, retried),
		theCounts(prepareStats, retried),
		theCounts(robotStats, retried),
		theCounts(variableStats, retried),
		theCounts(stats, retried)))
	tabbed.Write(tabs("Failed builds ",
		theCounts(assistantStats, failed),
		theCounts(prepareStats, failed),
		theCounts(robotStats, failed),
		theCounts(variableStats, failed),
		theCounts(stats, failed)))
	tabbed.Write(tabs("Setup times   ",
		theTimes(assistantStats, priority(robotstarts, finished), priority(started)),
		theTimes(prepareStats, priority(robotstarts, finished), priority(started)),
		theTimes(robotStats, priority(robotstarts, finished), priority(started)),
		theTimes(variableStats, priority(robotstarts, finished), priority(started)),
		theTimes(stats, priority(robotstarts, finished), priority(started))))
	tabbed.Write(tabs("Run times     ",
		theTimes(assistantStats, priority(robotends), priority(robotstarts)),
		theTimes(prepareStats, priority(robotends), priority(robotstarts)),
		theTimes(robotStats, priority(robotends), priority(robotstarts)),
		theTimes(variableStats, priority(robotends), priority(robotstarts)),
		theTimes(stats, priority(robotends), priority(robotstarts))))
	tabbed.Write(tabs("Total times   ",
		theTimes(assistantStats, priority(finished), priority(started)),
		theTimes(prepareStats, priority(finished), priority(started)),
		theTimes(robotStats, priority(finished), priority(started)),
		theTimes(variableStats, priority(finished), priority(started)),
		theTimes(stats, priority(finished), priority(started))))
	tabbed.Flush()
	return statCount, sink.Bytes()
}

func theCounts(source BuildEvents, check flagger) string {
	total := 0
	for _, event := range source {
		if check(event) {
			total++
		}
	}
	return asCount(total)
}

func priority(pickers ...picker) []picker {
	return pickers
}

func theTimes(source BuildEvents, till []picker, from []picker) string {
	total := 0.0
	for _, event := range source {
		done := event.first(till...)
		if done > 0.0 {
			area := done - event.first(from...)
			total += area
		}
	}
	return asSecond(total)
}

func theSize(source BuildEvents) string {
	return fmt.Sprintf("%d", len(source))
}

func selectStats(source BuildEvents, key string) BuildEvents {
	result := source.filter(func(the *BuildEvent) bool {
		return the.What == key
	})
	return result
}

func tabs(columns ...any) []byte {
	form := strings.Repeat("%s\t", len(columns)) + "\n"
	return []byte(fmt.Sprintf(form, columns...))
}

func BuildEventFilenameFor(stamp time.Time) string {
	year, week := stamp.ISOWeek()
	filename := fmt.Sprintf("stats_%s_%04d_%02d.log", common.UserHomeIdentity(), year, week)
	return filepath.Join(common.JournalLocation(), filename)
}

func CurrentEventFilename() string {
	return BuildEventFilenameFor(common.Clock.Time())
}

func BuildEventFilenamesFor(weekcount int) []string {
	weekstep := -7 * 24 * time.Hour
	timestamp := common.Clock.Time()
	result := make([]string, 0, weekcount+1)
	for weekcount >= 0 {
		result = append(result, BuildEventFilenameFor(timestamp))
		timestamp = timestamp.Add(weekstep)
		weekcount--
	}
	return result
}

func serialize(event *BuildEvent) (err error) {
	defer fail.Around(&err)

	blob, err := json.Marshal(event)
	fail.On(err != nil, "Could not serialize event: %v -> %v", event.What, err)
	return AppendJournal(CurrentEventFilename(), blob)
}

func NewBuildEvent() *BuildEvent {
	return &BuildEvent{
		When:    common.Clock.When(),
		Version: common.Version,
	}
}

func (it *BuildEvent) stowatch() float64 {
	return common.Clock.Elapsed().Seconds()
}

func (it *BuildEvent) finished(label string) *BuildEvent {
	it.What = label
	it.Finished = it.stowatch()
	it.Controller = common.ControllerType
	it.Space = common.HolotreeSpace
	return it
}

func (it *BuildEvent) Successful() {
	it.Success = true
}

func (it *BuildEvent) StartNow(force bool) {
	it.Started = it.stowatch()
	it.Force = force
}

func (it *BuildEvent) Blueprint(blueprint string) {
	it.BlueprintHash = blueprint
}

func (it *BuildEvent) Rebuild() {
	it.Retry = true
	it.Build = true
}

func (it *BuildEvent) PrepareComplete() {
	it.Build = true
	it.Prepared = it.stowatch()
}

func (it *BuildEvent) MicromambaComplete() {
	it.Build = true
	it.MicromambaDone = it.stowatch()
}

func (it *BuildEvent) PipComplete() {
	it.Build = true
	it.PipDone = it.stowatch()
}

func (it *BuildEvent) PostInstallComplete() {
	it.Build = true
	it.PostInstallDone = it.stowatch()
}

func (it *BuildEvent) RecordComplete() {
	it.RecordDone = it.stowatch()
}

func (it *BuildEvent) Dirty(dirtyness float64) {
	it.Dirtyness = dirtyness
}

func (it *BuildEvent) RestoreComplete() {
	it.RestoreDone = it.stowatch()
}

func (it *BuildEvent) PreRunComplete() {
	it.Run = true
	it.PreRunDone = it.stowatch()
}

func (it *BuildEvent) RobotStarts() {
	it.Run = true
	it.RobotStart = it.stowatch()
}

func (it *BuildEvent) RobotEnds() {
	it.Run = true
	it.RobotEnd = it.stowatch()
	reportRatio("Build/Pre-run", it.first(prerun, restore)-it.Started, it.first(prerun, restore)-it.RestoreDone)
	reportRatio("Setup/Run", it.first(prerun, restore)-it.Started, it.RobotEnd-it.first(prerun, restore))
}

func reportRatio(label string, first, second float64) {
	left := int64(math.Ceil(10 * first))
	right := int64(math.Ceil(10 * second))
	gcd := common.Gcd(left, right)
	pretty.Lowlight("  |  %q  relative time allocation ratio:  %d:%d", label, left/gcd, right/gcd)
}

func (it *BuildEvent) first(tools ...picker) float64 {
	for _, tool := range tools {
		value := tool(it)
		if value > 0 {
			return value
		}
	}
	return 0
}

func (it BuildEvents) filter(query acceptor) BuildEvents {
	result := make(BuildEvents, 0, len(it))
	for _, event := range it {
		if query(event) {
			result = append(result, event)
		}
	}
	return result
}

func (it BuildEvents) pick(tool picker) Numbers {
	result := make(Numbers, 0, len(it))
	for _, event := range it {
		result = append(result, tool(event))
	}
	return result
}

func sprint(form string, fields ...any) []byte {
	return []byte(fmt.Sprintf(form, fields...))
}

func (it BuildEvents) Statsline(tabbed *tabwriter.Writer, label string, nice prettify, tool picker) {
	numbers := it.pick(tool)
	sort.Float64s(numbers)
	average, low, median, high, last := numbers.Statsline()
	tabbed.Write(sprint("%s\t%s\t%s\t%s\t%s\t%s\t\n", label, nice(average), nice(low), nice(median), nice(high), nice(last)))
}

func (it Numbers) safe(at int) float64 {
	total := len(it)
	if total == 0 {
		return 0.0
	}
	if at < 0 {
		return it[0]
	}
	if at < total {
		return it[at]
	}
	return it[total-1]
}

func (it Numbers) Statsline() (average, low, median, high, worst float64) {
	total := len(it)
	if total < 1 {
		return
	}
	sum := 0.0
	for _, value := range it {
		sum += value
	}
	half := total >> 1
	percentile := total / 10
	last := total - 1
	right := last - percentile
	return sum / float64(total), it.safe(percentile), it.safe(half), it.safe(right), it.safe(last)
}

func Stats(weeks uint) (result BuildEvents, err error) {
	defer fail.Around(&err)
	result = make(BuildEvents, 0, 100)
	for _, journalname := range BuildEventFilenamesFor(int(weeks)) {
		if !pathlib.IsFile(journalname) {
			continue
		}
		handle, err := os.Open(journalname)
		fail.On(err != nil, "Failed to open event journal %v -> %v", journalname, err)
		defer handle.Close()
		source := bufio.NewReader(handle)
		fail.On(err != nil, "Failed to read %s.", journalname)
	innerloop:
		for {
			line, err := source.ReadBytes('\n')
			if err == io.EOF {
				break innerloop
			}
			fail.On(err != nil, "Failed to read %s.", journalname)
			event := &BuildEvent{}
			err = json.Unmarshal(line, event)
			if err != nil {
				continue innerloop
			}
			result = append(result, event)
		}
	}
	return result, nil
}
