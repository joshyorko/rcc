package common

import (
	"fmt"
	"time"
)

const (
	perfMessage = `

Here are performance score results. Higher is better, 0 is reference point.

Score for network=%d, filesystem=%d, and time=%d with %d workers on %q.
`
	topScale = 125
	netScale = 1000
	fsScale  = 100
)

type Scorecard interface {
	Start() Scorecard
	Midpoint() Scorecard
	Done() Scorecard
	Score(uint64, int) string
}

type scorecard struct {
	start      time.Time
	network    time.Time
	filesystem time.Time
}

func (it *scorecard) Score(scale uint64, seconds int) string {
	network := it.network.Sub(it.start).Milliseconds()
	filesystem := it.filesystem.Sub(it.network).Milliseconds()
	Debug("Raw score values: network=%d and filesystem=%d", network, filesystem)
	if network < 1 || filesystem < 0 {
		return "Score: N/A [measurement not done]"
	}

	return fmt.Sprintf(perfMessage, topScale-(network/netScale), topScale-(filesystem/fsScale), seconds, scale, Platform())
}

func (it *scorecard) Start() Scorecard {
	it.start = time.Now()
	return it
}

func (it *scorecard) Midpoint() Scorecard {
	it.network = time.Now()
	return it
}

func (it *scorecard) Done() Scorecard {
	it.filesystem = time.Now()
	return it
}

func NewScorecard() Scorecard {
	marker := time.Now()
	return &scorecard{
		start:      marker,
		network:    marker,
		filesystem: marker,
	}
}
