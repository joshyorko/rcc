package xviper_test

import (
	"testing"

	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/xviper"
)

func TestCanFormatToGuidForm(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	must_be.Equal("00000000-0000-0000-0000-000000000000", xviper.AsGuid([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}))
	must_be.Equal("00010203-0405-0607-0809-0a0b0c0d0e0f", xviper.AsGuid([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}))
}

func TestCanGetTrackingIdentity(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	first := xviper.TrackingIdentity()
	must_be.True(len(first) == 36)
	again := xviper.TrackingIdentity()
	must_be.True(len(again) == 36)
	must_be.Equal(first, again)
}
