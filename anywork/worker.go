package anywork

import (
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/joshyorko/rcc/common"
)

var (
	group       WorkGroup
	pipeline    WorkQueue
	failpipe    Failures
	errcount    Counters
	headcount   uint64
	WorkerCount int
)

type Work func()
type WorkQueue chan Work
type Failures chan string
type Counters chan uint64

func catcher(title string, identity uint64) {
	catch := recover()
	if catch != nil {
		failpipe <- fmt.Sprintf("Recovering %q #%d: %v", title, identity, catch)
	}
}

func process(fun Work, identity uint64) {
	defer catcher("process", identity)
	fun()
}

func member(identity uint64) {
	defer catcher("member", identity)
	for {
		work, ok := <-pipeline
		if !ok {
			break
		}
		process(work, identity)
		group.done()
	}
}

func watcher(failures Failures, counters Counters) {
	counter := uint64(0)
	for {
		select {
		case fail := <-failures:
			counter += 1
			fmt.Fprintln(os.Stderr, fail)
		case counters <- counter:
			counter = 0
		}
	}
}

func init() {
	group = NewGroup()
	// Large buffer to avoid backpressure on slow file systems (e.g., Windows with antivirus)
	// Memory cost is minimal (~800KB) and prevents worker stalls
	pipeline = make(WorkQueue, 100000)
	failpipe = make(Failures)
	errcount = make(Counters)
	headcount = 0
	AutoScale()
	go watcher(failpipe, errcount)
}

func Scale() uint64 {
	return headcount
}

func AutoScale() {
	// Use the optimal worker count for I/O-bound operations
	// This is more aggressive than NumCPU-1 because holotree
	// restoration is I/O-bound, not CPU-bound.
	var limit uint64
	if WorkerCount > 1 {
		// Legacy: respect explicit WorkerCount if set programmatically
		limit = uint64(WorkerCount)
	} else {
		// Use adaptive formula from common package
		limit = uint64(common.OptimalWorkerCount())
	}

	for headcount < limit {
		go member(headcount)
		headcount += 1
	}
}

func Backlog(todo Work) {
	if todo != nil {
		group.add()
		pipeline <- todo
	}
}

func Sync() error {
	trials := int(Scale())
	for retries := 0; retries < trials; retries++ {
		runtime.Gosched()
	}
	group.Wait()
	count := <-errcount
	if count > 0 {
		return fmt.Errorf("There has been %d failures. See messages above.", count)
	}
	return nil
}

func OnErrPanicCloseAll(err error, closers ...io.Closer) {
	if err != nil {
		for _, closer := range closers {
			if closer != nil {
				closer.Close()
			}
		}
		panic(err)
	}
}
