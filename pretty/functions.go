package pretty

import (
	"fmt"
	"strings"
	"time"

	"github.com/robocorp/rcc/common"
)

const (
	rccpov   = `From rcc %q (controller: %q) point of view, %q was`
	maxSteps = 15
)

var (
	ProgressMark     time.Time
	onlyOnceMessages = make(map[string]bool)
)

func init() {
	ProgressMark = time.Now()
}

func Ok() error {
	common.Log("%sOK.%s", Green, Reset)
	return nil
}

func JustOnce(format string, rest ...interface{}) {
	message := fmt.Sprintf(format, rest...)
	if !onlyOnceMessages[message] {
		onlyOnceMessages[message] = true
		Highlight(format, rest...)
	}
}

func DebugNote(format string, rest ...interface{}) {
	niceform := fmt.Sprintf("%s%sNote: %s%s", Blue, Bold, format, Reset)
	common.Debug(niceform, rest...)
}

func Note(format string, rest ...interface{}) {
	niceform := fmt.Sprintf("%s%sNote: %s%s", Cyan, Bold, format, Reset)
	common.Log(niceform, rest...)
}

func Warning(format string, rest ...interface{}) {
	niceform := fmt.Sprintf("%sWarning: %s%s", Yellow, format, Reset)
	common.Log(niceform, rest...)
}

func Highlight(format string, rest ...interface{}) {
	niceform := fmt.Sprintf("%s%s%s", Magenta, format, Reset)
	common.Log(niceform, rest...)
}

func Lowlight(format string, rest ...interface{}) {
	niceform := fmt.Sprintf("%s%s%s", Grey, format, Reset)
	common.Log(niceform, rest...)
}

func Exit(code int, format string, rest ...interface{}) {
	var niceform string
	if code == 0 {
		niceform = fmt.Sprintf("%s%s%s", Green, format, Reset)
	} else {
		niceform = fmt.Sprintf("%s%s%s", Red, format, Reset)
	}
	common.Exit(code, niceform, rest...)
}

// Guard watches, that only truthful shall pass. Otherwise exits with code and details.
func Guard(truth bool, code int, format string, rest ...interface{}) {
	if !truth {
		Exit(code, format, rest...)
	}
}

func RccPointOfView(context string, err error) {
	explain := fmt.Sprintf(rccpov, common.Version, common.ControllerType, context)
	printer := Lowlight
	message := fmt.Sprintf("@@@  %s SUCCESS. @@@", explain)
	journal := fmt.Sprintf("%s SUCCESS.", explain)
	if err != nil {
		printer = Highlight
		message = fmt.Sprintf("@@@  %s FAILURE, reason: %q. See details above.  @@@", explain, err)
		journal = fmt.Sprintf("%s FAILURE, reason: %s", explain, err)
	}
	banner := strings.Repeat("@", len(message))
	printer(banner)
	printer(message)
	printer(banner)
	common.RunJournal("robot exit", journal, "rcc point of view")
}

func Regression(step int, form string, details ...interface{}) {
	progress(Red, step, form, details...)
}

func Progress(step int, form string, details ...interface{}) {
	color := Cyan
	if step == maxSteps {
		color = Green
	}
	progress(color, step, form, details...)
}

func progress(color string, step int, form string, details ...interface{}) {
	previous := ProgressMark
	ProgressMark = time.Now()
	delta := ProgressMark.Sub(previous).Round(1 * time.Millisecond).Seconds()
	message := fmt.Sprintf(form, details...)
	common.Log("%s####  Progress: %02d/%d  %s  %8.3fs  %s%s", color, step, maxSteps, common.Version, delta, message, Reset)
	common.Timeline("%d/%d %s", step, maxSteps, message)
	common.RunJournal("environment", "build", "Progress: %02d/%d  %s  %8.3fs  %s", step, maxSteps, common.Version, delta, message)
}
