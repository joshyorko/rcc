package common

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	logsource  = make(logwriters)
	logbarrier = sync.WaitGroup{}
)

type logwriter func() (*os.File, string)
type logwriters chan logwriter

func loggerLoop(writers logwriters) {
	var stamp string
	line := uint64(0)
	for {
		line += 1
		todo, ok := <-writers
		if !ok {
			continue
		}
		out, message := todo()

		if TraceFlag() {
			stamp = time.Now().Format("02.150405.000 ")
		} else if LogLinenumbers {
			stamp = fmt.Sprintf("%3d ", line)
		} else {
			stamp = ""
		}
		fmt.Fprintf(out, "%s%s\n", stamp, message)
		out.Sync()
		logbarrier.Done()
	}
}

func init() {
	go loggerLoop(logsource)
}

func AcceptableOutput(message string) bool {
	for _, fragment := range LogHides {
		if strings.Contains(message, fragment) {
			return false
		}
	}
	return true
}

func printout(out *os.File, message string) {
	if AcceptableOutput(message) {
		logbarrier.Add(1)
		logsource <- func() (*os.File, string) {
			return out, message
		}
	}
}

func Fatal(context string, err error) {
	if err != nil {
		printout(os.Stderr, fmt.Sprintf("Fatal [%s]: %v", context, err))
	}
}

func Error(context string, err error) {
	if err != nil {
		Log("Error [%s]: %v", context, err)
	}
}

func Uncritical(context string, err error) {
	if err != nil {
		Log("Warning [%s; not critical]: %v", context, err)
	}
}

func Log(format string, details ...interface{}) {
	if !Silent() {
		prefix := ""
		if DebugFlag() || TraceFlag() {
			prefix = "[N] "
		}
		printout(os.Stderr, fmt.Sprintf(prefix+format, details...))
	}
}

func Debug(format string, details ...interface{}) error {
	if DebugFlag() {
		printout(os.Stderr, fmt.Sprintf("[D] "+format, details...))
	}
	return nil
}

func Trace(format string, details ...interface{}) error {
	if TraceFlag() {
		printout(os.Stderr, fmt.Sprintf("[T] "+format, details...))
	}
	return nil
}

func Stdout(format string, details ...interface{}) {
	message := format
	if len(details) > 0 {
		message = fmt.Sprintf(format, details...)
	}
	if AcceptableOutput(message) {
		fmt.Fprint(os.Stdout, message)
		os.Stdout.Sync()
	}
}

func WaitLogs() {
	defer Timeline("wait logs done")

	runtime.Gosched()
	logbarrier.Wait()
}
