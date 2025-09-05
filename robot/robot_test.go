package robot_test

import (
	"strings"
	"testing"

	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/robot"
)

func TestCannotReadMissingRobotYaml(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	sut, err := robot.LoadRobotYaml("testdata/badmissing.yaml", false)
	wont.Nil(err)
	must.Nil(sut)
}

func TestCanAcceptPlatformFiles(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	for _, filename := range []string{"anyscript", "anyscript.bat", "anyscript.cmd"} {
		must.True(robot.PlatformAcceptableFile("amd64", "linux", filename))
		must.True(robot.PlatformAcceptableFile("amd64", "windows", filename))
		must.True(robot.PlatformAcceptableFile("amd64", "darwin", filename))
		must.True(robot.PlatformAcceptableFile("arm64", "linux", filename))
		must.True(robot.PlatformAcceptableFile("arm64", "windows", filename))
		must.True(robot.PlatformAcceptableFile("arm64", "darwin", filename))
	}

	for _, filename := range []string{"any.bat", "any.cmd", "any.sh", "at_linux.sh", "at_arm64.sh", "at_arm64_linux.sh"} {
		must.True(robot.PlatformAcceptableFile("arm64", "linux", filename))
	}

	for _, filename := range []string{"any.bat", "any.cmd", "any.sh", "at_darwin.sh", "at_arm64.sh", "at_arm64_darwin.sh"} {
		must.True(robot.PlatformAcceptableFile("arm64", "darwin", filename))
	}

	for _, filename := range []string{"any.bat", "any.cmd", "any.sh", "at_windows.sh", "at_amd64.sh", "at_amd64_windows.sh"} {
		must.True(robot.PlatformAcceptableFile("amd64", "windows", filename))
	}

	for _, filename := range []string{"at_arm64.sh", "at_windows.bat", "at_darwin.sh", "at_arm64_linux.sh"} {
		wont.True(robot.PlatformAcceptableFile("amd64", "linux", filename))
	}

	for _, filename := range []string{"at_linux.sh", "at_arm64.sh", "at_amd64_darwin.sh", "at_amd64_linux.sh", "at_arm64_windows.sh"} {
		wont.True(robot.PlatformAcceptableFile("amd64", "windows", filename))
	}

	for _, filename := range []string{"at_linux.sh", "at_arm64.sh", "at_arm64_darwin.sh", "at_amd64_linux.sh", "at_amd64_windows.sh"} {
		wont.True(robot.PlatformAcceptableFile("amd64", "darwin", filename))
	}
}

func TestCanMatchArchitecture(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	wont.Nil(robot.GoosPattern)
	wont.Nil(robot.GoarchPattern)
	must.Equal([]string{"darwin", "darwin"}, robot.GoosPattern.FindStringSubmatch("foo_darwin_arm64_freeze.yaml"))
	must.Equal([]string{"arm64", "arm64"}, robot.GoarchPattern.FindStringSubmatch("foo_darwin_arm64_freeze.yaml"))
}

func TestCanReadRealRobotYaml(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	sut, err := robot.LoadRobotYaml("testdata/robot.yaml", false)
	must.Nil(err)
	wont.Nil(sut)
	must.Equal(1, len(sut.IgnoreFiles()))
	must.Equal(3, len(sut.AvailableTasks()))
	must.Nil(sut.DefaultTask())
	must.Nil(sut.TaskByName(""))
	must.Nil(sut.TaskByName("missing task"))
	wont.Nil(sut.TaskByName("task form name"))
	wont.Nil(sut.TaskByName("Shell Form Name"))
	wont.Nil(sut.TaskByName("  Old command form name "))
	must.Equal(1, len(sut.Paths()))
	must.Equal(3, len(sut.PythonPaths()))
	must.True(2 < len(sut.SearchPath(".")))
	must.True(strings.HasSuffix(sut.CondaConfigFile(), "conda.yaml"))
	must.True(strings.HasSuffix(sut.WorkingDirectory(), "testdata"))
	must.True(strings.HasSuffix(sut.ArtifactDirectory(), "output"))
	valid, err := sut.Validate()
	must.True(valid)
	must.Nil(err)
}

func TestCanGetShellFormCommand(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	sut, err := robot.LoadRobotYaml("testdata/robot.yaml", false)
	must.Nil(err)
	wont.Nil(sut)
	task := sut.TaskByName("Shell Form Name")
	wont.Nil(task)

	command := task.Commandline()
	wont.Nil(command)
	must.Equal(8, len(command))
	must.Equal("tasks/shilling.robot", command[7])
}

func TestCanGetTaskFormCommand(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	sut, err := robot.LoadRobotYaml("testdata/robot.yaml", false)
	must.Nil(err)
	wont.Nil(sut)
	task := sut.TaskByName("Task Form Name")
	wont.Nil(task)

	command := task.Commandline()
	wont.Nil(command)
	must.Equal(12, len(command))
}
