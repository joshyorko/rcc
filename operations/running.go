package operations

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/joshyorko/rcc/anywork"
	"github.com/joshyorko/rcc/cloud"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/conda"
	"github.com/joshyorko/rcc/htfs"
	"github.com/joshyorko/rcc/journal"
	"github.com/joshyorko/rcc/pathlib"
	"github.com/joshyorko/rcc/pretty"
	"github.com/joshyorko/rcc/robot"
	"github.com/joshyorko/rcc/shell"
)

const (
	actualRun      = `actual main robot run`
	preRun         = `pre-run script execution`
	newEnvironment = `environment creation`
)

var (
	rcHosts  = []string{"RC_API_SECRET_HOST", "RC_API_WORKITEM_HOST"}
	rcTokens = []string{"RC_API_SECRET_TOKEN", "RC_API_WORKITEM_TOKEN"}
)

// dashboardWriter wraps an io.Writer and feeds output lines to a dashboard
type dashboardWriter struct {
	underlying io.Writer
	dashboard  pretty.Dashboard
	buffer     []byte
}

func newDashboardWriter(underlying io.Writer, dashboard pretty.Dashboard) *dashboardWriter {
	return &dashboardWriter{
		underlying: underlying,
		dashboard:  dashboard,
		buffer:     make([]byte, 0, 256),
	}
}

// NewDashboardWriterForTest is exported for testing purposes
func NewDashboardWriterForTest(underlying io.Writer, dashboard pretty.Dashboard) io.Writer {
	return newDashboardWriter(underlying, dashboard)
}

func (w *dashboardWriter) Write(p []byte) (n int, err error) {
	// Write to underlying writer first if it exists
	if w.underlying != nil {
		n, err = w.underlying.Write(p)
		if err != nil {
			return n, err
		}
	} else {
		// If no underlying writer, just report the bytes as written
		n = len(p)
	}

	// Process lines for dashboard
	for _, b := range p {
		if b == '\n' {
			// Complete line - send to dashboard
			line := string(w.buffer)
			if w.dashboard != nil && line != "" {
				w.dashboard.AddOutput(line)
			}
			w.buffer = w.buffer[:0]
		} else {
			w.buffer = append(w.buffer, b)
		}
	}

	return n, nil
}

type TokenPeriod struct {
	ValidityTime int // minutes
	GracePeriod  int // minutes
}

type RunFlags struct {
	*TokenPeriod
	AccountName     string
	WorkspaceId     string
	EnvironmentFile string
	RobotYaml       string
	Assistant       bool
	NoPipFreeze     bool
}

func (it *TokenPeriod) EnforceGracePeriod() *TokenPeriod {
	if it == nil {
		return it
	}
	if it.GracePeriod < 5 {
		it.GracePeriod = 5
	}
	if it.GracePeriod > 120 {
		it.GracePeriod = 120
	}
	if it.ValidityTime < 15 {
		it.ValidityTime = 15
	}
	return it
}

func asSeconds(minutes int) int {
	return 60 * minutes
}

func DefaultTokenPeriod() *TokenPeriod {
	result := &TokenPeriod{}
	return result.EnforceGracePeriod()
}

func (it *TokenPeriod) AsSeconds() (int, int, bool) {
	if it == nil {
		return asSeconds(15), asSeconds(5), false
	}
	it.EnforceGracePeriod()
	return asSeconds(it.ValidityTime), asSeconds(it.GracePeriod), true
}

func (it *TokenPeriod) Liveline() int64 {
	valid, _, _ := it.AsSeconds()
	when := time.Now().Unix()
	return when + int64(valid)
}

func (it *TokenPeriod) Deadline() int64 {
	valid, grace, _ := it.AsSeconds()
	when := time.Now().Unix()
	return when + int64(valid+grace)
}

func (it *TokenPeriod) RequestSeconds() int {
	valid, grace, _ := it.AsSeconds()
	return int(valid + grace)
}

func FreezeEnvironmentListing(label string, config robot.Robot) {
	goldenfile := conda.GoldenMasterFilename(label)
	listing := conda.LoadWantedDependencies(goldenfile)
	if len(listing) == 0 {
		common.Log("No dependencies found at %q", goldenfile)
		return
	}
	env, err := conda.ReadPackageCondaYaml(config.CondaConfigFile(), false)
	if err != nil {
		common.Log("Could not read %q, reason: %v", config.CondaConfigFile(), err)
		return
	}
	frozen := env.FreezeDependencies(listing)
	err = frozen.SaveAs(config.FreezeFilename())
	if err != nil {
		common.Log("Could not save %q, reason: %v", config.FreezeFilename(), err)
	}
}

func ExecutionEnvironmentListing(wantedfile, label string, searchPath pathlib.PathParts, directory, outputDir string, environment []string) bool {
	common.Timeline("execution environment listing")
	defer common.Log("--")
	goldenfile := conda.GoldenMasterFilename(label)
	err := conda.SideBySideViewOfDependencies(goldenfile, wantedfile)
	if err != nil {
		pip, ok := searchPath.Which("pip", conda.FileExtensions)
		if !ok {
			return false
		}
		fullPip, err := filepath.EvalSymlinks(pip)
		if err != nil {
			return false
		}
		common.Log("Installed pip packages:")
		if common.NoOutputCapture {
			_, err = shell.New(environment, directory, fullPip, "freeze", "--all").Execute(false)
		} else {
			_, err = shell.New(environment, directory, fullPip, "freeze", "--all").Tee(outputDir, false)
		}
	}
	if err != nil {
		return false
	}
	return true
}

func LoadAnyTaskEnvironment(packfile string, force bool) (bool, robot.Robot, robot.Task, string) {
	FixRobot(packfile)
	config, err := robot.LoadRobotYaml(packfile, false)
	if err != nil {
		pretty.Exit(1, "Error: %v", err)
	}
	anytasks := config.AvailableTasks()
	if len(anytasks) == 0 {
		pretty.Exit(1, "Could not find tasks from %q.", packfile)
	}
	return LoadTaskWithEnvironment(packfile, anytasks[0], force)
}

func LoadTaskWithEnvironment(packfile, theTask string, force bool) (bool, robot.Robot, robot.Task, string) {
	common.Timeline("task environment load started")
	FixRobot(packfile)
	config, err := robot.LoadRobotYaml(packfile, true)
	if err != nil {
		pretty.Exit(1, "Error: %v", err)
	}

	ok, err := config.Validate()
	if !ok {
		pretty.Exit(2, "Error: %v", err)
	}

	todo := config.TaskByName(theTask)
	if todo == nil {
		pretty.Exit(3, "Error: Could not resolve what task to run. Select one using --task option.\nAvailable task names are: %v.", strings.Join(config.AvailableTasks(), ", "))
	}

	if config.HasHolozip() && !common.UsesHolotree() {
		pretty.Exit(4, "Error: this robot requires holotree, but no --space was given!")
	}

	pathlib.EnsureDirectoryExists(config.ArtifactDirectory())
	journal.ForRun(filepath.Join(config.ArtifactDirectory(), "journal.run"))
	cache, err := SummonCache()
	if err == nil && len(cache.Userset()) > 1 {
		pretty.Note("There seems to be multiple users sharing %s, which might cause problems.", common.Product.HomeVariable())
		pretty.Note("These are the users: %s.", cache.Userset())
		pretty.Highlight("To correct this problem, make sure that there is only one user per %s.", common.Product.HomeVariable())
		common.RunJournal("sharing", fmt.Sprintf("name=%s from=%s users=%s", theTask, packfile, cache.Userset()), fmt.Sprintf("multiple users shareing %s", common.Product.HomeVariable()))
	}

	common.RunJournal("start task", fmt.Sprintf("name=%s from=%s", theTask, packfile), "at task environment setup")

	if !config.UsesConda() {
		return true, config, todo, ""
	}

	// Create unified dashboard ONCE at the start - it will persist through env build AND robot run
	// The dashboard is created here and will be stopped in ExecuteTask after robot completes
	if pretty.ShouldUseDashboard() {
		_ = pretty.GetOrCreateUnifiedDashboard()
	}

	label, _, err := htfs.NewEnvironment(config.CondaConfigFile(), config.Holozip(), true, force, PullCatalog)

	if err != nil {
		// Stop dashboard on error
		if unified := pretty.GetUnifiedDashboard(); unified != nil {
			unified.Stop(false)
		}
		pretty.RccPointOfView(newEnvironment, err)
		pretty.Exit(4, "Error: %v", err)
	}
	return false, config, todo, label
}

func SelectExecutionModel(runFlags *RunFlags, simple bool, template []string, config robot.Robot, todo robot.Task, label string, interactive bool, extraEnv map[string]string) {
	common.TimelineBegin("robot execution (simple=%v).", simple)
	common.RunJournal("start", "robot", "started")
	defer common.RunJournal("stop", "robot", "done")
	defer common.TimelineEnd()
	pathlib.EnsureDirectoryExists(config.ArtifactDirectory())
	if simple {
		common.RunJournal("select", "robot", "simple run")
		pathlib.NoteDirectoryContent("[Before run] Artifact dir", config.ArtifactDirectory(), true)
		ExecuteSimpleTask(runFlags, template, config, todo, interactive, extraEnv)
	} else {
		common.RunJournal("run", "robot", "task run")
		ExecuteTask(runFlags, template, config, todo, label, interactive, extraEnv)
	}
}

func ExecuteSimpleTask(flags *RunFlags, template []string, config robot.Robot, todo robot.Task, interactive bool, extraEnv map[string]string) {
	common.Debug("Command line is: %v", template)
	task := make([]string, len(template))
	copy(task, template)
	searchPath := pathlib.TargetPath()
	searchPath = searchPath.Prepend(config.Paths()...)
	found, ok := searchPath.Which(task[0], conda.FileExtensions)
	if !ok {
		pretty.Exit(6, "Error: Cannot find command: %v", task[0])
	}
	fullpath, err := filepath.EvalSymlinks(found)
	if err != nil {
		pretty.Exit(7, "Error: %v", err)
	}
	var data Token
	if len(flags.WorkspaceId) > 0 {
		claims := RunRobotClaims(flags.TokenPeriod.RequestSeconds(), flags.WorkspaceId)
		data, err = AuthorizeClaims(flags.AccountName, claims, flags.TokenPeriod.EnforceGracePeriod())
	}
	if err != nil {
		pretty.Exit(8, "Error: %v", err)
	}
	task[0] = fullpath
	directory := config.WorkingDirectory()
	environment := robot.PlainEnvironment([]string{searchPath.AsEnvironmental("PATH")}, true)
	if len(data) > 0 {
		endpoint := data["endpoint"]
		for _, key := range rcHosts {
			environment = append(environment, fmt.Sprintf("%s=%s", key, endpoint))
		}
		token := data["token"]
		for _, key := range rcTokens {
			environment = append(environment, fmt.Sprintf("%s=%s", key, token))
		}
		environment = append(environment, fmt.Sprintf("RC_WORKSPACE_ID=%s", flags.WorkspaceId))
	}
	if extraEnv != nil {
		for key, value := range extraEnv {
			environment = append(environment, fmt.Sprintf("%s=%s", key, value))
		}
	}
	outputDir, err := pathlib.EnsureDirectory(config.ArtifactDirectory())
	if err != nil {
		pretty.Exit(9, "Error: %v", err)
	}
	common.Debug("about to run command - %v", task)
	if common.NoOutputCapture {
		_, err = shell.New(environment, directory, task...).Execute(interactive)
	} else {
		_, err = shell.New(environment, directory, task...).Tee(outputDir, interactive)
	}
	if err != nil {
		pretty.Exit(10, "Error: %v", err)
	}
	pretty.Ok()
}

func findExecutableOrDie(searchPath pathlib.PathParts, executable string) string {
	found, ok := searchPath.Which(executable, conda.FileExtensions)
	if !ok {
		pretty.Exit(6, "Error: Cannot find command: %v", executable)
	}
	fullpath, err := filepath.EvalSymlinks(found)
	if err != nil {
		pretty.Exit(7, "Error: %v", err)
	}
	return fullpath
}

// deriveRobotName extracts a robot name from the robot yaml filename
// Falls back to "Robot" if extraction fails
func deriveRobotName(robotYamlPath string) string {
	if robotYamlPath == "" {
		return "Robot"
	}

	// Get the directory name (typically the robot project directory)
	dir := filepath.Dir(robotYamlPath)
	if dir != "" && dir != "." {
		basename := filepath.Base(dir)
		if basename != "" && basename != "." {
			return basename
		}
	}

	return "Robot"
}

// deriveTaskName extracts a task name from the commandline
// For robot framework, tries to find the .robot file being executed
func deriveTaskName(commandline []string) string {
	if len(commandline) == 0 {
		return "Main Task"
	}

	// Look for .robot files in arguments
	for _, arg := range commandline {
		if strings.HasSuffix(arg, ".robot") {
			// Extract just the filename without path and extension
			base := filepath.Base(arg)
			name := strings.TrimSuffix(base, ".robot")
			if name != "" {
				return name
			}
		}
	}

	// Fall back to the executable name
	executable := filepath.Base(commandline[0])
	if executable != "" {
		return executable
	}

	return "Main Task"
}

func ExecuteTask(flags *RunFlags, template []string, config robot.Robot, todo robot.Task, label string, interactive bool, extraEnv map[string]string) {
	common.Debug("Command line is: %v", template)
	developmentEnvironment, err := robot.LoadEnvironmentSetup(flags.EnvironmentFile)
	if err != nil {
		pretty.Exit(5, "Error: %v", err)
	}
	task := make([]string, len(template))
	copy(task, template)
	searchPath := config.SearchPath(label)
	task[0] = findExecutableOrDie(searchPath, task[0])
	var data Token
	if !flags.Assistant && len(flags.WorkspaceId) > 0 {
		claims := RunRobotClaims(flags.TokenPeriod.RequestSeconds(), flags.WorkspaceId)
		data, err = AuthorizeClaims(flags.AccountName, claims, nil)
	}
	if err != nil {
		pretty.Exit(8, "Error: %v", err)
	}
	directory := config.WorkingDirectory()
	environment := config.RobotExecutionEnvironment(label, developmentEnvironment.AsEnvironment(), true)
	if len(data) > 0 {
		endpoint := data["endpoint"]
		for _, key := range rcHosts {
			environment = append(environment, fmt.Sprintf("%s=%s", key, endpoint))
		}
		token := data["token"]
		for _, key := range rcTokens {
			environment = append(environment, fmt.Sprintf("%s=%s", key, token))
		}
		environment = append(environment, fmt.Sprintf("RC_WORKSPACE_ID=%s", flags.WorkspaceId))
	}
	if extraEnv != nil {
		for key, value := range extraEnv {
			environment = append(environment, fmt.Sprintf("%s=%s", key, value))
		}
	}
	before := make(map[string]string)
	beforeHash, beforeErr := conda.DigestFor(label, before)
	outputDir, err := pathlib.EnsureDirectory(config.ArtifactDirectory())
	if err != nil {
		pretty.Exit(9, "Error: %v", err)
	}

	// Get or create dashboard for robot execution
	// The unified dashboard was created in LoadTaskWithEnvironment before env build
	var dashboard pretty.Dashboard
	var stdoutWriter, stderrWriter io.Writer

	// Use dashboard when terminal is interactive (has TTY) and output capture is enabled
	if pretty.ShouldUseDashboard() && !common.NoOutputCapture {
		// Derive robot and task names from the configuration
		robotName := deriveRobotName(flags.RobotYaml)
		taskName := deriveTaskName(task)

		common.Debug("Setting up dashboard for robot=%s, task=%s", robotName, taskName)

		// Use existing unified dashboard (created in LoadTaskWithEnvironment)
		if unified := pretty.GetUnifiedDashboard(); unified != nil {
			dashboard = unified
			// Transition to robot phase - the dashboard is already running
			unified.TransitionToRobotPhase(robotName, taskName)
			// Set additional robot context info
			host, _ := os.Hostname()
			if unified.GetModel() != nil {
				unified.GetModel().RobotState.Hostname = host
				unified.GetModel().RobotState.Controller = common.ControllerIdentity()
				unified.GetModel().RobotState.Workers = int(anywork.Scale())
			}
		} else {
			// No unified dashboard - create a standalone robot dashboard
			dashboard = pretty.NewRobotRunDashboard(robotName)
			if dashboard != nil {
				dashboard.Start()
				dashboard.SetStep(0, pretty.StepRunning, taskName)

				// Feed environment and context info to standalone dashboard
				if teaDash, ok := dashboard.(*pretty.TeaRobotDashboard); ok {
					teaDash.SetEnvironmentInfo(common.EnvironmentHash, directory, label)
					who, _ := user.Current()
					host, _ := os.Hostname()
					contextName := fmt.Sprintf("%s@%s", who.Username, host)
					teaDash.SetContextInfo(contextName, common.Platform(), int(anywork.Scale()), runtime.NumCPU())
				}
			}
		}

		// Create dashboard writers that feed output to the dashboard
		if dashboard != nil {
			stdoutWriter = newDashboardWriter(nil, dashboard)
			stderrWriter = newDashboardWriter(nil, dashboard)
		}
	}

	// Ensure dashboard is stopped on exit - this is the ONLY place the unified dashboard gets stopped
	defer func() {
		if dashboard != nil {
			dashboard.Stop(err == nil)
		}
	}()

	if !flags.NoPipFreeze && !flags.Assistant && !common.Silent() && !interactive {
		wantedfile, _ := config.DependenciesFile()
		ExecutionEnvironmentListing(wantedfile, label, searchPath, directory, outputDir, environment)
	}

	pathlib.NoteDirectoryContent("[Before run] Artifact dir", config.ArtifactDirectory(), true)

	FreezeEnvironmentListing(label, config)
	preRunScripts := config.PreRunScripts()
	if !common.DeveloperFlag && preRunScripts != nil && len(preRunScripts) > 0 {
		common.Timeline("pre run scripts started")
		common.Debug("===  pre run script phase ===")
		for _, script := range preRunScripts {
			if !robot.PlatformAcceptableFile(runtime.GOARCH, runtime.GOOS, script) {
				continue
			}
			scriptCommand, err := shell.Split(script)
			if err != nil {
				pretty.RccPointOfView(preRun, err)
				pretty.Exit(11, "%sScript '%s' parsing failure: %v%s", pretty.Red, script, err, pretty.Reset)
			}
			scriptCommand[0] = findExecutableOrDie(searchPath, scriptCommand[0])
			common.Debug("Running pre run script '%s' ...", script)
			_, err = shell.New(environment, directory, scriptCommand...).Execute(interactive)
			if err != nil {
				pretty.RccPointOfView(preRun, err)
				pretty.Exit(12, "%sScript '%s' failure: %v%s", pretty.Red, script, err, pretty.Reset)
			}
		}
		journal.CurrentBuildEvent().PreRunComplete()
		common.Timeline("pre run scripts completed")
	}

	common.Debug("about to run command - %v", task)
	journal.CurrentBuildEvent().RobotStarts()

	pipe := WatchChildren(os.Getpid(), 550*time.Millisecond)
	shell.WithInterrupt(func() {
		exitcode := 0
		if common.NoOutputCapture {
			exitcode, err = shell.New(environment, directory, task...).Execute(interactive)
		} else {
			// Use TeeWithSink to integrate dashboard while maintaining file logging
			if dashboard != nil {
				exitcode, err = shell.New(environment, directory, task...).TeeWithSink(outputDir, stdoutWriter, stderrWriter, interactive)
			} else {
				exitcode, err = shell.New(environment, directory, task...).Tee(outputDir, interactive)
			}
		}
		if exitcode != 0 {
			details := fmt.Sprintf("%s_%d_%08x", common.Platform(), exitcode, uint32(exitcode))
			cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.cli.run.failure", details)
		}
	})

	// Stop dashboard IMMEDIATELY after robot execution completes
	// This ensures clean transition before post-run output (RCC point of view, etc.)
	if dashboard != nil {
		dashboard.Stop(err == nil)
		dashboard = nil // Prevent double-stop in defer
	}

	pretty.RccPointOfView(actualRun, err)
	seen, ok := <-pipe
	suberr := SubprocessWarning(seen, ok)
	if suberr != nil {
		pretty.Warning("Problem with subprocess warnings, reason: %v", suberr)
	}
	journal.CurrentBuildEvent().RobotEnds()
	after := make(map[string]string)
	afterHash, afterErr := conda.DigestFor(label, after)
	conda.DiagnoseDirty(label, label, beforeHash, afterHash, beforeErr, afterErr, before, after, true)
	if err != nil {
		pretty.Exit(10, "Error: %v (robot run exit)", err)
	}
	pretty.Ok()
}
