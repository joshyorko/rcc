package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/robocorp/rcc/anywork"
	"github.com/robocorp/rcc/cloud"
	"github.com/robocorp/rcc/cmd"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/conda"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/set"
)

const (
	timezonekey = `rcc.cli.tz`
	oskey       = `rcc.cli.os`
	daily       = 60 * 60 * 24
)

var (
	markedAlready = false
)

func EnsureUserRegistered() (string, error) {
	var warning string

	cache, err := operations.SummonCache()
	if err != nil {
		return warning, err
	}
	who, err := user.Current()
	if err != nil {
		return warning, err
	}
	updated, ok := set.Update(cache.Users, strings.ToLower(who.Username))
	size := len(updated)
	if size > 1 {
		warning = fmt.Sprintf("More than one user is using same %s location! Those users are: %s!", common.Product.HomeVariable(), strings.Join(updated, ", "))
	}
	if !ok {
		return warning, nil
	}
	cache.Users = updated
	return warning, cache.Save()
}

func TimezoneMetric() error {
	cache, err := operations.SummonCache()
	if err != nil {
		return err
	}
	deadline, ok := cache.Stamps[timezonekey]
	if ok && deadline > common.When {
		return nil
	}
	cache.Stamps[timezonekey] = common.When + daily
	zone := time.Now().Format("MST-0700")
	cloud.InternalBackgroundMetric(common.ControllerIdentity(), timezonekey, zone)
	cloud.InternalBackgroundMetric(common.ControllerIdentity(), oskey, common.Platform())
	return cache.Save()
}

func ExitProtection() {
	runtime.Gosched()
	status := recover()
	if status != nil {
		markTempForRecycling()
		exit, ok := status.(common.ExitCode)
		if ok {
			exit.ShowMessage()
			pretty.Highlight("[rcc] exit status will be: %d!", exit.Code)
			cloud.WaitTelemetry()
			common.WaitLogs()
			os.Exit(exit.Code)
		}
		cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.panic.origin", cmd.Origin())
		cloud.WaitTelemetry()
		common.WaitLogs()
		panic(status)
	}
	cloud.WaitTelemetry()
	common.WaitLogs()
}

func startTempRecycling() {
	if common.DisableTempManagement() {
		common.Timeline("temp management disabled -- no temp recycling")
		return
	}
	defer common.Timeline("temp recycling done")
	pattern := filepath.Join(common.ProductTempRoot(), "*", "recycle.now")
	found, err := filepath.Glob(pattern)
	if err != nil {
		common.Debug("Recycling failed, reason: %v", err)
		return
	}
	for _, filename := range found {
		folder := filepath.Dir(filename)
		changed, err := pathlib.Modtime(folder)
		if err == nil && time.Since(changed) > 48*time.Hour {
			go os.RemoveAll(folder)
		}
	}
	runtime.Gosched()
}

func markTempForRecycling() {
	if common.DisableTempManagement() {
		common.Timeline("temp management disabled -- temp not marked for recycling")
		return
	}
	if markedAlready {
		return
	}
	target := common.ProductTempName()
	if pathlib.Exists(target) {
		filename := filepath.Join(target, "recycle.now")
		pathlib.WriteFile(filename, []byte("True"), 0o644)
		common.Debug("Marked %q for recycling.", target)
		markedAlready = true
	}
}

func main() {
	defer ExitProtection()

	notify := operations.RccVersionCheck()
	if notify != nil {
		defer notify()
	}

	warning, _ := EnsureUserRegistered()
	if len(warning) > 0 {
		defer pretty.Warning("%s", warning)
	}

	anywork.Backlog(conda.BugsCleanup)

	if common.SharedHolotree {
		common.TimelineBegin("Start [shared mode]. (parent/pid: %d/%d)", os.Getppid(), os.Getpid())
	} else {
		common.TimelineBegin("Start [private mode]. (parent/pid: %d/%d)", os.Getppid(), os.Getpid())
	}
	defer common.EndOfTimeline()
	if common.OneOutOf(6) {
		go startTempRecycling()
	}
	defer markTempForRecycling()
	defer os.Stderr.Sync()
	defer os.Stdout.Sync()
	cmd.Execute()
	common.Timeline("Command execution done.")
	if common.OneOutOf(5) {
		TimezoneMetric()
	}

	if common.WarrantyVoided() {
		common.Timeline("Running in 'warranty voided' mode.")
		pretty.Warning("Note that 'rcc' is running in 'warranty voided' mode.")
	}

	anywork.Sync()
}
