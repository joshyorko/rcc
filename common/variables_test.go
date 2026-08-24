package common

import (
	"sync"
	"testing"
)

func TestResolveSharedHolotreeMode(t *testing.T) {
	tests := []struct {
		marker bool
		mode   string
		want   bool
	}{
		{marker: false, mode: "", want: false},
		{marker: true, mode: "", want: true},
		{marker: true, mode: "private", want: false},
		{marker: false, mode: "shared", want: true},
		{marker: true, mode: "unexpected", want: true},
	}
	for _, test := range tests {
		if got := resolveSharedHolotreeMode(test.marker, test.mode); got != test.want {
			t.Fatalf("marker=%v mode=%q: got %v, want %v", test.marker, test.mode, got, test.want)
		}
	}
}

func TestVerbosityConcurrentAccess(t *testing.T) {
	const iterations = 10_000
	t.Setenv(RCC_VERBOSITY, "")

	start := make(chan struct{})
	readers := []func() bool{DebugFlag, TraceFlag, Silent}
	results := make([]int, len(readers))
	var wait sync.WaitGroup
	wait.Add(len(readers) + 1)

	for index, read := range readers {
		go func(index int, read func() bool) {
			defer wait.Done()
			<-start
			for range iterations {
				if read() {
					results[index]++
				}
			}
		}(index, read)
	}
	go func() {
		defer wait.Done()
		<-start
		for index := range iterations {
			switch index % 4 {
			case 0:
				DefineVerbosity(true, false, false)
			case 1:
				DefineVerbosity(false, false, false)
			case 2:
				DefineVerbosity(false, true, false)
			case 3:
				DefineVerbosity(false, false, true)
			}
		}
	}()

	close(start)
	wait.Wait()

	DefineVerbosity(false, false, false)
	if Silent() || DebugFlag() || TraceFlag() {
		t.Fatal("normal verbosity flags changed after concurrent access")
	}
}
