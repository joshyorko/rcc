package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
	exposeFlag  bool
	tunnelName  string
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
	flag.BoolVar(&exposeFlag, "expose", false, "Expose server via Cloudflare Quick Tunnel.")
	flag.StringVar(&tunnelName, "tunnel-name", "", "Use Named Tunnel instead of Quick Tunnel (requires CF_TUNNEL_TOKEN).")
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

	var tunnelMgr *remotree.TunnelManager

	if exposeFlag {
		// Create tunnel manager (tunnelName empty for Quick Tunnel)
		tunnelMgr = remotree.NewTunnelManager(tunnelName)

		// Start tunnel in background
		err := tunnelMgr.Start(serverPort)
		if err != nil {
			common.Log("Failed to start tunnel: %v", err)
			pretty.Guard(false, 2, "Failed to start tunnel: %v", err)
		}
		defer tunnelMgr.Stop()

		// Wait for public URL to be assigned
		publicURL, err := tunnelMgr.GetPublicURL(10 * time.Second)
		if err != nil {
			common.Log("Failed to get public URL: %v", err)
			pretty.Guard(false, 3, "Failed to get public URL: %v", err)
		}

		tunnelType := "Quick Tunnel"
		if tunnelName != "" {
			tunnelType = fmt.Sprintf("Named Tunnel: %s", tunnelName)
		}

		common.Stdout("\n")
		common.Stdout("  🌍 Public URL: %s\n", publicURL)
		common.Stdout("  🔐 Tunnel Status: Connected (%s)\n", tunnelType)
		common.Stdout("\n")
	}

	remotree.Serve(serverName, serverPort, domainId, holdingArea)
}

func main() {
	defer ExitProtection()
	pretty.Setup()

	flag.Parse()
	common.DefineVerbosity(false, debugFlag, traceFlag)
	process()
}
