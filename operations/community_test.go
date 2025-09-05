package operations_test

import (
	"testing"

	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/operations"
)

func TestCreateCommunityLocation(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)
	wont_be.Nil(must_be)

	must_be.Equal("http://path.to/my-robot.zip", operations.CommunityLocation("http://path.to/my-robot.zip", ""))
	must_be.Equal("https://path.to/safe-robot.zip", operations.CommunityLocation("https://path.to/safe-robot.zip", "ignored"))
	must_be.Equal("https://github.com/foobart/twitter-bot/archive/main.zip", operations.CommunityLocation("github.com/foobart/twitter-bot", "main"))
	must_be.Equal("https://github.com/foobart/twitter-bot/archive/master.zip", operations.CommunityLocation("foobart/twitter-bot", "master"))
	must_be.Equal("https://github.com/robocorp/twitter-bot/archive/main.zip", operations.CommunityLocation("twitter-bot", "main"))
}
