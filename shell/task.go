package shell

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/google/shlex"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
)

type (
	Common interface {
		Debug(string, ...interface{}) error
		Trace(string, ...interface{}) error
		Timeline(string, ...interface{})
	}

	Task struct {
		environment []string
		directory   string
		executable  string
		args        []string
		stderronly  bool
		nostderr    bool
	}

	Wrapper func()
)

func Split(commandline string) ([]string, error) {
	return shlex.Split(commandline)
}

func New(environment []string, directory string, task ...string) *Task {
	executable, args := task[0], task[1:]
	return &Task{
		environment: environment,
		directory:   directory,
		executable:  executable,
		args:        args,
		stderronly:  false,
		nostderr:    false,
	}
}

func (it *Task) StderrOnly() *Task {
	it.stderronly = true
	return it
}

func (it *Task) NoStderr() *Task {
	it.nostderr = true
	return it
}

func (it *Task) stdout() io.Writer {
	if it.stderronly {
		return os.Stderr
	}
	return os.Stdout
}

func (it *Task) execute(stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	common.Trace("Execute %q with arguments %q", it.executable, it.args)
	command := exec.Command(it.executable, it.args...)
	command.Env = it.environment
	command.Dir = it.directory
	command.Stdin = stdin
	command.Stdout = stdout
	if it.nostderr {
		command.Stderr = nil
	} else {
		command.Stderr = stderr
	}
	command.WaitDelay = 3 * time.Second
	err := command.Start()
	if err != nil {
		return -500, err
	}
	common.Timeline("exec %q started", it.executable)
	common.Debug("PID #%d is %q.", command.Process.Pid, command)
	defer func() {
		if command.ProcessState.ExitCode() != 0 {
			common.Log("Process %d: %v, command: %s %s [%s/%d]", command.Process.Pid, command.ProcessState, it.executable, it.args, common.Version, os.Getpid())
		} else {
			common.Debug("PID #%d finished: %v.", command.Process.Pid, command.ProcessState)
		}
	}()
	err = command.Wait()
	exit, ok := err.(*exec.ExitError)
	if ok {
		return exit.ExitCode(), err
	}
	if err != nil {
		return -500, err
	}
	return 0, nil
}

func (it *Task) Transparent() (int, error) {
	return it.execute(os.Stdin, it.stdout(), os.Stderr)
}

func (it *Task) Execute(interactive bool) (int, error) {
	var stdin io.Reader = os.Stdin
	if !interactive {
		stdin = bytes.NewReader([]byte{})
	}
	return it.execute(stdin, it.stdout(), os.Stderr)
}

func (it *Task) Tee(folder string, interactive bool) (int, error) {
	err := os.MkdirAll(folder, 0755)
	if err != nil {
		return -600, err
	}
	outfile, err := pathlib.Create(filepath.Join(folder, "stdout.log"))
	if err != nil {
		return -601, err
	}
	defer outfile.Close()
	errfile, err := pathlib.Create(filepath.Join(folder, "stderr.log"))
	if err != nil {
		return -602, err
	}
	defer errfile.Close()
	stdout := io.MultiWriter(it.stdout(), outfile)
	stderr := io.MultiWriter(os.Stderr, errfile)
	var stdin io.Reader = os.Stdin
	if !interactive {
		stdin = bytes.NewReader([]byte{})
	}
	return it.execute(stdin, stdout, stderr)
}

func (it *Task) Observed(sink io.Writer, interactive bool) (int, error) {
	stdout := io.MultiWriter(it.stdout(), sink)
	stderr := io.MultiWriter(os.Stderr, sink)
	var stdin io.Reader = os.Stdin
	if !interactive {
		stdin = bytes.NewReader([]byte{})
	}
	return it.execute(stdin, stdout, stderr)
}

func (it *Task) Tracked(sink io.Writer, interactive bool) (int, error) {
	var stdin io.Reader = os.Stdin
	if !interactive {
		stdin = bytes.NewReader([]byte{})
	}
	return it.execute(stdin, sink, sink)
}

func (it *Task) CaptureOutput() (string, int, error) {
	stdin := bytes.NewReader([]byte{})
	stdout := bytes.NewBuffer(nil)
	code, err := it.execute(stdin, stdout, os.Stderr)
	return stdout.String(), code, err
}

func WithInterrupt(task Wrapper) {
	signals := make(chan os.Signal, 1)
	defer signal.Stop(signals)
	defer close(signals)
	go func() {
		signal.Notify(signals, os.Interrupt)
		got, ok := <-signals
		if ok {
			pretty.Note("Detected and ignored %q signal. Second one will not be ignored. [rcc]", got)
		}
		signal.Stop(signals)
	}()
	task()
}
