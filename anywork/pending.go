package anywork

type (
	WorkGroup interface {
		add()
		done()
		Wait()
	}

	workgroup struct {
		level   waitpipe
		waiting waiting
	}

	waitpipe chan bool
	waiting  chan waitpipe
)

func NewGroup() WorkGroup {
	group := &workgroup{
		level:   make(waitpipe),
		waiting: make(waiting),
	}
	go group.waiter()
	return group
}

func (it *workgroup) add() {
	it.level <- true
}

func (it *workgroup) done() {
	it.level <- false
}

func (it *workgroup) Wait() {
	reply := make(waitpipe)
	it.waiting <- reply
	_, _ = <-reply
}

func (it *workgroup) waiter() {
	var counter int64
	pending := make([]waitpipe, 0, 5)
forever:
	for {
		if counter < 0 {
			panic("anywork: counter below zero")
		}
		if counter == 0 && len(pending) > 0 {
			for _, waiter := range pending {
				close(waiter)
			}
			pending = make([]waitpipe, 0, 5)
		}
		select {
		case up, ok := <-it.level:
			if !ok {
				break forever
			}
			if up {
				counter += 1
			} else {
				counter -= 1
			}
		case waiter, ok := <-it.waiting:
			if !ok {
				break forever
			}
			pending = append(pending, waiter)
		}
	}
	panic("anywork: for some reason, waiter have just exited")
}
