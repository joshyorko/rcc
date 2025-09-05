package htfs_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/htfs"
)

func TestBasics(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	sut := "simple text"
	must.Equal(11, len(sut))
	wont.Panic(func() {
		fmt.Sprintf("%q", sut[:11])
	})
	wont.Panic(func() {
		fmt.Sprintf("%q", sut[11:])
	})
	must.Panic(func() {
		fmt.Sprintf("%q", sut[:12])
	})
	must.Panic(func() {
		fmt.Sprintf("%q", sut[12:])
	})
	must.Equal(sut, sut[:len(sut)])
	must.Equal("", sut[len(sut):])
}

func TestUsingNonhashingWorks(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	sink := bytes.NewBuffer(nil)
	sut := htfs.RelocateWriter(sink, "loha")

	wont.Nil(sut)
	size, err := sut.Write([]byte("O aloha! Aloha, Holoham!"))
	must.Nil(err)
	must.Equal(24, size)
	must.Equal([]int64{3, 10, 18}, sut.Locations())
	size, err = sut.Write([]byte("O aloha! Aloha, Holoham!"))
	must.Nil(err)
	must.Equal(24, size)
	must.Equal([]int64{3, 10, 18, 27, 34, 42}, sut.Locations())
}
