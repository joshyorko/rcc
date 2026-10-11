package common

import (
	"fmt"
	"strings"
)

var (
	TimelineEnabled bool
	pipe            chan string
	indent          chan bool
	done            chan bool
)

type timevent struct {
	level int
	when  Duration
	what  string
}

func timeliner(events chan string, indent, done chan bool) {
	history := make([]*timevent, 0, 100)
	level := 0
loop:
	for {
		select {
		case event, ok := <-events:
			if !ok {
				break loop
			}
			history = append(history, &timevent{level, Clock.Elapsed(), event})
		case deeper, ok := <-indent:
			if !ok {
				break loop
			}
			if deeper {
				level += 1
			} else {
				level -= 1
			}
			if level < 0 {
				level = 0
			}
		}
	}
	death := Clock.Elapsed()
	if TimelineEnabled && death.Milliseconds() > 0 {
		history = append(history, &timevent{0, death, "Now."})
		Log("----  rcc timeline  ----")
		Log(" #  percent  seconds  event [rcc %s]", Version)
		for at, event := range history {
			permille := event.when * 1000 / death
			percent := float64(permille) / 10.0
			indent := strings.Repeat("| ", event.level)
			Log("%3d:  %5.1f%%  %7s  %s%s", at+1, percent, event.when, indent, event.what)
		}
		Log("----  rcc timeline  ----")
	}
	close(done)
}

func init() {
	pipe = make(chan string)
	indent = make(chan bool)
	done = make(chan bool)
	go timeliner(pipe, indent, done)
}

func IgnoreAllPanics() {
	recover()
}

func Timeline(form string, details ...interface{}) {
	defer IgnoreAllPanics()
	pipe <- fmt.Sprintf(form, details...)
}

func TimelineBegin(form string, details ...interface{}) {
	Timeline(form, details...)
	indent <- true
}

func TimelineEnd() {
	indent <- false
	Timeline("`--")
}

func EndOfTimeline() {
	TimelineEnd()
	close(pipe)
	close(indent)
	<-done
}
