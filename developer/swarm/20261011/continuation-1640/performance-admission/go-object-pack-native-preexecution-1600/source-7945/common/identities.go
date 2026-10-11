package common

import (
	"fmt"
	"time"
)

var (
	Identities chan string
	Startup    time.Time
)

func identityProvider(sink chan string) {
	var identity uint64
	for {
		identity += 1
		sink <- fmt.Sprintf("#%d", identity)
	}
}

func init() {
	Startup = time.Now()

	Identities = make(chan string, 3)
	go identityProvider(Identities)
}
