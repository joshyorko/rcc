package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"

	"github.com/robocorp/rcc/anywork"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/conda"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/set"
	"github.com/robocorp/rcc/xviper"

	"github.com/spf13/cobra"
)

var (
	anythingIgnore  string
	profilefile     string
	profiling       *os.File
	versionFlag     bool
	silentFlag      bool
	debugFlag       bool
	traceFlag       bool
	productFakeFlag bool // this is handled in common init

	excludedCommands = []string{"completion"}
)

func toplevelCommands(parent *cobra.Command) {
	common.Log("\nToplevel commands (%v)", common.Version)
	for _, child := range parent.Commands() {
		if child.Hidden || len(child.Commands()) > 0 {
			continue
		}
		name := strings.Split(child.Use, " ")
		common.Log("| %s%-14s%s  %s", pretty.Cyan, name[0], pretty.Reset, child.Short)
	}
}

func commandTree(level int, prefix string, parent *cobra.Command) {
	if parent.Hidden {
		return
	}
	if level == 1 && len(parent.Commands()) == 0 {
		return
	}
	name := strings.Split(parent.Use, " ")
	if set.Member(excludedCommands, name[0]) {
		return
	}
	if level == 1 {
		common.Log("%s", strings.TrimSpace(prefix))
	}
	label := fmt.Sprintf("%s%s", prefix, name[0])
	common.Log("%-16s  %s", label, parent.Short)
	indent := prefix + "| "
	for _, child := range parent.Commands() {
		commandTree(level+1, indent, child)
	}
}

var rootCmd = &cobra.Command{
	Use:   "rcc",
	Short: fmt.Sprintf("rcc is environment manager for %s Automation Stack", common.Product.Name()),
	Long: fmt.Sprintf(`rcc provides support for creating and managing tasks,
communicating with %s Control Room, and managing virtual environments where
tasks can be developed, debugged, and run.`, common.Product.Name()),
	Run: func(cmd *cobra.Command, args []string) {
		if versionFlag {
			common.Stdout("%s\n", common.Version)
		} else {
			commandTree(0, "", cmd.Root())
			toplevelCommands(cmd.Root())
		}
	},
}

func Origin() string {
	target, _, err := rootCmd.Find(os.Args[1:])
	origin := []string{common.Version}
	for err == nil && target != nil {
		origin = append(origin, target.Name())
		target = target.Parent()
	}
	return strings.Join(origin, ":")
}

func Execute() {
	defer func() {
		if profiling != nil {
			common.Timeline("closing profiling started")
			pprof.StopCPUProfile()
			profiling.Sync()
			profiling.Close()
			common.TimelineEnd()
		}
	}()

	rootCmd.SetArgs(os.Args[1:])

	err := rootCmd.Execute()
	pretty.Guard(err == nil, 1, "Error: [rcc %v] %v", common.Version, err)
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.Flags().BoolVarP(&versionFlag, "version", "v", false, "Show rcc version and exit.")

	rootCmd.PersistentFlags().StringVar(&profilefile, "pprof", "", "Filename to save profiling information.")
	rootCmd.PersistentFlags().StringVar(&common.ControllerType, "controller", "user", "internal, DO NOT USE (unless you know what you are doing)")
	rootCmd.PersistentFlags().StringVar(&common.SemanticTag, "tag", "transient", "semantic reason/context, why are you invoking rcc")
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", fmt.Sprintf("config file (default is $%s/rcc.yaml)", common.Product.HomeVariable()))
	rootCmd.PersistentFlags().StringVar(&anythingIgnore, "anything", "", "freeform string value that can be set without any effect, for example CLI versioning/reference")

	rootCmd.PersistentFlags().BoolVarP(&productFakeFlag, "sema4ai", "", false, "Select Sema4.ai toolset strategy.")
	rootCmd.PersistentFlags().BoolVarP(&productFakeFlag, "robocorp", "", false, "Select Robocorp toolset strategy.")
	rootCmd.PersistentFlags().BoolVarP(&common.NoBuild, "no-build", "", false, "never allow building new environments, only use what exists already in hololib (also RCC_NO_BUILD=1)")
	rootCmd.PersistentFlags().BoolVarP(&common.NoRetryBuild, "no-retry-build", "", false, "no retry in case of first environment build fails, just report error immediately")
	rootCmd.PersistentFlags().BoolVarP(&silentFlag, "silent", "", false, "be less verbose on output (also RCC_VERBOSITY=silent)")
	rootCmd.PersistentFlags().BoolVarP(&common.Liveonly, "liveonly", "", false, "do not create base environment from live ... DANGER! For containers only!")
	rootCmd.PersistentFlags().BoolVarP(&pathlib.Lockless, "lockless", "", false, "do not use file locking ... DANGER!")
	rootCmd.PersistentFlags().BoolVarP(&pretty.Colorless, "colorless", "", false, "do not use colors in CLI UI")
	rootCmd.PersistentFlags().BoolVarP(&common.NoCache, "nocache", "", false, "do not use cache for credentials and tokens, always request them from cloud")

	rootCmd.PersistentFlags().BoolVarP(&common.LogLinenumbers, "numbers", "", false, "put line numbers on rcc produced log output")
	rootCmd.PersistentFlags().BoolVarP(&debugFlag, "debug", "", false, "to get debug output where available (not for normal production use; also RCC_VERBOSITY=debug)")
	rootCmd.PersistentFlags().BoolVarP(&traceFlag, "trace", "", false, "to get trace output where available (not for normal production use; also RCC_VERBOSITY=trace)")
	rootCmd.PersistentFlags().BoolVarP(&common.TimelineEnabled, "timeline", "", false, "print timeline at the end of run")
	rootCmd.PersistentFlags().BoolVarP(&common.StrictFlag, "strict", "", false, "be more strict on environment creation and handling")
	rootCmd.PersistentFlags().IntVarP(&anywork.WorkerCount, "workers", "", 0, "scale background workers manually (do not use, unless you know what you are doing)")
	rootCmd.PersistentFlags().BoolVarP(&common.UnmanagedSpace, "unmanaged", "", false, "work with unmanaged holotree spaces, DO NOT USE (unless you know what you are doing)")
	rootCmd.PersistentFlags().BoolVarP(&common.WarrantyVoidedFlag, "warranty-voided", "", common.WarrantyVoidedFlag, "experimental, warranty voided, dangerous mode ... DO NOT USE (unless you know what you are doing)")
	rootCmd.PersistentFlags().BoolVarP(&common.NoTempManagement, "no-temp-management", "", common.NoTempManagement, "rcc wont do any temp directory management ... DO NOT USE (unless you know what you are doing)")
	rootCmd.PersistentFlags().BoolVarP(&common.NoPycManagement, "no-pyc-management", "", common.NoPycManagement, "rcc wont do any .pyc file management ... DO NOT USE (unless you know what you are doing)")
	rootCmd.PersistentFlags().StringArrayVarP(&common.LogHides, "log-hide", "", []string{}, "hide logging output that matches given text fragment and this option can be given multiple times")
	rootCmd.PersistentFlags().BoolVarP(&common.BundledFlag, "bundled", "", common.BundledFlag, "used to tell rcc, that this is bundled use (do not use, unless you know what you are doing)")
}

func initConfig() {
	if profilefile != "" {
		common.TimelineBegin("profiling run started")
		sink, err := pathlib.Create(profilefile)
		pretty.Guard(err == nil, 5, "Failed to create profile file %q, reason %v.", profilefile, err)
		err = pprof.StartCPUProfile(sink)
		pretty.Guard(err == nil, 6, "Failed to start CPU profile, reason %v.", err)
		profiling = sink
	}
	if cfgFile != "" {
		xviper.SetConfigFile(cfgFile)
	} else {
		xviper.SetConfigFile(filepath.Join(common.Product.Home(), "rcc.yaml"))
	}

	common.DefineVerbosity(silentFlag, debugFlag, traceFlag)
	common.UnifyStageHandling()

	pretty.Setup()

	if common.WarrantyVoided() {
		pretty.Warning("Note that 'rcc' is running in 'warranty voided' mode.")
	}

	common.Timeline("%q", os.Args)
	common.Trace("CLI command was: %#v", os.Args)
	common.Debug("Using config file: %v", xviper.ConfigFileUsed())
	conda.ValidateLocations()
	anywork.AutoScale()
}
