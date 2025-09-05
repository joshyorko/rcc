package robot_test

import (
	"testing"

	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/robot"
)

func TestCanEmptyEnvironmentWontBreakThings(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	result, err := robot.LoadEnvironmentSetup("")
	must_be.Nil(err)
	must_be.Nil(result)
	must_be.Equal(0, len(result.AsEnvironment()))
}

func TestCanGetEnvironmentFromBytes(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	result, err := robot.EnvironmentSetupFrom([]byte("{\"foo\":\"bar\",\"num\":42,\"flag\":true}"))
	must_be.Nil(err)
	wont_be.Nil(result)
	must_be.Equal("", result["missing"])
	must_be.Equal("bar", result["foo"])
	must_be.Equal("42", result["num"])
	must_be.Equal("true", result["flag"])
	must_be.Equal(3, len(result.AsEnvironment()))
}
