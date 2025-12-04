package interactive

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joshyorko/rcc/robot"
)

// RobotInfo holds information about a detected robot
type RobotInfo struct {
	Name        string
	Path        string
	Tasks       []string
	CondaFile   string
	RobotYAML   string // raw content
	CondaYAML   string // raw content
	Description string
}

// ViewMode represents the current view mode
type ViewMode int

const (
	ModeList ViewMode = iota
	ModeDetail
	ModeRun // Run configuration mode
)

// RunConfig holds the run configuration
type RunConfig struct {
	Task        int    // selected task index
	Space       string // holotree space
	Dev         bool   // development mode
	Force       bool   // force environment rebuild
	Interactive bool   // interactive terminal mode
	NoOutputs   bool   // disable output capture
}

// RobotsView displays detected robots
type RobotsView struct {
	styles     *Styles
	robots     []RobotInfo
	cursor     int
	scanPath   string
	mode       ViewMode
	detailTab  int // 0 = overview, 1 = robot.yaml, 2 = conda.yaml
	scrollY    int
	editorMsg  string
	runConfig  RunConfig // run configuration
	runOption  int       // current option in run mode (0=task, 1=space, 2=options...)
	runStatus  string    // status message for run
}

// NewRobotsView creates a new robots view
func NewRobotsView(styles *Styles) *RobotsView {
	cwd, _ := os.Getwd()
	v := &RobotsView{
		styles:   styles,
		scanPath: cwd,
		runConfig: RunConfig{
			Space: "user",
		},
	}
	v.scanForRobots()
	return v
}

// Init implements View
func (v *RobotsView) Init() tea.Cmd {
	return nil
}

// scanForRobots scans the current directory for robot.yaml files synchronously
func (v *RobotsView) scanForRobots() {
	v.robots = nil

	// Walk the directory looking for robot.yaml files
	filepath.Walk(v.scanPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip hidden directories and common excludes
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "__pycache__" || name == "output" || name == "build" {
				return filepath.SkipDir
			}
			return nil
		}

		// Check if this is a robot.yaml
		if info.Name() == "robot.yaml" {
			robotInfo := loadRobotInfo(path)
			if robotInfo != nil {
				v.robots = append(v.robots, *robotInfo)
			}
		}

		return nil
	})
}

func loadRobotInfo(robotPath string) *RobotInfo {
	config, err := robot.LoadRobotYaml(robotPath, false)
	if err != nil {
		return nil
	}

	info := &RobotInfo{
		Path: robotPath,
		Name: filepath.Base(filepath.Dir(robotPath)),
	}

	// Get tasks
	info.Tasks = config.AvailableTasks()

	// Get conda file
	info.CondaFile = config.CondaConfigFile()

	// Load raw robot.yaml content
	if robotContent, err := os.ReadFile(robotPath); err == nil {
		info.RobotYAML = string(robotContent)
	}

	// Load raw conda.yaml content
	if info.CondaFile != "" {
		condaPath := filepath.Join(filepath.Dir(robotPath), info.CondaFile)
		if condaContent, err := os.ReadFile(condaPath); err == nil {
			info.CondaYAML = string(condaContent)
		}
	}

	return info
}

// editorFinishedMsg signals the editor has finished
type editorFinishedMsg struct {
	err error
}

// robotRunFinishedMsg signals the robot run has finished
type robotRunFinishedMsg struct {
	err error
}

// Update implements View
func (v *RobotsView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case editorFinishedMsg:
		if msg.err != nil {
			v.editorMsg = "Editor error: " + msg.err.Error()
		} else {
			v.editorMsg = "File saved"
			// Reload the robot info
			if v.cursor < len(v.robots) {
				r := &v.robots[v.cursor]
				if updated := loadRobotInfo(r.Path); updated != nil {
					*r = *updated
				}
			}
		}
		return v, nil

	case robotRunFinishedMsg:
		if msg.err != nil {
			v.runStatus = "Run failed: " + msg.err.Error()
		} else {
			v.runStatus = "Run completed successfully"
		}
		v.mode = ModeList
		return v, nil

	case tea.KeyMsg:
		// Handle different modes
		switch v.mode {
		case ModeDetail:
			return v.updateDetailMode(msg)
		case ModeRun:
			return v.updateRunMode(msg)
		default:
			return v.updateListMode(msg)
		}
	}
	return v, nil
}

func (v *RobotsView) updateListMode(msg tea.KeyMsg) (View, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Down):
		if v.cursor < len(v.robots)-1 {
			v.cursor++
		}
	case key.Matches(msg, keys.Up):
		if v.cursor > 0 {
			v.cursor--
		}
	case key.Matches(msg, keys.Top):
		v.cursor = 0
	case key.Matches(msg, keys.Bottom):
		v.cursor = len(v.robots) - 1
	case key.Matches(msg, keys.Refresh):
		v.scanForRobots()
	case key.Matches(msg, keys.Select), key.Matches(msg, keys.Right):
		// Enter detail mode
		if v.cursor < len(v.robots) {
			v.mode = ModeDetail
			v.detailTab = 0
			v.scrollY = 0
		}
	case key.Matches(msg, keys.Run), msg.String() == "r":
		// Enter run mode
		if v.cursor < len(v.robots) && len(v.robots[v.cursor].Tasks) > 0 {
			v.mode = ModeRun
			v.runOption = 0
			v.runConfig.Task = 0
			v.runStatus = ""
		}
	case msg.String() == "e":
		// Edit robot.yaml
		if v.cursor < len(v.robots) {
			return v, v.openEditor(v.robots[v.cursor].Path)
		}
	}
	return v, nil
}

func (v *RobotsView) updateRunMode(msg tea.KeyMsg) (View, tea.Cmd) {
	// Run mode has 6 options: task, space, dev, force, interactive, no-outputs
	maxOptions := 6

	switch {
	case key.Matches(msg, keys.Back), msg.String() == "q", msg.String() == "esc":
		// Return to list mode
		v.mode = ModeList
		v.runStatus = ""
	case key.Matches(msg, keys.Down), msg.String() == "j":
		if v.runOption < maxOptions-1 {
			v.runOption++
		}
	case key.Matches(msg, keys.Up), msg.String() == "k":
		if v.runOption > 0 {
			v.runOption--
		}
	case key.Matches(msg, keys.Select), msg.String() == "enter":
		// Execute the run
		return v, v.executeRun()
	case msg.String() == " ", msg.String() == "tab":
		// Toggle/cycle current option
		r := v.robots[v.cursor]
		switch v.runOption {
		case 0: // Task selection - cycle through tasks
			v.runConfig.Task = (v.runConfig.Task + 1) % len(r.Tasks)
		case 1: // Space - toggle between "user" and "dev"
			if v.runConfig.Space == "user" {
				v.runConfig.Space = "dev"
			} else {
				v.runConfig.Space = "user"
			}
		case 2: // Dev mode toggle
			v.runConfig.Dev = !v.runConfig.Dev
		case 3: // Force toggle
			v.runConfig.Force = !v.runConfig.Force
		case 4: // Interactive toggle
			v.runConfig.Interactive = !v.runConfig.Interactive
		case 5: // No outputs toggle
			v.runConfig.NoOutputs = !v.runConfig.NoOutputs
		}
	case msg.String() == "left", msg.String() == "h":
		// Cycle backwards for task
		if v.runOption == 0 {
			r := v.robots[v.cursor]
			v.runConfig.Task = (v.runConfig.Task - 1 + len(r.Tasks)) % len(r.Tasks)
		}
	case msg.String() == "right", msg.String() == "l":
		// Cycle forwards for task
		if v.runOption == 0 {
			r := v.robots[v.cursor]
			v.runConfig.Task = (v.runConfig.Task + 1) % len(r.Tasks)
		}
	}
	return v, nil
}

// executeRun starts the robot run
func (v *RobotsView) executeRun() tea.Cmd {
	if v.cursor >= len(v.robots) {
		return nil
	}

	r := v.robots[v.cursor]
	task := ""
	if v.runConfig.Task < len(r.Tasks) {
		task = r.Tasks[v.runConfig.Task]
	}

	// Build the rcc run command
	args := []string{"run", "-r", r.Path, "-t", task, "-s", v.runConfig.Space}
	if v.runConfig.Dev {
		args = append(args, "--dev")
	}
	if v.runConfig.Force {
		args = append(args, "-f")
	}
	if v.runConfig.Interactive {
		args = append(args, "--interactive")
	}
	if v.runConfig.NoOutputs {
		args = append(args, "--no-outputs")
	}

	// Get the path to the rcc executable (use the current binary)
	executable, err := os.Executable()
	if err != nil {
		executable = "rcc"
	}

	c := exec.Command(executable, args...)
	c.Dir = filepath.Dir(r.Path)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return robotRunFinishedMsg{err: err}
	})
}

func (v *RobotsView) updateDetailMode(msg tea.KeyMsg) (View, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back), key.Matches(msg, keys.Left), msg.String() == "q":
		// Return to list mode
		v.mode = ModeList
		v.editorMsg = ""
	case msg.String() == "tab", key.Matches(msg, keys.Right):
		// Cycle through tabs
		v.detailTab = (v.detailTab + 1) % 3
		v.scrollY = 0
	case msg.String() == "shift+tab":
		// Cycle backwards
		v.detailTab = (v.detailTab + 2) % 3
		v.scrollY = 0
	case key.Matches(msg, keys.Down):
		v.scrollY++
	case key.Matches(msg, keys.Up):
		if v.scrollY > 0 {
			v.scrollY--
		}
	case msg.String() == "e":
		// Edit the current file
		if v.cursor < len(v.robots) {
			r := v.robots[v.cursor]
			switch v.detailTab {
			case 0, 1: // Overview or robot.yaml
				return v, v.openEditor(r.Path)
			case 2: // conda.yaml
				condaPath := filepath.Join(filepath.Dir(r.Path), r.CondaFile)
				return v, v.openEditor(condaPath)
			}
		}
	}
	return v, nil
}

// openEditor opens the file in the user's preferred editor
func (v *RobotsView) openEditor(filePath string) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vim" // fallback
	}

	c := exec.Command(editor, filePath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}

// View implements View
func (v *RobotsView) View() string {
	switch v.mode {
	case ModeDetail:
		return v.renderDetailView()
	case ModeRun:
		return v.renderRunView()
	default:
		return v.renderListView()
	}
}

func (v *RobotsView) renderListView() string {
	var b strings.Builder

	// Header with RCC-style
	header := v.styles.Info.Render("####") + "  " + v.styles.PanelTitle.Render("Detected Robots") + "  " + v.styles.Info.Render("####")
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(v.styles.Subtle.Render(fmt.Sprintf("    Scanning: %s", v.scanPath)))
	b.WriteString("\n\n")

	if len(v.robots) == 0 {
		b.WriteString(v.styles.Warning.Render("    No robots found in this directory."))
		b.WriteString("\n\n")
		b.WriteString(v.styles.Subtle.Render("    Try running from a directory containing robot.yaml files."))
		return b.String()
	}

	// Robot list with better spacing
	for i, r := range v.robots {
		var line string
		taskCount := fmt.Sprintf("(%d tasks)", len(r.Tasks))

		if i == v.cursor {
			line = v.styles.ListItemSelected.Render(fmt.Sprintf("  ▶ %s %s", r.Name, taskCount))
		} else {
			line = v.styles.ListItem.Render(fmt.Sprintf("    %s ", r.Name)) + v.styles.Subtle.Render(taskCount)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Details panel for selected robot
	if v.cursor < len(v.robots) {
		r := v.robots[v.cursor]
		b.WriteString("\n")

		detailHeader := v.styles.Info.Render("####") + "  " + v.styles.PanelTitle.Render("Robot Details") + "  " + v.styles.Info.Render("####")
		b.WriteString(detailHeader)
		b.WriteString("\n\n")

		b.WriteString("    " + v.styles.Subtle.Render("Path:  ") + v.styles.Highlight.Render(r.Path) + "\n")
		b.WriteString("    " + v.styles.Subtle.Render("Conda: ") + v.styles.Info.Render(r.CondaFile) + "\n")

		if len(r.Tasks) > 0 {
			b.WriteString("\n    " + v.styles.Subtle.Render("Tasks:") + "\n")
			for _, task := range r.Tasks {
				b.WriteString("      " + v.styles.Success.Render("• ") + v.styles.Highlight.Render(task) + "\n")
			}
		}
	}

	return b.String()
}

func (v *RobotsView) renderRunView() string {
	if v.cursor >= len(v.robots) {
		return "No robot selected"
	}

	r := v.robots[v.cursor]
	var b strings.Builder

	// Header
	header := v.styles.Info.Render("####") + "  " + v.styles.PanelTitle.Render("Run Robot") + "  " + v.styles.Info.Render("####")
	b.WriteString(header)
	b.WriteString("\n\n")

	// Robot info
	b.WriteString("    " + v.styles.Subtle.Render("Robot: ") + v.styles.Highlight.Render(r.Name) + "\n")
	b.WriteString("    " + v.styles.Subtle.Render("Path:  ") + v.styles.Subtle.Render(filepath.Dir(r.Path)) + "\n\n")

	// Options list - wizard style with numbered options
	options := []struct {
		num   int
		name  string
		value string
		desc  string
	}{
		{1, "Task", "", "Select task to run"},
		{2, "Space", v.runConfig.Space, "Holotree space name"},
		{3, "Dev Mode", v.boolToStr(v.runConfig.Dev), "Use devTasks instead of normal tasks"},
		{4, "Force", v.boolToStr(v.runConfig.Force), "Force conda cache update"},
		{5, "Interactive", v.boolToStr(v.runConfig.Interactive), "Allow interactive terminal"},
		{6, "No Outputs", v.boolToStr(v.runConfig.NoOutputs), "Disable output capture"},
	}

	// Task value
	if v.runConfig.Task < len(r.Tasks) {
		options[0].value = r.Tasks[v.runConfig.Task]
	}

	for i, opt := range options {
		cursor := "  "
		numStyle := v.styles.Success
		nameStyle := v.styles.Highlight
		if i == v.runOption {
			cursor = v.styles.Success.Render("▶ ")
			numStyle = v.styles.Warning
			nameStyle = v.styles.Warning
		}

		// Format: ▶ 1) Task: my-task
		//            Description text
		line := cursor + numStyle.Render(fmt.Sprintf("%d)", opt.num)) + " " +
			nameStyle.Render(opt.name+": ") + v.styles.Info.Render(opt.value)
		b.WriteString(line + "\n")
		b.WriteString("       " + v.styles.Subtle.Render(opt.desc) + "\n\n")
	}

	// Command preview
	b.WriteString(v.styles.Info.Render("####") + "  " + v.styles.PanelTitle.Render("Command Preview") + "  " + v.styles.Info.Render("####") + "\n\n")
	cmdPreview := v.buildCommandPreview(r)
	b.WriteString("    " + v.styles.Subtle.Render("$ ") + v.styles.Highlight.Render(cmdPreview) + "\n\n")

	// Help text
	b.WriteString(v.styles.Subtle.Render("    j/k: Navigate  Space/Tab: Toggle  Enter: Run  q/Esc: Cancel") + "\n")

	// Status message
	if v.runStatus != "" {
		b.WriteString("\n    " + v.styles.Warning.Render(v.runStatus) + "\n")
	}

	return b.String()
}

func (v *RobotsView) boolToStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func (v *RobotsView) buildCommandPreview(r RobotInfo) string {
	task := ""
	if v.runConfig.Task < len(r.Tasks) {
		task = r.Tasks[v.runConfig.Task]
	}

	cmd := fmt.Sprintf("rcc run -r %s -t %s -s %s", r.Path, task, v.runConfig.Space)
	if v.runConfig.Dev {
		cmd += " --dev"
	}
	if v.runConfig.Force {
		cmd += " -f"
	}
	if v.runConfig.Interactive {
		cmd += " --interactive"
	}
	if v.runConfig.NoOutputs {
		cmd += " --no-outputs"
	}
	return cmd
}

func (v *RobotsView) renderDetailView() string {
	if v.cursor >= len(v.robots) {
		return "No robot selected"
	}

	r := v.robots[v.cursor]
	var b strings.Builder

	// === Header: Robot name and path ===
	header := v.styles.Title.Render(r.Name)
	path := v.styles.Subtle.Render(r.Path)
	b.WriteString(header + "  " + path)
	b.WriteString("\n\n")

	// === Tabs ===
	tabs := v.renderDetailTabs()
	b.WriteString(tabs)
	b.WriteString("\n\n")

	// === Content based on tab ===
	switch v.detailTab {
	case 0:
		b.WriteString(v.renderOverviewTab(r))
	case 1:
		b.WriteString(v.renderYAMLTab("robot.yaml", r.RobotYAML))
	case 2:
		b.WriteString(v.renderYAMLTab("conda.yaml", r.CondaYAML))
	}

	// === Editor message ===
	if v.editorMsg != "" {
		b.WriteString("\n")
		b.WriteString(v.styles.Success.Render(v.editorMsg))
	}

	return b.String()
}

func (v *RobotsView) renderDetailTabs() string {
	tabNames := []string{"Overview", "robot.yaml", "conda.yaml"}
	var tabs []string

	for i, name := range tabNames {
		if i == v.detailTab {
			tabs = append(tabs, v.styles.ActiveTab.Render(" "+name+" "))
		} else {
			tabs = append(tabs, v.styles.Tab.Render(" "+name+" "))
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, tabs...)
}

func (v *RobotsView) renderOverviewTab(r RobotInfo) string {
	var b strings.Builder

	// Tasks section
	tasksTitle := v.styles.PanelTitle.Render("Tasks")
	b.WriteString(tasksTitle)
	b.WriteString("\n")

	if len(r.Tasks) == 0 {
		b.WriteString(v.styles.Subtle.Render("  No tasks defined"))
	} else {
		for i, task := range r.Tasks {
			icon := "  "
			if i == 0 {
				icon = v.styles.Success.Render("▶ ")
			}
			b.WriteString(icon + v.styles.Highlight.Render(task) + "\n")
		}
	}
	b.WriteString("\n")

	// Environment section
	envTitle := v.styles.PanelTitle.Render("Environment")
	b.WriteString(envTitle)
	b.WriteString("\n")
	b.WriteString("  " + v.styles.Subtle.Render("Conda file: ") + v.styles.Info.Render(r.CondaFile))
	b.WriteString("\n\n")

	// Files section
	filesTitle := v.styles.PanelTitle.Render("Files")
	b.WriteString(filesTitle)
	b.WriteString("\n")
	b.WriteString("  " + v.styles.TreeLeaf.Render("robot.yaml") + "\n")
	if r.CondaFile != "" {
		b.WriteString("  " + v.styles.TreeLeaf.Render(r.CondaFile) + "\n")
	}

	return b.String()
}

func (v *RobotsView) renderYAMLTab(filename, content string) string {
	if content == "" {
		return v.styles.Subtle.Render("No content available")
	}

	// Split into lines and handle scrolling
	lines := strings.Split(content, "\n")
	visibleLines := 20 // max visible lines

	start := v.scrollY
	if start >= len(lines) {
		start = len(lines) - 1
	}
	if start < 0 {
		start = 0
	}

	end := start + visibleLines
	if end > len(lines) {
		end = len(lines)
	}

	// Render with line numbers
	var b strings.Builder
	for i := start; i < end; i++ {
		lineNum := v.styles.Subtle.Render(fmt.Sprintf("%3d ", i+1))
		lineContent := v.renderYAMLLine(lines[i])
		b.WriteString(lineNum + lineContent + "\n")
	}

	// Scroll indicator
	if len(lines) > visibleLines {
		indicator := v.styles.Subtle.Render(fmt.Sprintf("\n[%d/%d lines]", end, len(lines)))
		b.WriteString(indicator)
	}

	return b.String()
}

func (v *RobotsView) renderYAMLLine(line string) string {
	// Simple YAML syntax highlighting
	trimmed := strings.TrimSpace(line)

	// Comments
	if strings.HasPrefix(trimmed, "#") {
		return v.styles.Subtle.Render(line)
	}

	// Keys (before colon)
	if idx := strings.Index(line, ":"); idx > 0 {
		key := line[:idx]
		rest := line[idx:]
		return v.styles.Info.Render(key) + v.styles.Highlight.Render(rest)
	}

	// List items
	if strings.HasPrefix(trimmed, "-") {
		return v.styles.Warning.Render(line)
	}

	return line
}

// Name implements View
func (v *RobotsView) Name() string {
	return "Robots"
}

// ShortHelp implements View
func (v *RobotsView) ShortHelp() string {
	switch v.mode {
	case ModeDetail:
		return "tab:switch  j/k:scroll  e:edit  q:back"
	case ModeRun:
		return "j/k:nav  space:toggle  enter:run  q:cancel"
	default:
		return "j/k:nav  enter:view  r:run  e:edit  R:refresh"
	}
}
