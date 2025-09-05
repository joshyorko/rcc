package pathlib

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/robocorp/rcc/anywork"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
)

const (
	touchDelay    = 7
	deadlineDelay = touchDelay * -5
	partSeparator = `___`
)

var (
	slashPattern      = regexp.MustCompile("[/\\\\]+")
	underscorePattern = regexp.MustCompile("_+")
	spacePattern      = regexp.MustCompile("\\s+")
)

type (
	Lockpid struct {
		ParentID   int
		ProcessID  int
		Controller string
		Space      string
		Username   string
		Basename   string
	}
	Lockpids []*Lockpid
)

func LockHoldersBy(filename string) (result Lockpids, err error) {
	defer fail.Around(&err)

	holders, err := LoadLockpids()
	fail.On(err != nil, "%v", err)
	total := len(holders)
	if total == 0 {
		return holders, nil
	}

	selector := unify(filepath.Base(filename))
	result = Lockpids{}
	for _, candidate := range holders {
		if candidate.Basename == selector {
			result = append(result, candidate)
		}
	}

	return result, nil
}

func LoadLockpids() (result Lockpids, err error) {
	defer fail.Around(&err)

	deadline := time.Now().Add(deadlineDelay * time.Second)
	result = make(Lockpids, 0, 10)
	root := common.HololibPids()
	entries, err := os.ReadDir(root)
	fail.On(err != nil, "Failed to read lock pids directory, reason: %v", err)

browsing:
	for _, entry := range entries {
		fullpath := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil || info == nil {
			continue
		}
		if info.IsDir() {
			anywork.Backlog(func() {
				TryRemoveAll("lockpid/dir", fullpath)
				common.Trace(">> Trying to remove extra dir at lockpids: %q", fullpath)
			})
			continue browsing
		}
		if info.ModTime().Before(deadline) {
			anywork.Backlog(func() {
				TryRemove("lockpid/stale", fullpath)
				common.Trace(">> Trying to remove old file at lockpids: %q", fullpath)
			})
			continue browsing
		}
		lockpid, ok := parseLockpid(entry.Name())
		if !ok {
			anywork.Backlog(func() {
				TryRemove("lockpid/unknown", fullpath)
				common.Trace(">> Trying to remove unknown file at lockpids: %q", fullpath)
			})
			continue browsing
		}
		result = append(result, lockpid)
	}
	return result, nil
}

func parseLockpid(basename string) (*Lockpid, bool) {
	parts := strings.Split(basename, partSeparator)
	if len(parts) != 6 {
		return nil, false
	}
	parentID, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, false
	}
	processID, err := strconv.Atoi(parts[3])
	if err != nil {
		return nil, false
	}
	return &Lockpid{
		ParentID:   parentID,
		ProcessID:  processID,
		Controller: unify(parts[0]),
		Space:      unify(parts[2]),
		Username:   unify(parts[4]),
		Basename:   unify(parts[5]),
	}, true
}

func LockpidFor(filename string) *Lockpid {
	basename := filepath.Base(filename)
	username := "anonymous"
	who, err := user.Current()
	if err == nil {
		username = unslash(who.Username)
	}
	return &Lockpid{
		ParentID:   os.Getppid(),
		ProcessID:  os.Getpid(),
		Controller: unify(common.ControllerType),
		Space:      unify(common.HolotreeSpace),
		Username:   unify(username),
		Basename:   unify(basename),
	}
}

func (it *Lockpid) Message() string {
	return fmt.Sprintf("Possibly pending lock %q, user: %q, space: %q, and controller: %q (parent/pid: %d/%d). May cause environment wait/build delay.", it.Basename, it.Username, it.Space, it.Controller, it.ParentID, it.ProcessID)
}

func (it *Lockpid) Keepalive() chan bool {
	latch := make(chan bool)
	go keepFresh(it, latch)
	runtime.Gosched()
	common.Trace("Trying to keep lockpid %q fresh fron now on.", it.Location())
	return latch
}

func (it *Lockpid) Touch() {
	where := it.Location()
	anywork.Backlog(func() {
		ForceTouchWhen(where, time.Now())
		common.Trace(">> Tried to touch lockpid %q now.", where)
	})
	runtime.Gosched()
}

func (it *Lockpid) Erase() {
	where := it.Location()
	anywork.Backlog(func() {
		TryRemove("lockpid", where)
		common.Trace(">> Tried to erase lockpid %q now.", where)
	})
	runtime.Gosched()
}

func (it *Lockpid) Filename() string {
	return fmt.Sprintf("%s___%d___%s___%d___%s___%s", it.Controller, it.ParentID, it.Space, it.ProcessID, it.Username, it.Basename)
}

func (it *Lockpid) Location() string {
	return filepath.Join(common.HololibPids(), it.Filename())
}

func keepFresh(lockpid *Lockpid, latch chan bool) {
	defer lockpid.Erase()
	delay := touchDelay * time.Second
forever:
	for {
		lockpid.Touch()
		select {
		case <-latch:
			break forever
		case <-time.After(delay):
			continue forever
		}
	}
}

func unspace(text string) string {
	parts := spacePattern.Split(text, -1)
	return strings.Join(parts, "_")
}

func unslash(text string) string {
	parts := slashPattern.Split(text, -1)
	return strings.Join(parts, "_")
}

func oneunderscore(text string) string {
	parts := underscorePattern.Split(text, -1)
	return strings.Join(parts, "_")
}

func unify(text string) string {
	return oneunderscore(unslash(unspace(strings.TrimSpace(text))))
}
