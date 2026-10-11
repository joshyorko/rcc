package htfs

import (
	"bytes"
	"io"
)

type (
	WriteLocator interface {
		io.Writer
		Locations() []int64
	}

	simple struct {
		windowsize int
		window     []byte
		trigger    byte
		needle     []byte
		history    int64
		delegate   io.Writer
		found      []int64
	}
)

func RelocateWriter(delegate io.Writer, needle string) WriteLocator {
	blob := []byte(needle)
	windowsize := len(blob)
	result := &simple{
		windowsize: windowsize,
		window:     make([]byte, 0, 8192+windowsize),
		trigger:    blob[windowsize-1],
		needle:     blob,
		history:    0,
		delegate:   delegate,
		found:      make([]int64, 0, 20),
	}
	return result
}

func (it *simple) trimWindow() {
	shift := len(it.window) - it.windowsize
	if shift > 0 {
		copy(it.window, it.window[shift:])
		it.window = it.window[:it.windowsize]
	}
}

func (it *simple) Write(payload []byte) (int, error) {
	pending := len(it.window)
	it.window = append(it.window, payload...)
	defer it.trimWindow()

	shift, view, trigger, limit := 0, it.window, it.trigger, it.windowsize
search:
	for limit < len(view) {
		found := bytes.IndexByte(view, trigger)
		if found < 0 {
			break search
		}
		head := view[:found+1]
		view = view[found+1:]
		end := shift + found + 1 - pending
		start := end - limit
		headsize := len(head)
		if limit <= headsize {
			candidate := head[headsize-limit:]
			relation := bytes.Compare(it.needle, candidate)
			if relation == 0 {
				it.found = append(it.found, it.history+int64(start))
			}
		}
		shift += len(head)
	}

	// seek here when found, append to it.found
	it.history += int64(len(payload))
	return it.delegate.Write(payload)
}

func (it *simple) Locations() []int64 {
	return it.found
}
