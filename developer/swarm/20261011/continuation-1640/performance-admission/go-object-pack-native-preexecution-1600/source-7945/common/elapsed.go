package common

import (
	"fmt"
	"time"
)

type stopwatch struct {
	message string
	started time.Time
}

type Duration time.Duration

func (it Duration) Seconds() float64 {
	return float64(it.Truncate(time.Millisecond)) / float64(time.Second)
}

func (it Duration) Truncate(granularity time.Duration) Duration {
	return Duration(time.Duration(it).Truncate(granularity))
}

func (it Duration) Milliseconds() int64 {
	return time.Duration(it).Milliseconds()
}

func (it Duration) String() string {
	return fmt.Sprintf("%5.3f", float64(it.Milliseconds())/1000.0)
}

func Stopwatch(form string, details ...interface{}) *stopwatch {
	message := fmt.Sprintf(form, details...)
	return &stopwatch{
		message: message,
		started: time.Now(),
	}
}

func (it *stopwatch) When() int64 {
	return it.started.Unix()
}

func (it *stopwatch) Time() time.Time {
	return it.started
}

func (it *stopwatch) String() string {
	elapsed := it.Elapsed().Truncate(time.Millisecond)
	return fmt.Sprintf("%v", elapsed)
}

func (it *stopwatch) Elapsed() Duration {
	return Duration(time.Since(it.started))
}

func (it *stopwatch) Debug() Duration {
	humane, elapsed := it.explained()
	Debug(humane)
	return elapsed
}

func (it *stopwatch) Log() Duration {
	humane, elapsed := it.explained()
	Log(humane)
	return elapsed
}

func (it *stopwatch) Report() Duration {
	return it.Log()
}

func (it *stopwatch) Text() string {
	humane, _ := it.explained()
	return humane
}

func (it *stopwatch) explained() (string, Duration) {
	elapsed := it.Elapsed()
	return fmt.Sprintf("%s %ss", it.message, elapsed), elapsed
}
