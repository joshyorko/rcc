package interactive

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joshyorko/rcc/operations"
	"github.com/joshyorko/rcc/robot"
	"github.com/joshyorko/rcc/wizard"
)

// toastExpiredMsg is sent when a toast should be removed
type toastExpiredMsg struct{}

type robotData struct {
	name      string
	path      string
	directory string
	tasks     []string
	envFiles  []string
}

// View modes
const (
	modeList   = 0
	modeDetail = 1
)

// Focus states for list mode (simplified - no task/env on list view)
const (
	focusActions   = 0 // Create/pull actions
	focusRobotList = 1 // Robot list
)

// Action options
const (
	actionPullRobot         = 0 // rcc pull <url>
	actionTemplateBasic     = 1 // 01-python
	actionTemplateBrowser   = 2 // 02-python-browser
	actionTemplateWorkitems = 3 // 03-python-workitems
	actionTemplateAI        = 4 // 04-python-assistant-ai
)

// Focus states for detail mode - RUN first, config last
const (
	focusDetailTask   = 0 // Task selection (primary)
	focusDetailEnv    = 1 // Env selection
	focusDetailConfig = 2 // Config actions (edit, rebuild)
)

// Config action options in detail mode
const (
	configEditRobot  = 0
	configEditConda  = 1
	configRebuildEnv = 2
)

// pullRobotMsg is sent when a pull operation completes
type pullRobotMsg struct {
	success bool
	message string
	path    string
}

// EnvStatus represents the environment build status
type EnvStatus int

const (
	EnvStatusUnknown EnvStatus = iota
	EnvStatusReady
	EnvStatusNeedsRebuild
	EnvStatusNotBuilt
)

type RobotsView struct {
	styles  *Styles
	robots  []robotData
	width   int
	height  int
	loading bool
	err     error

	// View mode
	mode int // 0=list, 1=detail

	// List mode navigation
	actionIdx int // Which action is selected (pull/create)
	robotIdx  int // Which robot is selected
	focus     int // 0=actions, 1=robot list, 2=task row, 3=env row
	taskIdx   int // Which task is selected
	envIdx    int // Which env is selected (-1 = none)

	// Detail mode state
	detailFocus int // 0=task, 1=env, 2=config
	configIdx   int // Which config action

	// Environment info (for detail mode)
	envStatus    EnvStatus
	envLastBuilt time.Time
	condaHash    string
	condaModTime time.Time

	// Robot yaml parsed info (for detail mode)
	condaFile    string
	pythonVer    string
	dependencies []string

	// Full parsed data for dashboard
	taskCommands  map[string]string // task name -> command
	condaChannels []string
	condaDeps     []string
	pipDeps       []string
	artifactsDir  string
	preRunScripts []string

	// Input mode for pull
	inputMode   bool
	inputBuffer string
	inputStep   int    // 0=URL, 1=directory
	pullURL     string // Store URL while getting directory

	// Editing mode (blocks all updates)
	editing bool

	// Spinner for async operations
	spinner  spinner.Model
	spinning bool

	// State

	// Creation Wizard State
	creating    bool
	createStep  int // 0=Name, 1=Template
	createName  string
	templates   []wizard.Template
	tmplListIdx int
}

func NewRobotsView(styles *Styles) *RobotsView {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(styles.theme.Accent)

	return &RobotsView{
		styles:      styles,
		spinner:     s,
		robots:      []robotData{},
		width:       120,
		height:      30,
		loading:     true,
		mode:        modeList,
		actionIdx:   0,
		robotIdx:    0,
		focus:       focusActions,
		taskIdx:     0,
		envIdx:      -1,
		detailFocus: focusDetailTask,
		configIdx:   configEditRobot,
		envStatus:   EnvStatusUnknown,
	}
}

func (v *RobotsView) Init() tea.Cmd {
	return v.scanForRobots
}

// addToast adds a toast notification using the global system
func (v *RobotsView) addToast(message string, tType ToastType) tea.Cmd {
	return ShowToast(message, tType)
}

type robotsLoadedMsg struct {
	robots []robotData
	err    error
}

func (v *RobotsView) scanForRobots() tea.Msg {
	// Add a small delay to make the refresh perceptible
	time.Sleep(500 * time.Millisecond)
	cwd, err := os.Getwd()
	if err != nil {
		return robotsLoadedMsg{err: err}
	}

	var robots []robotData
	maxDepth := 3
	baseDepth := strings.Count(cwd, string(os.PathSeparator))

	filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		currentDepth := strings.Count(path, string(os.PathSeparator)) - baseDepth
		if currentDepth > maxDepth && info.IsDir() {
			return filepath.SkipDir
		}

		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "__pycache__" || name == "venv" || name == ".venv" || name == "build" || name == "tmp" {
				return filepath.SkipDir
			}
		}

		if info.Name() == "robot.yaml" {
			item := robotData{
				path:      path,
				directory: filepath.Dir(path),
				name:      filepath.Base(filepath.Dir(path)),
			}

			if r, err := robot.LoadRobotYaml(path, false); err == nil {
				if tasks := r.AvailableTasks(); len(tasks) > 0 {
					item.tasks = tasks
				}
			}

			item.envFiles = findEnvFiles(item.directory)
			robots = append(robots, item)
		}

		return nil
	})

	return robotsLoadedMsg{robots: robots}
}

func findEnvFiles(robotDir string) []string {
	var envFiles []string

	patterns := []string{
		"env.json",
		"devdata/env.json",
		"devdata/*.json",
		"env/*.json",
		"environments/*.json",
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(robotDir, pattern))
		if err == nil {
			for _, match := range matches {
				relPath, _ := filepath.Rel(robotDir, match)
				envFiles = append(envFiles, relPath)
			}
		}
	}

	return envFiles
}

// Detail mode messages
type robotDetailLoadedMsg struct {
	condaFile    string
	condaModTime time.Time
	condaHash    string
	pythonVer    string
	dependencies []string
	envStatus    EnvStatus
	envLastBuilt time.Time

	// Full parsed data
	taskCommands  map[string]string
	condaChannels []string
	condaDeps     []string
	pipDeps       []string
	artifactsDir  string
	preRunScripts []string
}
type envRebuildMsg struct {
	success bool
	message string
}
type editFileMsg struct {
	file    string
	changed bool
}

func (v *RobotsView) loadRobotDetails() tea.Msg {
	r := v.selectedRobot()
	if r == nil {
		return robotDetailLoadedMsg{}
	}

	result := robotDetailLoadedMsg{
		taskCommands: make(map[string]string),
	}

	// Parse robot.yaml for full details
	if robotYaml, err := robot.LoadRobotYaml(r.path, false); err == nil {
		result.condaFile = robotYaml.CondaConfigFile()
		result.artifactsDir = robotYaml.ArtifactDirectory()
		result.preRunScripts = robotYaml.PreRunScripts()

		// Get task commands
		for _, taskName := range robotYaml.AvailableTasks() {
			if task := robotYaml.TaskByName(taskName); task != nil {
				cmdLine := task.Commandline()
				if len(cmdLine) > 0 {
					result.taskCommands[taskName] = strings.Join(cmdLine, " ")
				}
			}
		}
	}

	// Load conda.yaml info - condaFile is already absolute, or use default
	condaPath := result.condaFile
	if condaPath == "" {
		condaPath = filepath.Join(r.directory, "conda.yaml")
	}

	if info, err := os.Stat(condaPath); err == nil {
		result.condaModTime = info.ModTime()

		// Read and parse conda.yaml fully
		if data, err := os.ReadFile(condaPath); err == nil {
			hash := sha256.Sum256(data)
			result.condaHash = hex.EncodeToString(hash[:8])

			// Parse for full info
			result.pythonVer, result.condaChannels, result.condaDeps, result.pipDeps = parseCondaYaml(string(data))
			// Keep dependencies for backwards compat
			result.dependencies = append(result.condaDeps, result.pipDeps...)
		}
	}

	// Check environment status
	rccDir := filepath.Join(r.directory, ".rcc")
	if info, err := os.Stat(rccDir); err == nil {
		result.envStatus = EnvStatusReady
		result.envLastBuilt = info.ModTime()
		// If conda.yaml was modified after last build, needs rebuild
		if result.condaModTime.After(result.envLastBuilt) {
			result.envStatus = EnvStatusNeedsRebuild
		}
	} else {
		result.envStatus = EnvStatusUnknown
	}

	return result
}

// parseCondaYaml extracts all relevant info from conda.yaml
func parseCondaYaml(content string) (pythonVer string, channels, condaDeps, pipDeps []string) {
	lines := strings.Split(content, "\n")
	inChannels := false
	inDeps := false
	inPip := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect sections
		if strings.HasPrefix(trimmed, "channels:") {
			inChannels = true
			inDeps = false
			inPip = false
			continue
		}
		if strings.HasPrefix(trimmed, "dependencies:") {
			inChannels = false
			inDeps = true
			inPip = false
			continue
		}

		// Parse channels
		if inChannels && strings.HasPrefix(trimmed, "- ") {
			channel := strings.TrimPrefix(trimmed, "- ")
			channels = append(channels, channel)
		}

		// Parse dependencies
		if inDeps && strings.HasPrefix(trimmed, "- ") {
			dep := strings.TrimPrefix(trimmed, "- ")

			if dep == "pip:" {
				inPip = true
				continue
			}

			if strings.HasPrefix(dep, "python") {
				// Extract python version
				if parts := strings.Split(dep, "="); len(parts) > 1 {
					pythonVer = strings.TrimSpace(parts[len(parts)-1])
				} else if parts := strings.Split(dep, ">="); len(parts) > 1 {
					pythonVer = ">=" + strings.TrimSpace(parts[1])
				} else if parts := strings.Split(dep, "<"); len(parts) > 1 {
					pythonVer = "<" + strings.TrimSpace(parts[1])
				}
			} else if !inPip && dep != "pip" {
				condaDeps = append(condaDeps, dep)
			}
		}

		// Parse pip dependencies (indented under pip:)
		if inPip && strings.HasPrefix(trimmed, "- ") {
			dep := strings.TrimPrefix(trimmed, "- ")
			pipDeps = append(pipDeps, dep)
		}

		// Stop at next top-level section
		if inDeps && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && trimmed != "" && !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "#") {
			break
		}
	}

	return
}

func parseDependencies(condaContent string) (pythonVer string, dependencies []string) {
	lines := strings.Split(condaContent, "\n")
	inDeps := false
	inPip := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "dependencies:") {
			inDeps = true
			continue
		}

		if inDeps && strings.HasPrefix(trimmed, "- ") {
			dep := strings.TrimPrefix(trimmed, "- ")

			if strings.HasPrefix(dep, "python") {
				// Extract python version
				if parts := strings.Split(dep, "="); len(parts) > 1 {
					pythonVer = strings.TrimSpace(parts[1])
				} else if parts := strings.Split(dep, ">="); len(parts) > 1 {
					pythonVer = ">=" + strings.TrimSpace(parts[1])
				}
			} else if dep == "pip:" {
				inPip = true
			} else if !inPip && !strings.HasPrefix(dep, "pip") {
				// Add conda dependency
				if len(dependencies) < 5 {
					dependencies = append(dependencies, dep)
				}
			}
		}

		if inPip && strings.HasPrefix(trimmed, "- ") {
			dep := strings.TrimPrefix(trimmed, "- ")
			if len(dependencies) < 5 && !strings.Contains(dep, "==") {
				dependencies = append(dependencies, dep)
			}
		}

		// Stop at next section
		if inDeps && !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "#") && trimmed != "" && !strings.HasPrefix(trimmed, "pip:") {
			break
		}
	}
	return
}

func (v *RobotsView) checkEnvStatus() {
	r := v.selectedRobot()
	if r == nil {
		return
	}

	// Check if there's a .rcc directory or holotree space
	rccDir := filepath.Join(r.directory, ".rcc")
	if _, err := os.Stat(rccDir); err == nil {
		v.envStatus = EnvStatusReady
		if info, err := os.Stat(rccDir); err == nil {
			v.envLastBuilt = info.ModTime()
		}
	} else {
		v.envStatus = EnvStatusUnknown
	}

	// If conda.yaml was modified after last build, needs rebuild
	if v.envStatus == EnvStatusReady && v.condaModTime.After(v.envLastBuilt) {
		v.envStatus = EnvStatusNeedsRebuild
	}
}

func (v *RobotsView) Update(msg tea.Msg) (View, tea.Cmd) {
	// Block all updates while editing
	if v.editing {
		if editMsg, ok := msg.(editFileMsg); ok {
			v.editing = false
			if editMsg.changed {

				return v, tea.Batch(
					v.addToast(fmt.Sprintf("Saved %s", filepath.Base(editMsg.file)), ToastInfo),
					v.loadRobotDetails,
				)
			}
			return v, v.loadRobotDetails
		}
		return v, nil
	}

	// Handle creation wizard
	if v.creating {
		return v.handleCreateKeys(msg)
	}

	// Handle input mode (pull)
	if v.inputMode {
		return v.handleInputMode(msg)
	}

	switch msg := msg.(type) {
	case robotsLoadedMsg:
		v.loading = false
		v.err = msg.err
		v.robots = msg.robots
		v.actionIdx = 0
		v.robotIdx = 0
		v.focus = focusActions
		v.taskIdx = 0
		v.envIdx = -1
		v.mode = modeList
		// Signal refresh complete if not initial load (hacky check: loading was true)
		return v, ShowInfoToast(fmt.Sprintf("Loaded %d robots", len(v.robots)))

	case robotDetailLoadedMsg:
		v.loading = false
		v.condaFile = msg.condaFile
		v.condaModTime = msg.condaModTime
		v.condaHash = msg.condaHash
		v.pythonVer = msg.pythonVer
		v.dependencies = msg.dependencies
		v.envStatus = msg.envStatus
		v.envLastBuilt = msg.envLastBuilt
		v.taskCommands = msg.taskCommands
		v.condaChannels = msg.condaChannels
		v.condaDeps = msg.condaDeps
		v.pipDeps = msg.pipDeps
		v.artifactsDir = msg.artifactsDir
		v.preRunScripts = msg.preRunScripts

	case envRebuildMsg:
		v.loading = false
		v.spinning = false
		var cmd tea.Cmd
		if msg.success {
			v.envStatus = EnvStatusReady
			v.envLastBuilt = time.Now()
			cmd = ShowSuccessToast("Environment rebuilt successfully")
		} else {
			cmd = ShowErrorToast("Rebuild failed: " + msg.message)
		}
		return v, cmd

	case pullRobotMsg:
		v.spinning = false
		var cmd tea.Cmd
		if msg.success {
			cmd = ShowSuccessToast("Robot pulled successfully!")
			// Refresh robot list to show the new robot
			return v, tea.Batch(cmd, v.scanForRobots)
		} else {
			// Truncate error message if too long
			errMsg := msg.message
			if len(errMsg) > 60 {
				errMsg = errMsg[:57] + "..."
			}
			cmd = ShowErrorToast("Pull failed: " + errMsg)
		}
		return v, cmd

	case spinner.TickMsg:
		if v.spinning {
			var cmd tea.Cmd
			v.spinner, cmd = v.spinner.Update(msg)
			return v, cmd
		}

	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height

	case tea.KeyMsg:
		if v.mode == modeList {
			return v.handleListKeys(msg)
		} else {
			return v.handleDetailKeys(msg)
		}

	case viewChangedMsg:
		if msg.to == ViewRobots {
			v.loading = true
			v.mode = modeList
			return v, v.scanForRobots
		}

	case templatesLoadedMsg:
		v.templates = msg.templates
	}

	return v, nil
}

func (v *RobotsView) handleListKeys(key interface{}) (View, tea.Cmd) {
	kMsg, ok := key.(tea.KeyMsg)
	if !ok {
		return v, nil
	}
	switch kMsg.String() {
	case "up", "k":
		v.moveUpList()
	case "down", "j":
		v.moveDownList()
	case "left", "h":
		v.moveLeftList()
	case "right", "l":
		v.moveRightList()
	case "r", "R":
		v.loading = true
		return v, v.scanForRobots
	case "esc":
		v.focus = focusActions
	case "enter":
		// Handle action based on focus
		if v.focus == focusActions {
			return v.handleListAction()
		}
		// Enter detail mode for selected robot
		if r := v.selectedRobot(); r != nil {
			v.mode = modeDetail
			v.detailFocus = focusDetailTask // Start on RUN, not config
			v.configIdx = configEditRobot
			return v, v.loadRobotDetails
		}
	}
	return v, nil
}

func (v *RobotsView) handleListAction() (View, tea.Cmd) {
	// Action 0 is Pull
	if v.actionIdx == 0 {
		v.inputMode = true
		v.inputBuffer = ""
		return v, nil
	}

	// Action 1 is Create New (replaces old template actions)
	if v.actionIdx == 1 {
		v.creating = true
		v.createStep = 0
		v.createName = ""
		v.tmplListIdx = 0
		v.templates = nil
		return v, func() tea.Msg {
			// Load templates in background
			tmpls, err := wizard.LoadTemplates(false)
			if err != nil {
				return nil
			}
			return templatesLoadedMsg{templates: tmpls}
		}
	}

	return v, nil
}

type templatesLoadedMsg struct {
	templates []wizard.Template
}

func (v *RobotsView) handleInputMode(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			v.inputMode = false
			v.inputBuffer = ""
			v.inputStep = 0
			v.pullURL = ""
		case "enter":
			if v.inputStep == 0 {
				// Step 0: URL entered, move to directory
				if v.inputBuffer != "" {
					v.pullURL = v.inputBuffer
					v.inputStep = 1
					// Default to repo name extracted from URL
					v.inputBuffer = extractRepoName(v.pullURL)
				}
			} else {
				// Step 1: Directory entered, execute pull
				if v.inputBuffer != "" {
					url := v.pullURL
					dir := v.inputBuffer
					v.inputMode = false
					v.inputBuffer = ""
					v.inputStep = 0
					v.pullURL = ""
					v.spinning = true
					return v, tea.Batch(v.spinner.Tick, v.pullRobot(url, dir))
				}
			}
		case "backspace":
			if len(v.inputBuffer) > 0 {
				v.inputBuffer = v.inputBuffer[:len(v.inputBuffer)-1]
			}
		default:
			if len(msg.String()) == 1 {
				v.inputBuffer += msg.String()
			}
		}
	}
	return v, nil
}

// extractRepoName extracts the repository name from a git URL
func extractRepoName(url string) string {
	// Remove trailing .git
	url = strings.TrimSuffix(url, ".git")
	// Get last path segment
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		name := parts[len(parts)-1]
		// Handle git@github.com:user/repo format
		if colonIdx := strings.LastIndex(name, ":"); colonIdx != -1 {
			name = name[colonIdx+1:]
		}
		if name != "" {
			return name
		}
	}
	return "robot"
}

func (v *RobotsView) pullRobot(url, directory string) tea.Cmd {
	return func() tea.Msg {
		rccPath, err := os.Executable()
		if err != nil {
			return pullRobotMsg{success: false, message: err.Error()}
		}

		// rcc pull <url> -d <directory>
		cmd := exec.Command(rccPath, "pull", url, "-d", directory)
		// cmd.Dir = directory -- DO NOT SET executing directory to the target, it doesn't exist yet!
		output, err := cmd.CombinedOutput()
		if err != nil {
			errMsg := string(output)
			if errMsg == "" {
				errMsg = err.Error()
			}
			return pullRobotMsg{success: false, message: errMsg}
		}

		return pullRobotMsg{success: true, message: "Robot pulled to " + directory, path: directory}
	}
}

func (v *RobotsView) handleDetailKeys(msg tea.KeyMsg) (View, tea.Cmd) {
	r := v.selectedRobot()

	switch msg.String() {
	case "esc":
		// Return to list mode
		v.mode = modeList
		return v, nil

	case "tab":
		// Cycle between panels: Task -> Env -> Task
		if v.detailFocus == focusDetailTask {
			if len(r.envFiles) > 0 {
				v.detailFocus = focusDetailEnv
				if v.envIdx < 0 {
					v.envIdx = 0
				}
			}
		} else {
			v.detailFocus = focusDetailTask
		}
		return v, nil

	case "up", "k":
		v.moveUpDetail()
	case "down", "j":
		v.moveDownDetail()
	case "left", "h":
		v.moveLeftDetail()
	case "right", "l":
		v.moveRightDetail()

	case "c":
		// Copy command to clipboard
		if r != nil {
			cmd := v.buildCommandPreview(r)
			if err := copyToClipboard(cmd); err == nil {
				return v, v.addToast("Command copied to clipboard", ToastSuccess)
			} else {
				return v, v.addToast("Failed to copy: "+err.Error(), ToastError)
			}
		}

	case "e":
		// Edit robot.yaml
		if r != nil {
			return v, v.editFile(r.path)
		}

	case "E":
		// Edit conda.yaml
		if r != nil {
			condaPath := v.condaFile
			if condaPath == "" {
				condaPath = filepath.Join(r.directory, "conda.yaml")
			}
			return v, v.editFile(condaPath)
		}

	case "r":
		// Rebuild environment
		v.spinning = true
		return v, tea.Batch(v.spinner.Tick, v.rebuildEnvironment)

	case "R":
		// Refresh
		v.loading = true
		return v, v.loadRobotDetails

	case "enter":
		// Run the robot
		return v, v.runRobot()
	}
	return v, nil
}

// copyToClipboard copies text to system clipboard
func copyToClipboard(text string) error {
	// Try xclip first (Linux)
	cmd := exec.Command("xclip", "-selection", "clipboard")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Try xsel (Linux)
	cmd = exec.Command("xsel", "--clipboard", "--input")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Try wl-copy (Wayland)
	cmd = exec.Command("wl-copy")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Try pbcopy (macOS)
	cmd = exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err == nil {
		return nil
	}

	return fmt.Errorf("no clipboard utility found")
}

// List mode navigation (simplified - just actions and robot list)
func (v *RobotsView) moveUpList() {
	switch v.focus {
	case focusActions:
		if v.actionIdx > 0 {
			v.actionIdx--
		}
	case focusRobotList:
		if v.robotIdx > 0 {
			v.robotIdx--
			v.resetSelections()
		} else {
			// At top of robot list, move to actions
			v.focus = focusActions
			v.actionIdx = 1 // Last action
		}
	}
}

func (v *RobotsView) moveDownList() {
	switch v.focus {
	case focusActions:
		if v.actionIdx < 1 { // Only 2 actions (0 and 1)
			v.actionIdx++
		} else if len(v.robots) > 0 {
			// Move to robot list
			v.focus = focusRobotList
		}
	case focusRobotList:
		if v.robotIdx < len(v.robots)-1 {
			v.robotIdx++
			v.resetSelections()
		}
		// No more sections below robot list
	}
}

func (v *RobotsView) moveLeftList() {
	// Nothing to cycle in list mode
}

func (v *RobotsView) moveRightList() {
	// Nothing to cycle in list mode
}

// Detail mode navigation - order is Task -> Env -> Config (top to bottom)
func (v *RobotsView) moveUpDetail() {
	r := v.selectedRobot()
	if r == nil {
		return
	}

	switch v.detailFocus {
	case focusDetailTask:
		// Already at top, do nothing
	case focusDetailEnv:
		v.detailFocus = focusDetailTask
	case focusDetailConfig:
		if v.configIdx > 0 {
			v.configIdx--
		} else if len(r.envFiles) > 0 {
			v.detailFocus = focusDetailEnv
		} else {
			v.detailFocus = focusDetailTask
		}
	}
}

func (v *RobotsView) moveDownDetail() {
	r := v.selectedRobot()
	if r == nil {
		return
	}

	switch v.detailFocus {
	case focusDetailTask:
		if len(r.envFiles) > 0 {
			v.detailFocus = focusDetailEnv
		} else {
			v.detailFocus = focusDetailConfig
			v.configIdx = 0
		}
	case focusDetailEnv:
		v.detailFocus = focusDetailConfig
		v.configIdx = 0
	case focusDetailConfig:
		if v.configIdx < configRebuildEnv {
			v.configIdx++
		}
		// Already at bottom
	}
}

func (v *RobotsView) moveLeftDetail() {
	r := v.selectedRobot()
	if r == nil {
		return
	}

	switch v.detailFocus {
	case focusDetailTask:
		if len(r.tasks) > 0 {
			v.taskIdx--
			if v.taskIdx < 0 {
				v.taskIdx = len(r.tasks) - 1
			}
		}
	case focusDetailEnv:
		v.envIdx--
		if v.envIdx < -1 {
			v.envIdx = len(r.envFiles) - 1
		}
	}
}

func (v *RobotsView) moveRightDetail() {
	r := v.selectedRobot()
	if r == nil {
		return
	}

	switch v.detailFocus {
	case focusDetailTask:
		if len(r.tasks) > 0 {
			v.taskIdx = (v.taskIdx + 1) % len(r.tasks)
		}
	case focusDetailEnv:
		v.envIdx++
		if v.envIdx >= len(r.envFiles) {
			v.envIdx = -1
		}
	}
}

func (v *RobotsView) resetSelections() {
	v.taskIdx = 0
	v.envIdx = -1
}

func (v *RobotsView) selectedRobot() *robotData {
	if v.robotIdx >= 0 && v.robotIdx < len(v.robots) {
		return &v.robots[v.robotIdx]
	}
	return nil
}

func (v *RobotsView) handleConfigAction() tea.Cmd {
	r := v.selectedRobot()
	if r == nil {
		return nil
	}

	switch v.configIdx {
	case configEditRobot:
		return v.editFile(r.path)
	case configEditConda:
		// v.condaFile from CondaConfigFile() is already an absolute path
		// If empty, fall back to conda.yaml in robot directory
		condaPath := v.condaFile
		if condaPath == "" {
			condaPath = filepath.Join(r.directory, "conda.yaml")
		}
		return v.editFile(condaPath)
	case configRebuildEnv:
		v.loading = true
		v.spinning = true
		return tea.Batch(v.spinner.Tick, v.rebuildEnvironment)
	}
	return nil
}

func (v *RobotsView) editFile(filePath string) tea.Cmd {
	// Set editing state to block all other updates
	v.editing = true

	// Use vim for editing - ignore EDITOR env var since TUI users expect vim
	editor := "vim"

	// Store mtime before edit
	var beforeMtime time.Time
	if info, err := os.Stat(filePath); err == nil {
		beforeMtime = info.ModTime()
	}

	// Use tea.ExecProcess to properly suspend TUI while editing
	cmd := exec.Command(editor, filePath)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		// Check if file changed after edit
		changed := false
		if info, err := os.Stat(filePath); err == nil {
			changed = info.ModTime().After(beforeMtime)
		}
		return editFileMsg{file: filePath, changed: changed}
	})
}

func (v *RobotsView) rebuildEnvironment() tea.Msg {
	r := v.selectedRobot()
	if r == nil {
		return envRebuildMsg{success: false, message: "No robot selected"}
	}

	// Run rcc holotree variables to rebuild
	rccPath, err := os.Executable()
	if err != nil {
		return envRebuildMsg{success: false, message: err.Error()}
	}

	cmd := exec.Command(rccPath, "holotree", "variables", "-r", r.path, "--space", "ui")
	cmd.Dir = r.directory
	output, err := cmd.CombinedOutput()
	if err != nil {
		return envRebuildMsg{success: false, message: string(output)}
	}

	return envRebuildMsg{success: true, message: "Environment ready"}
}

func (v *RobotsView) runRobot() tea.Cmd {
	r := v.selectedRobot()
	if r == nil {
		return nil
	}

	action := ActionResult{
		Type:      ActionRunRobot,
		RobotPath: r.path,
	}

	if len(r.tasks) > 0 && v.taskIdx < len(r.tasks) {
		action.RobotTask = r.tasks[v.taskIdx]
	}

	if v.envIdx >= 0 && v.envIdx < len(r.envFiles) {
		action.EnvFile = filepath.Join(r.directory, r.envFiles[v.envIdx])
	}

	return func() tea.Msg { return actionMsg{action: action} }
}

func (v *RobotsView) View() string {
	if v.creating {
		return v.renderCreateView()
	}
	if v.inputMode {
		return v.renderInputMode()
	}
	if v.mode == modeDetail {
		return v.renderDetailView()
	}
	return v.renderListView()
}

func (v *RobotsView) renderListView() string {
	theme := v.styles.theme
	vs := NewViewStyles(theme)

	boxWidth := v.width - 8
	if boxWidth < 60 {
		boxWidth = 60
	}
	contentWidth := boxWidth - 6

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 2).
		Width(boxWidth)

	var b strings.Builder

	// Header
	subtitle := ""
	if !v.loading && len(v.robots) > 0 {
		subtitle = fmt.Sprintf("(%d found)", len(v.robots))
	}
	b.WriteString(RenderHeader(vs, "Robots", subtitle, contentWidth))
	b.WriteString("\n")

	// Loading state
	if v.loading {
		b.WriteString(vs.Subtext.Render("Scanning for robots..."))
		return v.placeBox(boxStyle.Render(b.String()))
	}

	// Error state
	if v.err != nil {
		b.WriteString(vs.Error.Render("Error: " + v.err.Error()))
		return v.placeBox(boxStyle.Render(b.String()))
	}

	// Actions section
	b.WriteString(vs.Accent.Bold(true).Render("ACTIONS"))
	b.WriteString("\n\n")

	actions := []struct {
		label string
		desc  string
	}{
		{"Pull Robot from Git", "Clone robot from any git repository"},
		{"Create New Robot", "Create a new robot from a template"},
	}

	for i, action := range actions {
		prefix := "  "
		style := vs.Normal
		if v.focus == focusActions && i == v.actionIdx {
			prefix = "> "
			style = vs.Selected
		}
		b.WriteString(style.Render(prefix + action.label))
		b.WriteString("\n")
		if v.focus == focusActions && i == v.actionIdx {
			b.WriteString("    ")
			b.WriteString(vs.Subtext.Render(action.desc))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(vs.Separator.Render(strings.Repeat("-", contentWidth)))
	b.WriteString("\n\n")

	// Empty state
	if len(v.robots) == 0 {
		b.WriteString(vs.Subtext.Render("No robots found in current directory"))
		b.WriteString("\n\n")
		b.WriteString(vs.Subtext.Render("Select an action above to get started"))
		b.WriteString("\n")

		// Footer
		b.WriteString("\n")
		hints := []KeyHint{
			{"Enter", "select"},
			{"Arrows", "navigate"},
			{"R", "refresh"},
		}
		b.WriteString(RenderFooter(vs, hints, contentWidth))
		return v.placeBox(boxStyle.Render(b.String()))
	}

	// Robot list section
	b.WriteString(vs.Accent.Bold(true).Render("ROBOTS"))
	b.WriteString("\n\n")
	maxVisible := 6
	startIdx := 0
	if v.robotIdx >= maxVisible {
		startIdx = v.robotIdx - maxVisible + 1
	}

	for i := startIdx; i < len(v.robots) && i < startIdx+maxVisible; i++ {
		r := v.robots[i]
		isSelected := i == v.robotIdx

		// Build display name
		name := r.name
		if len(name) > 20 {
			name = name[:17] + "..."
		}

		// Relative path
		relPath, _ := filepath.Rel(".", r.directory)
		if relPath == "" || relPath == "." {
			relPath = "./"
		} else {
			relPath = "./" + relPath
		}
		if len(relPath) > 30 {
			relPath = relPath[:27] + "..."
		}

		// Format line with padding
		line := fmt.Sprintf("%-21s %s", name, relPath)

		if isSelected && v.focus == focusRobotList {
			b.WriteString(vs.Selected.Render("> " + line))
		} else if isSelected {
			b.WriteString(vs.Accent.Render("> " + line))
		} else {
			b.WriteString(vs.Normal.Render("  " + line))
		}
		b.WriteString("\n")
	}

	// Show scroll indicator if needed
	if len(v.robots) > maxVisible {
		remaining := len(v.robots) - startIdx - maxVisible
		if remaining > 0 {
			b.WriteString(vs.Subtext.Render(fmt.Sprintf("  ... +%d more (use arrows)", remaining)))
			b.WriteString("\n")
		}
	}

	// Spinner when pulling
	if v.spinning {
		b.WriteString("\n")
		b.WriteString("  ")
		b.WriteString(v.spinner.View())
		b.WriteString(vs.Info.Render(" Pulling robot..."))
		b.WriteString("\n")
	}

	// Render toasts overlay
	// Handled by App global toast system now

	// Footer
	b.WriteString("\n")
	hints := []KeyHint{
		{"Enter", "select/open"},
		{"Arrows", "navigate"},
		{"R", "refresh"},
	}
	b.WriteString(RenderFooter(vs, hints, contentWidth))

	return v.placeBox(boxStyle.Render(b.String()))
}

func (v *RobotsView) renderInputMode() string {
	theme := v.styles.theme
	vs := NewViewStyles(theme)

	boxWidth := v.width - 8
	if boxWidth < 60 {
		boxWidth = 60
	}
	if boxWidth > 100 {
		boxWidth = 100
	}
	contentWidth := boxWidth - 6

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 2).
		Width(boxWidth)

	var b strings.Builder

	if v.inputStep == 0 {
		// Step 1: Enter URL
		b.WriteString(RenderHeader(vs, "Pull Robot", "Step 1/2", contentWidth))
		b.WriteString("\n")

		b.WriteString(vs.Accent.Bold(true).Render("REPOSITORY URL"))
		b.WriteString("\n\n")

		b.WriteString(vs.Label.Render("URL  "))
		b.WriteString(vs.Selected.Render(v.inputBuffer + "_"))
		b.WriteString("\n\n")

		b.WriteString(vs.Subtext.Render("Enter a git repository URL or path"))
		b.WriteString("\n")
		b.WriteString(vs.Subtext.Render("Examples:"))
		b.WriteString("\n")
		b.WriteString(vs.Subtext.Render("  github.com/robocorp/example-web-scraper"))
		b.WriteString("\n")
		b.WriteString(vs.Subtext.Render("  https://gitlab.com/user/robot.git"))
		b.WriteString("\n")
		b.WriteString(vs.Subtext.Render("  git@github.com:user/repo.git"))
		b.WriteString("\n\n")

		hints := []KeyHint{
			{"Enter", "next"},
			{"Esc", "cancel"},
		}
		b.WriteString(RenderFooter(vs, hints, contentWidth))
	} else {
		// Step 2: Enter directory
		b.WriteString(RenderHeader(vs, "Pull Robot", "Step 2/2", contentWidth))
		b.WriteString("\n")

		b.WriteString(vs.Accent.Bold(true).Render("TARGET DIRECTORY"))
		b.WriteString("\n\n")

		b.WriteString(vs.Label.Render("URL  "))
		b.WriteString(vs.Subtext.Render(v.pullURL))
		b.WriteString("\n")
		b.WriteString(vs.Label.Render("Dir  "))
		b.WriteString(vs.Selected.Render(v.inputBuffer + "_"))
		b.WriteString("\n\n")

		b.WriteString(vs.Subtext.Render("Enter directory name to clone into"))
		b.WriteString("\n")
		b.WriteString(vs.Subtext.Render("(relative to current working directory)"))
		b.WriteString("\n\n")

		hints := []KeyHint{
			{"Enter", "pull"},
			{"Esc", "cancel"},
		}
		b.WriteString(RenderFooter(vs, hints, contentWidth))
	}

	return v.placeBox(boxStyle.Render(b.String()))
}

func (v *RobotsView) renderDetailView() string {
	theme := v.styles.theme
	vs := NewViewStyles(theme)

	r := v.selectedRobot()
	if r == nil {
		return lipgloss.NewStyle().Foreground(theme.TextBright).Render("No robot selected")
	}

	// Calculate layout dimensions
	totalWidth := v.width - 4
	if totalWidth < 80 {
		totalWidth = 80
	}
	if totalWidth > 140 {
		totalWidth = 140
	}

	// Two-column layout: Left (Tasks+Env) | Right (Deps+Info)
	leftWidth := (totalWidth * 55) / 100  // 55% for tasks/env
	rightWidth := totalWidth - leftWidth - 3 // Rest for deps, -3 for gap

	// Panel styling helper
	makePanel := func(title string, content string, isActive bool, w int) string {
		borderColor := theme.Border
		titleBg := theme.Surface
		if isActive {
			borderColor = theme.Accent
			titleBg = theme.Accent
		}

		titleStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.Background).
			Background(titleBg).
			Padding(0, 1)

		contentStyle := lipgloss.NewStyle().
			Foreground(theme.Text).
			Padding(0, 1)

		innerContent := lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render(title),
			"",
			contentStyle.Render(content),
		)

		return lipgloss.NewStyle().
			Width(w).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Render(innerContent)
	}

	// === BUILD TASKS PANEL ===
	var tasksContent strings.Builder
	if len(r.tasks) == 0 {
		tasksContent.WriteString(vs.Subtext.Render("  No tasks defined"))
	} else {
		for i, task := range r.tasks {
			if i > 0 {
				tasksContent.WriteString("\n")
			}
			prefix := "  ○"
			style := vs.Normal
			if i == v.taskIdx {
				if v.detailFocus == focusDetailTask {
					prefix = "▶ ●"
					style = lipgloss.NewStyle().
						Bold(true).
						Foreground(theme.TextBright).
						Background(theme.Highlight).
						Padding(0, 1)
				} else {
					prefix = "  ◉"
					style = vs.Accent
				}
			}
			// Show task command if selected
			line := prefix + " " + task
			tasksContent.WriteString(style.Render(line))
			if i == v.taskIdx && v.taskCommands[task] != "" {
				tasksContent.WriteString("\n")
				cmd := v.taskCommands[task]
				if len(cmd) > leftWidth-10 {
					cmd = cmd[:leftWidth-13] + "..."
				}
				tasksContent.WriteString(vs.Subtext.Render("      └─ " + cmd))
			}
		}
	}
	tasksPanel := makePanel("TASKS", tasksContent.String(), v.detailFocus == focusDetailTask, leftWidth)

	// === BUILD ENV FILES PANEL ===
	var envContent strings.Builder
	if len(r.envFiles) == 0 {
		envContent.WriteString(vs.Subtext.Render("  No env files"))
	} else {
		maxEnv := 6
		for i, envFile := range r.envFiles {
			if i >= maxEnv {
				envContent.WriteString("\n")
				envContent.WriteString(vs.Subtext.Render(fmt.Sprintf("  ... +%d more", len(r.envFiles)-maxEnv)))
				break
			}
			if i > 0 {
				envContent.WriteString("\n")
			}
			prefix := "  ○"
			style := vs.Normal
			if i == v.envIdx {
				if v.detailFocus == focusDetailEnv {
					prefix = "▶ ●"
					style = lipgloss.NewStyle().
						Bold(true).
						Foreground(theme.TextBright).
						Background(theme.Highlight).
						Padding(0, 1)
				} else {
					prefix = "  ◉"
					style = vs.Accent
				}
			}
			envContent.WriteString(style.Render(prefix + " " + filepath.Base(envFile)))
		}
	}
	envPanel := makePanel("ENV FILES", envContent.String(), v.detailFocus == focusDetailEnv, leftWidth)

	// === BUILD DEPENDENCIES PANEL ===
	var depsContent strings.Builder
	// Use brighter text for deps
	depStyle := lipgloss.NewStyle().Foreground(theme.Text)
	depDimStyle := lipgloss.NewStyle().Foreground(theme.TextMuted)

	// Separate conda and pip deps for visual distinction
	if len(v.condaDeps) == 0 && len(v.pipDeps) == 0 {
		depsContent.WriteString(depDimStyle.Render("  No dependencies"))
	} else {
		maxDeps := 10
		shown := 0

		// Conda deps with box symbol
		for _, dep := range v.condaDeps {
			if shown >= maxDeps {
				break
			}
			if shown > 0 {
				depsContent.WriteString("\n")
			}
			line := dep
			if len(line) > rightWidth-8 {
				line = line[:rightWidth-11] + "..."
			}
			depsContent.WriteString(depStyle.Render("  ■ " + line))
			shown++
		}

		// Pip deps with diamond symbol
		for _, dep := range v.pipDeps {
			if shown >= maxDeps {
				break
			}
			if shown > 0 {
				depsContent.WriteString("\n")
			}
			line := dep
			if len(line) > rightWidth-8 {
				line = line[:rightWidth-11] + "..."
			}
			depsContent.WriteString(depStyle.Render("  ◆ " + line))
			shown++
		}

		total := len(v.condaDeps) + len(v.pipDeps)
		if total > maxDeps {
			depsContent.WriteString("\n")
			depsContent.WriteString(depDimStyle.Render(fmt.Sprintf("  ... +%d more", total-maxDeps)))
		}
	}
	depsPanel := makePanel("DEPENDENCIES", depsContent.String(), false, rightWidth)

	// === BUILD INFO PANEL ===
	var infoContent strings.Builder
	infoStyle := lipgloss.NewStyle().Foreground(theme.Text)
	infoLabelStyle := lipgloss.NewStyle().Foreground(theme.TextMuted).Width(12)

	pyVer := v.pythonVer
	if pyVer == "" {
		pyVer = "?"
	}
	// Python with lambda symbol
	infoContent.WriteString(infoLabelStyle.Render("  λ Python"))
	infoContent.WriteString(infoStyle.Render(pyVer))
	infoContent.WriteString("\n")

	// Tasks count
	infoContent.WriteString(infoLabelStyle.Render("  ▶ Tasks"))
	infoContent.WriteString(infoStyle.Render(fmt.Sprintf("%d", len(r.tasks))))
	infoContent.WriteString("\n")

	// Env files count
	infoContent.WriteString(infoLabelStyle.Render("  ◇ Envs"))
	infoContent.WriteString(infoStyle.Render(fmt.Sprintf("%d", len(r.envFiles))))
	infoContent.WriteString("\n")

	// Channels (useful info about where deps come from)
	if len(v.condaChannels) > 0 {
		infoContent.WriteString(infoLabelStyle.Render("  ≡ Channel"))
		channelStr := v.condaChannels[0]
		if len(v.condaChannels) > 1 {
			channelStr += fmt.Sprintf(" +%d", len(v.condaChannels)-1)
		}
		infoContent.WriteString(infoStyle.Render(channelStr))
		infoContent.WriteString("\n")
	}

	// Conda hash for identification
	if v.condaHash != "" {
		infoContent.WriteString(infoLabelStyle.Render("  # Hash"))
		infoContent.WriteString(lipgloss.NewStyle().Foreground(theme.TextDim).Render(v.condaHash))
	}

	infoPanel := makePanel("INFO", infoContent.String(), false, rightWidth)

	// === ASSEMBLE LEFT COLUMN ===
	leftColumn := lipgloss.JoinVertical(lipgloss.Left,
		tasksPanel,
		"",
		envPanel,
	)

	// === ASSEMBLE RIGHT COLUMN ===
	rightColumn := lipgloss.JoinVertical(lipgloss.Left,
		infoPanel,
		"",
		depsPanel,
	)

	// === JOIN COLUMNS ===
	columns := lipgloss.JoinHorizontal(lipgloss.Top,
		leftColumn,
		"   ", // gap
		rightColumn,
	)

	// === HEADER BAR ===
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.Primary).
		Background(theme.Surface).
		Padding(0, 2)

	pathStyle := lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		PaddingLeft(2)

	relPath, _ := filepath.Rel(".", r.directory)
	if relPath == "" {
		relPath = r.directory
	}

	header := lipgloss.JoinHorizontal(lipgloss.Center,
		headerStyle.Render(r.name),
		pathStyle.Render(relPath),
	)

	// === COMMAND PREVIEW BAR ===
	cmd := v.buildShortCommand(r)
	cmdStyle := lipgloss.NewStyle().
		Foreground(theme.TextDim).
		Background(theme.Surface).
		Padding(0, 1).
		Width(totalWidth)
	cmdBar := cmdStyle.Render("> " + strings.ReplaceAll(cmd, "\n", " "))

	// === FOOTER ===
	footerStyle := lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		Width(totalWidth)

	keys := []string{
		lipgloss.NewStyle().Bold(true).Foreground(theme.Accent).Render("Enter") + " Run",
		lipgloss.NewStyle().Bold(true).Foreground(theme.Accent).Render("Tab") + " cycle",
		lipgloss.NewStyle().Bold(true).Foreground(theme.Accent).Render("e") + " edit",
		lipgloss.NewStyle().Bold(true).Foreground(theme.Accent).Render("r") + " rebuild",
		lipgloss.NewStyle().Bold(true).Foreground(theme.Accent).Render("c") + " copy cmd",
		lipgloss.NewStyle().Bold(true).Foreground(theme.Accent).Render("Esc") + " back",
	}
	footer := footerStyle.Render(strings.Join(keys, "  "))

	// === FINAL ASSEMBLY ===
	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		columns,
		"",
		cmdBar,
		footer,
	)

	// Center the whole thing
	return lipgloss.Place(
		v.width,
		v.height,
		lipgloss.Center,
		lipgloss.Center,
		content,
	)
}

// buildShortCommand creates a clean, short command preview
func (v *RobotsView) buildShortCommand(r *robotData) string {
	// Use relative directory name
	relDir, _ := filepath.Rel(".", r.directory)
	if relDir == "" || relDir == "." {
		relDir = "."
	}

	lines := []string{}
	lines = append(lines, fmt.Sprintf("rcc run -r %s/robot.yaml \\", relDir))

	if len(r.tasks) > 0 && v.taskIdx < len(r.tasks) {
		lines = append(lines, fmt.Sprintf("    -t %s \\", r.tasks[v.taskIdx]))
	}

	if v.envIdx >= 0 && v.envIdx < len(r.envFiles) {
		lines = append(lines, fmt.Sprintf("    -e %s/%s", relDir, r.envFiles[v.envIdx]))
	} else {
		// Remove trailing backslash from last line
		lastIdx := len(lines) - 1
		lines[lastIdx] = strings.TrimSuffix(lines[lastIdx], " \\")
	}

	return strings.Join(lines, "\n")
}

func (v *RobotsView) buildCommandPreview(r *robotData) string {
	relPath, _ := filepath.Rel(".", r.path)
	if relPath == "" {
		relPath = r.path
	}

	cmd := "rcc run -r " + relPath

	if len(r.tasks) > 0 && v.taskIdx < len(r.tasks) {
		cmd += " -t " + r.tasks[v.taskIdx]
	}

	if v.envIdx >= 0 && v.envIdx < len(r.envFiles) {
		envPath := filepath.Join(filepath.Dir(relPath), r.envFiles[v.envIdx])
		cmd += " -e " + envPath
	}

	return cmd
}

func (v *RobotsView) placeBox(box string) string {
	return lipgloss.Place(
		v.width,
		v.height,
		lipgloss.Center,
		lipgloss.Center,
		box,
	)
}

func (v *RobotsView) Name() string {
	return "Robots"
}

func (v *RobotsView) ShortHelp() string {
	if v.inputMode {
		return "Enter:pull Esc:cancel"
	}
	if v.mode == modeDetail {
		return "Enter:run Arrows:nav Esc:back"
	}
	return "Enter:select Arrows:nav R:refresh"
}

// timeAgo returns a human-readable time difference
func timeAgo(t time.Time) string {
	diff := time.Since(t)

	if diff < time.Minute {
		return "just now"
	} else if diff < time.Hour {
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", mins)
	} else if diff < 24*time.Hour {
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}
	days := int(diff.Hours() / 24)
	if days == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}

// Creation Wizard Logic

func (v *RobotsView) handleCreateKeys(msg tea.Msg) (View, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return v, nil
	}

	switch keyMsg.String() {
	case "esc":
		v.creating = false
		v.createStep = 0
		v.createName = ""
		return v, nil
	}

	if v.createStep == 0 {
		// Name Input
		switch keyMsg.String() {
		case "enter":
			if v.createName != "" {
				v.createStep = 1
				v.tmplListIdx = 0
			}
		case "backspace":
			if len(v.createName) > 0 {
				v.createName = v.createName[:len(v.createName)-1]
			}
		default:
			// Allow typing valid filename chars
			if len(keyMsg.String()) == 1 {
				v.createName += keyMsg.String()
			}
		}
	} else {
		// Template Selection
		switch keyMsg.String() {
		case "up", "k":
			if v.tmplListIdx > 0 {
				v.tmplListIdx--
			}
		case "down", "j":
			if v.tmplListIdx < len(v.templates)-1 {
				v.tmplListIdx++
			}
		case "enter":
			// Create robot
			return v, v.createRobot
		}
	}
	return v, nil
}

func (v *RobotsView) createRobot() tea.Msg {
	if v.tmplListIdx < 0 || v.tmplListIdx >= len(v.templates) {
		return pullRobotMsg{success: false, message: "Invalid template selection"}
	}

	template := v.templates[v.tmplListIdx]
	targetDir := v.createName

	// Check if directory exists
	if _, err := os.Stat(targetDir); err == nil {
		return pullRobotMsg{success: false, message: "Directory already exists: " + targetDir}
	}

	// Use operations to create
	// InitializeWorkarea(fullpath, templateName, force, wrapper)
	// We need absolute path for fullpath
	absPath, err := filepath.Abs(targetDir)
	if err != nil {
		return pullRobotMsg{success: false, message: err.Error()}
	}

	err = operations.InitializeWorkarea(absPath, template.Name, false, false)
	if err != nil {
		return pullRobotMsg{success: false, message: "Creation failed: " + err.Error()}
	}

	return pullRobotMsg{success: true, message: "Robot created: " + targetDir, path: targetDir}
}

func (v *RobotsView) renderCreateView() string {
	theme := v.styles.theme
	vs := NewViewStyles(theme)

	boxWidth := v.width - 8
	if boxWidth < 60 {
		boxWidth = 60
	}
	if boxWidth > 100 {
		boxWidth = 100
	}
	contentWidth := boxWidth - 6

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 2).
		Width(boxWidth)

	var b strings.Builder

	if v.createStep == 0 {
		b.WriteString(RenderHeader(vs, "New Robot", "Step 1/2", contentWidth))
		b.WriteString("\n")

		b.WriteString(vs.Accent.Bold(true).Render("ROBOT NAME"))
		b.WriteString("\n\n")

		b.WriteString(vs.Label.Render("Name "))
		b.WriteString(vs.Selected.Render(v.createName + "_"))
		b.WriteString("\n\n")
		b.WriteString(vs.Subtext.Render("Enter a name for your new robot."))
		b.WriteString("\n")
		b.WriteString(vs.Subtext.Render("A folder with this name will be created."))
		b.WriteString("\n\n")

		hints := []KeyHint{
			{"Enter", "next"},
			{"Esc", "cancel"},
		}
		b.WriteString(RenderFooter(vs, hints, contentWidth))
	} else {
		b.WriteString(RenderHeader(vs, "New Robot", "Step 2/2", contentWidth))
		b.WriteString("\n")

		b.WriteString(vs.Accent.Bold(true).Render("SELECT TEMPLATE"))
		b.WriteString("\n\n")

		// Template List
		maxShow := 8
		start := 0
		if v.tmplListIdx > maxShow-1 {
			start = v.tmplListIdx - (maxShow - 1)
		}

		for i := start; i < len(v.templates) && i < start+maxShow; i++ {
			tmpl := v.templates[i]
			prefix := "  "
			style := vs.Normal
			descStyle := vs.Subtext

			if i == v.tmplListIdx {
				prefix = "> "
				style = vs.Selected
				descStyle = vs.Info
			}

			line := fmt.Sprintf("%-20s %s", tmpl.DisplayName, descStyle.Render(tmpl.Description))
			b.WriteString(style.Render(prefix + line))
			b.WriteString("\n")
		}

		if len(v.templates) > maxShow {
			b.WriteString(vs.Subtext.Render("..."))
			b.WriteString("\n")
		}
		b.WriteString("\n")

		hints := []KeyHint{
			{"Enter", "create"},
			{"Esc", "back"},
		}
		b.WriteString(RenderFooter(vs, hints, contentWidth))
	}

	return v.placeBox(boxStyle.Render(b.String()))
}
