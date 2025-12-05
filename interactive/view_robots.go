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
	"github.com/joshyorko/rcc/robot"
)

// Toast represents a temporary notification
type Toast struct {
	Message   string
	Type      string // "success", "error", "info"
	ExpiresAt time.Time
}

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

	// Toast notifications
	toasts []Toast

	// Status message (legacy, being replaced by toasts)
	message string
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

// addToast adds a toast notification that auto-expires
func (v *RobotsView) addToast(msg string, toastType string) tea.Cmd {
	toast := Toast{
		Message:   msg,
		Type:      toastType,
		ExpiresAt: time.Now().Add(4 * time.Second),
	}
	v.toasts = append(v.toasts, toast)

	// Return a command to remove the toast after expiry
	return tea.Tick(4*time.Second, func(t time.Time) tea.Msg {
		return toastExpiredMsg{}
	})
}

// cleanExpiredToasts removes toasts that have expired
func (v *RobotsView) cleanExpiredToasts() {
	now := time.Now()
	active := []Toast{}
	for _, t := range v.toasts {
		if t.ExpiresAt.After(now) {
			active = append(active, t)
		}
	}
	v.toasts = active
}

type robotsLoadedMsg struct {
	robots []robotData
	err    error
}

func (v *RobotsView) scanForRobots() tea.Msg {
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

	result := robotDetailLoadedMsg{}

	// Parse robot.yaml for conda file reference
	// CondaConfigFile() returns an absolute path already
	if robotYaml, err := robot.LoadRobotYaml(r.path, false); err == nil {
		result.condaFile = robotYaml.CondaConfigFile()
	}

	// Load conda.yaml info - condaFile is already absolute, or use default
	condaPath := result.condaFile
	if condaPath == "" {
		condaPath = filepath.Join(r.directory, "conda.yaml")
	}

	if info, err := os.Stat(condaPath); err == nil {
		result.condaModTime = info.ModTime()

		// Read and hash conda contents
		if data, err := os.ReadFile(condaPath); err == nil {
			hash := sha256.Sum256(data)
			result.condaHash = hex.EncodeToString(hash[:8])

			// Parse for display info
			result.pythonVer, result.dependencies = parseDependencies(string(data))
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
				v.message = fmt.Sprintf("Saved %s - rebuild recommended", filepath.Base(editMsg.file))
				v.envStatus = EnvStatusNeedsRebuild
			}
			return v, v.loadRobotDetails
		}
		return v, nil
	}

	// Handle input mode
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

	case robotDetailLoadedMsg:
		v.loading = false
		v.condaFile = msg.condaFile
		v.condaModTime = msg.condaModTime
		v.condaHash = msg.condaHash
		v.pythonVer = msg.pythonVer
		v.dependencies = msg.dependencies
		v.envStatus = msg.envStatus
		v.envLastBuilt = msg.envLastBuilt

	case envRebuildMsg:
		v.loading = false
		v.spinning = false
		var toastCmd tea.Cmd
		if msg.success {
			v.envStatus = EnvStatusReady
			v.envLastBuilt = time.Now()
			toastCmd = v.addToast("Environment rebuilt successfully", "success")
		} else {
			toastCmd = v.addToast("Rebuild failed: "+msg.message, "error")
		}
		return v, toastCmd

	case pullRobotMsg:
		v.spinning = false
		v.message = ""
		var toastCmd tea.Cmd
		if msg.success {
			toastCmd = v.addToast("Robot pulled successfully!", "success")
			// Refresh robot list to show the new robot
			return v, tea.Batch(toastCmd, v.scanForRobots)
		} else {
			// Truncate error message if too long
			errMsg := msg.message
			if len(errMsg) > 60 {
				errMsg = errMsg[:57] + "..."
			}
			toastCmd = v.addToast("Pull failed: "+errMsg, "error")
		}
		return v, toastCmd

	case spinner.TickMsg:
		if v.spinning {
			var cmd tea.Cmd
			v.spinner, cmd = v.spinner.Update(msg)
			return v, cmd
		}

	case toastExpiredMsg:
		v.cleanExpiredToasts()

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
	}

	return v, nil
}

func (v *RobotsView) handleListKeys(msg tea.KeyMsg) (View, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		v.moveUpList()
	case "down", "j":
		v.moveDownList()
	case "left", "h":
		v.moveLeftList()
	case "right", "l":
		v.moveRightList()
	case "R":
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
			v.message = ""
			return v, v.loadRobotDetails
		}
	}
	return v, nil
}

func (v *RobotsView) handleListAction() (View, tea.Cmd) {
	// Handle pull robot - enter input mode for URL
	if v.actionIdx == actionPullRobot {
		v.inputMode = true
		v.inputBuffer = ""
		return v, nil
	}

	// Handle templates - execute directly
	templates := map[int]string{
		actionTemplateBasic:     "01-python",
		actionTemplateBrowser:   "02-python-browser",
		actionTemplateWorkitems: "03-python-workitems",
		actionTemplateAI:        "04-python-assistant-ai",
	}

	if templateName, ok := templates[v.actionIdx]; ok {
		action := ActionResult{
			Type:    ActionRunCommand,
			Command: "rcc robot init --template " + templateName,
		}
		return v, func() tea.Msg { return actionMsg{action: action} }
	}
	return v, nil
}

func (v *RobotsView) handleInputMode(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			v.inputMode = false
			v.inputBuffer = ""
		case "enter":
			if v.inputBuffer != "" {
				// Execute pull within UI
				url := v.inputBuffer
				v.inputMode = false
				v.inputBuffer = ""
				v.spinning = true
				v.message = "Pulling robot from " + url + "..."
				return v, tea.Batch(v.spinner.Tick, v.pullRobot(url))
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

func (v *RobotsView) pullRobot(url string) tea.Cmd {
	return func() tea.Msg {
		rccPath, err := os.Executable()
		if err != nil {
			return pullRobotMsg{success: false, message: err.Error()}
		}

		cmd := exec.Command(rccPath, "pull", url)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return pullRobotMsg{success: false, message: string(output)}
		}

		// Parse output to find where it was pulled
		outputStr := string(output)
		// rcc pull typically outputs the path where it was cloned
		return pullRobotMsg{success: true, message: "Robot pulled successfully", path: outputStr}
	}
}

func (v *RobotsView) handleDetailKeys(msg tea.KeyMsg) (View, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Return to list mode
		v.mode = modeList
		v.message = ""
		return v, nil

	case "up", "k":
		v.moveUpDetail()
	case "down", "j":
		v.moveDownDetail()
	case "left", "h":
		v.moveLeftDetail()
	case "right", "l":
		v.moveRightDetail()

	case "e":
		// Quick edit based on current focus
		if v.detailFocus == focusDetailConfig {
			return v, v.handleConfigAction()
		}

	case "r":
		// Rebuild environment
		v.loading = true
		v.message = "Rebuilding environment..."
		return v, v.rebuildEnvironment

	case "R":
		// Refresh
		v.loading = true
		return v, v.loadRobotDetails

	case "enter":
		if v.detailFocus == focusDetailConfig {
			return v, v.handleConfigAction()
		}
		// Run the robot
		return v, v.runRobot()
	}
	return v, nil
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
			v.actionIdx = actionTemplateAI // Last action
		}
	}
}

func (v *RobotsView) moveDownList() {
	switch v.focus {
	case focusActions:
		if v.actionIdx < actionTemplateAI {
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
		{"Python Basic", "Simple Python robot (01-python)"},
		{"Python Browser", "Browser automation with Playwright (02-python-browser)"},
		{"Python Work Items", "Work items processing (03-python-workitems)"},
		{"Python AI Assistant", "AI assistant robot (04-python-assistant-ai)"},
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
		if v.message != "" {
			b.WriteString(vs.Info.Render(" " + v.message))
		}
		b.WriteString("\n")
	}

	// Toast notifications
	if len(v.toasts) > 0 {
		b.WriteString("\n")
		for _, toast := range v.toasts {
			icon := "  "
			style := vs.Info
			switch toast.Type {
			case "success":
				icon = "  [OK] "
				style = vs.Success
			case "error":
				icon = "  [X] "
				style = vs.Error
			case "info":
				icon = "  [i] "
				style = vs.Info
			}
			b.WriteString(style.Render(icon + toast.Message))
			b.WriteString("\n")
		}
	}

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

	b.WriteString(RenderHeader(vs, "Pull Robot", "From Git", contentWidth))
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
		{"Enter", "pull"},
		{"Esc", "cancel"},
	}
	b.WriteString(RenderFooter(vs, hints, contentWidth))

	return v.placeBox(boxStyle.Render(b.String()))
}

func (v *RobotsView) renderDetailView() string {
	theme := v.styles.theme
	vs := NewViewStyles(theme)

	boxWidth := v.width - 8
	if boxWidth < 70 {
		boxWidth = 70
	}
	if boxWidth > 110 {
		boxWidth = 110
	}
	contentWidth := boxWidth - 6

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 2).
		Width(boxWidth)

	var b strings.Builder

	r := v.selectedRobot()
	if r == nil {
		b.WriteString(vs.Subtext.Render("No robot selected"))
		return v.placeBox(boxStyle.Render(b.String()))
	}

	// Header
	robotName := r.name
	if len(robotName) > 30 {
		robotName = robotName[:27] + "..."
	}
	relPath, _ := filepath.Rel(".", r.path)
	b.WriteString(RenderHeader(vs, "Robot", robotName, contentWidth))
	b.WriteString("\n")

	// RUN section - PRIMARY (at top)
	b.WriteString(vs.Accent.Bold(true).Render("RUN"))
	b.WriteString("\n\n")

	// Task row
	taskPrefix := "  "
	taskLabelStyle := vs.Normal
	if v.detailFocus == focusDetailTask {
		taskPrefix = "> "
		taskLabelStyle = vs.Selected
	}
	b.WriteString(taskLabelStyle.Render(taskPrefix))
	b.WriteString(vs.Label.Render("Task    "))

	if len(r.tasks) == 0 {
		b.WriteString(vs.Subtext.Render("(default)"))
	} else {
		for i, task := range r.tasks {
			if i == v.taskIdx {
				b.WriteString(vs.BadgeActive.Render("[" + task + "]"))
			} else {
				b.WriteString(vs.Badge.Render(" " + task + " "))
			}
			b.WriteString(" ")
		}
	}
	b.WriteString("\n")

	// Env row (only if env files exist)
	if len(r.envFiles) > 0 {
		envPrefix := "  "
		envLabelStyle := vs.Normal
		if v.detailFocus == focusDetailEnv {
			envPrefix = "> "
			envLabelStyle = vs.Selected
		}
		b.WriteString(envLabelStyle.Render(envPrefix))
		b.WriteString(vs.Label.Render("Env     "))

		// None option
		if v.envIdx == -1 {
			b.WriteString(vs.BadgeActive.Render("[none]"))
		} else {
			b.WriteString(vs.Badge.Render(" none "))
		}
		b.WriteString(" ")

		// Env files
		maxShow := 3
		for i, envFile := range r.envFiles {
			if i >= maxShow {
				b.WriteString(vs.Subtext.Render(fmt.Sprintf("+%d", len(r.envFiles)-maxShow)))
				break
			}
			displayName := filepath.Base(envFile)
			if len(displayName) > 12 {
				displayName = displayName[:9] + "..."
			}
			if i == v.envIdx {
				b.WriteString(vs.BadgeActive.Render("[" + displayName + "]"))
			} else {
				b.WriteString(vs.Badge.Render(" " + displayName + " "))
			}
			b.WriteString(" ")
		}
		b.WriteString("\n")
	}

	// Command preview
	b.WriteString("\n")
	cmd := v.buildCommandPreview(r)
	b.WriteString(vs.Info.Render("  " + cmd))
	b.WriteString("\n")

	b.WriteString("\n")
	b.WriteString(vs.Separator.Render(strings.Repeat("-", contentWidth)))
	b.WriteString("\n\n")

	// Info section (condensed)
	b.WriteString(vs.Accent.Bold(true).Render("INFO"))
	b.WriteString("\n\n")

	b.WriteString(vs.Label.Render("Path"))
	b.WriteString(vs.Info.Render(relPath))
	b.WriteString("\n")

	// Environment status inline
	b.WriteString(vs.Label.Render("Env"))
	switch v.envStatus {
	case EnvStatusReady:
		b.WriteString(vs.Success.Render("[Ready]"))
		if !v.envLastBuilt.IsZero() {
			b.WriteString(vs.Subtext.Render(fmt.Sprintf(" built %s", timeAgo(v.envLastBuilt))))
		}
	case EnvStatusNeedsRebuild:
		b.WriteString(vs.Warning.Render("[Needs Rebuild]"))
	case EnvStatusNotBuilt:
		b.WriteString(vs.Error.Render("[Not Built]"))
	default:
		b.WriteString(vs.Subtext.Render("[Unknown]"))
	}
	b.WriteString("\n")

	if v.pythonVer != "" {
		b.WriteString(vs.Label.Render("Python"))
		b.WriteString(vs.Text.Render(v.pythonVer))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(vs.Separator.Render(strings.Repeat("-", contentWidth)))
	b.WriteString("\n\n")

	// Configuration section (at bottom)
	b.WriteString(vs.Accent.Bold(true).Render("CONFIGURE"))
	b.WriteString("\n\n")

	configActions := []struct {
		label string
		desc  string
	}{
		{"Edit robot.yaml", "Configure tasks and settings"},
		{"Edit conda.yaml", "Configure dependencies"},
		{"Rebuild Environment", "Force rebuild from conda.yaml"},
	}

	for i, action := range configActions {
		prefix := "  "
		style := vs.Normal
		if v.detailFocus == focusDetailConfig && i == v.configIdx {
			prefix = "> "
			style = vs.Selected
		}
		b.WriteString(style.Render(prefix + action.label))
		b.WriteString("\n")
		if v.detailFocus == focusDetailConfig && i == v.configIdx {
			b.WriteString("    ")
			b.WriteString(vs.Subtext.Render(action.desc))
			b.WriteString("\n")
		}
	}

	// Spinner when rebuilding
	if v.spinning {
		b.WriteString("\n")
		b.WriteString("  ")
		b.WriteString(v.spinner.View())
		b.WriteString(vs.Info.Render(" Rebuilding environment..."))
		b.WriteString("\n")
	}

	// Toast notifications
	if len(v.toasts) > 0 {
		b.WriteString("\n")
		for _, toast := range v.toasts {
			icon := "  "
			style := vs.Info
			switch toast.Type {
			case "success":
				icon = "  [OK] "
				style = vs.Success
			case "error":
				icon = "  [X] "
				style = vs.Error
			case "info":
				icon = "  [i] "
				style = vs.Info
			}
			b.WriteString(style.Render(icon + toast.Message))
			b.WriteString("\n")
		}
	}

	// Footer
	b.WriteString("\n")
	hints := []KeyHint{
		{"Enter", "run"},
		{"Arrows", "navigate"},
		{"Esc", "back"},
	}
	b.WriteString(RenderFooter(vs, hints, contentWidth))

	return v.placeBox(boxStyle.Render(b.String()))
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
