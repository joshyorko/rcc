package mocks_test

import (
	"testing"

	"github.com/robocorp/rcc/cloud"
	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/mocks"
)

func TestCanWorkWithMockClient(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	fake := cloud.Response{Status: 200, Body: []byte("{}")}
	sut := mocks.NewClient(&fake, &fake, &fake, &fake)
	wont_be.Nil(sut)
	must_be.Text("https://this.is/mock", sut.Endpoint())
	other, err := sut.NewClient("https://other.is/mock")
	must_be.Nil(err)
	must_be.Same(sut, other)

	must_be.Panic(func() {
		sut.Verify(nil)
	})

	wont_be.Nil(sut.NewRequest("/the/path"))
	wont_be.Nil(sut.Get(nil))
	wont_be.Nil(sut.Put(nil))
	wont_be.Nil(sut.Post(nil))
	wont_be.Nil(sut.Delete(nil))
	sut.Verify(t)
}
