package main

import (
	"flag"
	"os"
	"path/filepath"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/remotree"
)

var (
	domainId    string
	serverName  string
	serverPort  int
	versionFlag bool
	holdingArea string
	debugFlag   bool
	traceFlag   bool
)

func defaultHoldLocation() string {
	where, err := pathlib.Abs(filepath.Join(pathlib.TempDir(), "rccremotehold"))
	if err != nil {
		return "temphold"
	}
	return where
}

func init() {
	flag.BoolVar(&debugFlag, "debug", false, "Turn on debugging output.")
	flag.BoolVar(&traceFlag, "trace", false, "Turn on tracing output.")

	flag.BoolVar(&versionFlag, "version", false, "Just show rccremote version and exit.")
	flag.StringVar(&serverName, "hostname", "localhost", "Hostname/address to bind server to.")
	flag.IntVar(&serverPort, "port", 4653, "Port to bind server in given hostname.")
	flag.StringVar(&holdingArea, "hold", defaultHoldLocation(), "Directory where to put HOLD files once known.")
	flag.StringVar(&domainId, "domain", "personal", "Symbolic domain that this peer serves.")
}

func ExitProtection() {
	status := recover()
	if status != nil {
		exit, ok := status.(common.ExitCode)
		if ok {
			exit.ShowMessage()
			common.WaitLogs()
			os.Exit(exit.Code)
		}
		common.WaitLogs()
		panic(status)
	}
	common.WaitLogs()
}

func showVersion() {
	common.Stdout("%s\n", common.Version)
	os.Exit(0)
}

func process() {
	if versionFlag {
		showVersion()
	}
	pretty.Guard(common.SharedHolotree, 1, "Shared holotree must be enabled and in use for rccremote to work.")
	common.Log("Remote for rcc starting (%s) ...", common.Version)
	remotree.Serve(serverName, serverPort, domainId, holdingArea)
}

func main() {
	defer ExitProtection()
	pretty.Setup()

	flag.Parse()
	common.DefineVerbosity(false, debugFlag, traceFlag)
	process()
}
