package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/common"
)

type serverCall struct {
	hostname string
	port     int
	domain   string
	hold     string
}

func captureServer(target *serverCall, result error) serverFunc {
	return func(hostname string, port int, domain, hold string) error {
		*target = serverCall{hostname, port, domain, hold}
		return result
	}
}

func TestRunUsesDefaultServerOptions(t *testing.T) {
	var called serverCall

	status := run(nil, commandDependencies{
		sharedHolotree: true,
		serve:          captureServer(&called, nil),
	})

	if status != nil {
		t.Fatalf("unexpected status: %#v", status)
	}
	expected := serverCall{"localhost", 4653, "personal", defaultHoldLocation()}
	if called != expected {
		t.Fatalf("unexpected server call: %#v", called)
	}
}

func TestRunForwardsExplicitServerOptions(t *testing.T) {
	var called serverCall

	status := run([]string{
		"-hostname", "127.0.0.1",
		"-port", "9000",
		"-domain", "team",
		"-hold", "/tmp/rccremote-test",
	}, commandDependencies{
		sharedHolotree: true,
		serve:          captureServer(&called, nil),
	})

	if status != nil {
		t.Fatalf("unexpected status: %#v", status)
	}
	expected := serverCall{"127.0.0.1", 9000, "team", "/tmp/rccremote-test"}
	if called != expected {
		t.Fatalf("unexpected server call: %#v", called)
	}
}

func TestRunVersionDoesNotStartServer(t *testing.T) {
	called := false
	output := ""

	status := run([]string{"-version"}, commandDependencies{
		sharedHolotree: true,
		serve: func(string, int, string, string) error {
			called = true
			return nil
		},
		stdout: func(format string, details ...interface{}) {
			output += fmt.Sprintf(format, details...)
		},
	})

	if status != nil {
		t.Fatalf("unexpected status: %#v", status)
	}
	if called {
		t.Fatal("version request started the server")
	}
	if output != common.Version+"\n" {
		t.Fatalf("unexpected version output: %q", output)
	}
}

func TestRunReturnsUsageErrorForInvalidOptions(t *testing.T) {
	status := run([]string{"-invalid-option"}, commandDependencies{})

	if status == nil || status.Code != 2 {
		t.Fatalf("unexpected status: %#v", status)
	}
	if !strings.Contains(status.Message, "flag provided but not defined") {
		t.Fatalf("invalid option missing from message: %q", status.Message)
	}
}

func TestRunReturnsUsageErrorWithoutWritingDirectlyToStderr(t *testing.T) {
	originalStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = writer
	defer func() {
		os.Stderr = originalStderr
	}()

	status := run([]string{"-invalid-option"}, commandDependencies{})
	if status == nil || status.Code != 2 {
		t.Fatalf("unexpected status: %#v", status)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != 0 {
		t.Fatalf("option parsing wrote directly to stderr: %q", output)
	}
}

func TestRunRequiresSharedHolotree(t *testing.T) {
	called := false

	status := run(nil, commandDependencies{
		serve: func(string, int, string, string) error {
			called = true
			return nil
		},
	})

	if status == nil || status.Code != 1 {
		t.Fatalf("unexpected status: %#v", status)
	}
	if called {
		t.Fatal("server started without shared holotree")
	}
}

func TestRunReturnsServerFailure(t *testing.T) {
	var called serverCall

	status := run(nil, commandDependencies{
		sharedHolotree: true,
		serve:          captureServer(&called, errors.New("cannot listen")),
	})

	if status == nil || status.Code != 1 {
		t.Fatalf("unexpected status: %#v", status)
	}
	if !strings.Contains(status.Message, "cannot listen") {
		t.Fatalf("server failure missing from message: %q", status.Message)
	}
}

func TestExitProtectionRecoversCommonExitCode(t *testing.T) {
	got := -1

	exitProtection(common.ExitCode{Code: 7, Message: "test exit"}, func(code int) {
		got = code
	})

	if got != 7 {
		t.Fatalf("unexpected exit code: %d", got)
	}
}
