package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pathlib"
	"github.com/joshyorko/rcc/pretty"
	"github.com/joshyorko/rcc/remotree"
)

type serverFunc func(address string, port int, domain, storage string) error

type commandOptions struct {
	domain   string
	hostname string
	port     int
	version  bool
	hold     string
	debug    bool
	trace    bool
}

type commandDependencies struct {
	sharedHolotree bool
	serve          serverFunc
	stdout         func(string, ...interface{})
}

func defaultHoldLocation() string {
	where, err := pathlib.Abs(filepath.Join(pathlib.TempDir(), "rccremotehold"))
	if err != nil {
		return "temphold"
	}
	return where
}

func parseOptions(arguments []string) (commandOptions, string, error) {
	options := commandOptions{
		domain:   "personal",
		hostname: "localhost",
		port:     4653,
		hold:     defaultHoldLocation(),
	}
	var output strings.Builder
	flags := flag.NewFlagSet("rccremote", flag.ContinueOnError)
	flags.SetOutput(&output)
	flags.BoolVar(&options.debug, "debug", false, "Turn on debugging output.")
	flags.BoolVar(&options.trace, "trace", false, "Turn on tracing output.")
	flags.BoolVar(&options.version, "version", false, "Just show rccremote version and exit.")
	flags.StringVar(&options.hostname, "hostname", options.hostname, "Hostname/address to bind server to.")
	flags.IntVar(&options.port, "port", options.port, "Port to bind server in given hostname.")
	flags.StringVar(&options.hold, "hold", options.hold, "Directory where to put HOLD files once known.")
	flags.StringVar(&options.domain, "domain", options.domain, "Symbolic domain that this peer serves.")
	err := flags.Parse(arguments)
	return options, output.String(), err
}

func exitProtection(status interface{}, exitProcess func(int)) {
	if status != nil {
		exit, ok := status.(common.ExitCode)
		if ok {
			exit.ShowMessage()
			common.WaitLogs()
			exitProcess(exit.Code)
			return
		}
		common.WaitLogs()
		panic(status)
	}
	common.WaitLogs()
}

func ExitProtection() {
	exitProtection(recover(), os.Exit)
}

func run(arguments []string, dependencies commandDependencies) *common.ExitCode {
	if dependencies.stdout == nil {
		dependencies.stdout = common.Stdout
	}
	options, flagOutput, err := parseOptions(arguments)
	if err == flag.ErrHelp {
		dependencies.stdout("%s", flagOutput)
		return nil
	}
	if err != nil {
		return &common.ExitCode{Code: 2, Message: strings.TrimSpace(flagOutput)}
	}
	common.DefineVerbosity(false, options.debug, options.trace)
	if options.version {
		dependencies.stdout("%s\n", common.Version)
		return nil
	}
	if !dependencies.sharedHolotree {
		return &common.ExitCode{Code: 1, Message: "Shared holotree must be enabled and in use for rccremote to work."}
	}
	common.Log("Remote for rcc starting (%s) ...", common.Version)
	err = dependencies.serve(options.hostname, options.port, options.domain, options.hold)
	if err != nil {
		return &common.ExitCode{Code: 1, Message: fmt.Sprintf("Remote server failed: %v", err)}
	}
	return nil
}

func main() {
	defer ExitProtection()
	pretty.Setup()

	status := run(os.Args[1:], commandDependencies{
		sharedHolotree: common.SharedHolotree,
		serve:          remotree.Serve,
		stdout:         common.Stdout,
	})
	if status != nil {
		panic(*status)
	}
}
