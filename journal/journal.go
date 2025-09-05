package journal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
)

var (
	spacePattern = regexp.MustCompile("\\s+")
	runJournal   *journal
)

type (
	journal struct {
		sync.Mutex
		filename string
	}
	Event struct {
		When       int64  `json:"when"`
		Controller string `json:"controller"`
		Event      string `json:"event"`
		Detail     string `json:"detail"`
		Comment    string `json:"comment,omitempty"`
	}
)

func init() {
	runJournal = &journal{sync.Mutex{}, ""}
	common.RegisterJournal(runJournal)
}

func ForRun(filename string) {
	runJournal.Lock()
	defer runJournal.Unlock()
	runJournal.filename = filename
}

func StopRunJournal() {
	ForRun("")
}

func (it *journal) Post(event, detail, commentForm string, fields ...interface{}) (err error) {
	if it == nil || len(it.filename) == 0 {
		return nil
	}
	defer fail.Around(&err)
	it.Lock()
	defer it.Unlock()
	message := Event{
		When:       time.Now().Unix(),
		Controller: common.ControllerIdentity(),
		Event:      Unify(event),
		Detail:     detail,
		Comment:    Unify(fmt.Sprintf(commentForm, fields...)),
	}
	blob, err := json.Marshal(message)
	fail.On(err != nil, "Could not serialize event: %v -> %v", event, err)
	return AppendJournal(it.filename, blob)
}

func Unify(value string) string {
	return strings.TrimSpace(spacePattern.ReplaceAllString(value, " "))
}

func Post(event, detail, commentForm string, fields ...interface{}) (err error) {
	defer fail.Around(&err)
	message := Event{
		When:       common.When,
		Controller: common.ControllerIdentity(),
		Event:      Unify(event),
		Detail:     detail,
		Comment:    Unify(fmt.Sprintf(commentForm, fields...)),
	}
	blob, err := json.Marshal(message)
	fail.On(err != nil, "Could not serialize event: %v -> %v", event, err)
	return AppendJournal(common.EventJournal(), blob)
}

func AppendJournal(journalname string, blob []byte) (err error) {
	defer fail.Around(&err)
	if common.WarrantyVoided() {
		return nil
	}
	handle, err := os.OpenFile(journalname, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	fail.On(err != nil, "Failed to open event journal %v -> %v", journalname, err)
	defer handle.Close()
	_, err = handle.Write(append(blob, '\n'))
	fail.On(err != nil, "Failed to write event journal %v -> %v", journalname, err)
	return handle.Sync()
}

func Events() (result []Event, err error) {
	defer fail.Around(&err)
	handle, err := os.Open(common.EventJournal())
	fail.On(err != nil, "Failed to open event journal %v -> %v", common.EventJournal(), err)
	defer handle.Close()
	source := bufio.NewReader(handle)
	fail.On(err != nil, "Failed to read %s.", common.EventJournal())
	result = make([]Event, 0, 100)
	for {
		line, err := source.ReadBytes('\n')
		if err == io.EOF {
			return result, nil
		}
		fail.On(err != nil, "Failed to read %s.", common.EventJournal())
		event := Event{}
		err = json.Unmarshal(line, &event)
		if err != nil {
			continue
		}
		result = append(result, event)
	}
}
