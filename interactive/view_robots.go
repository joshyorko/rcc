package interactive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joshyorko/rcc/robot"
)

type robotData struct {
	name      string
	path      string
	directory string
	tasks     []string
	envFiles  []string
}

// Focus states
const (
	focusRobotList = 0
	focusTaskRow   = 1
	focusEnvRow    = 2
)

type RobotsView struct {
	styles  *Styles
	robots  []robotData
	width   int
	height  int
	loading bool
	err     error

	// Navigation state
	robotIdx int // Which robot is selected
	focus    int // 0=robot list, 1=task row, 2=env row
	taskIdx  int // Which task is selected
	envIdx   int // Which env is selected (-1 = none)
}

func NewRobotsView(styles *Styles) *RobotsView {
	return &RobotsView{
		styles:   styles,
		robots:   []robotData{},
		width:    120,
		height:   30,
		loading:  true,
		robotIdx: 0,
		focus:    focusRobotList,
		taskIdx:  0,
		envIdx:   -1,
	}
}

func (v *RobotsView) Init() tea.Cmd {
	return v.scanForRobots
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

func (v *RobotsView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case robotsLoadedMsg:
		v.loading = false
		v.err = msg.err
		v.robots = msg.robots
		v.robotIdx = 0
		v.focus = focusRobotList
		v.taskIdx = 0
		v.envIdx = -1

	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			v.moveUp()
		case "down", "j":
			v.moveDown()
		case "left", "h":
			v.moveLeft()
		case "right", "l":
			v.moveRight()
		case "R":
			v.loading = true
			return v, v.scanForRobots
		case "esc":
			v.focus = focusRobotList
		case "enter":
			if r := v.selectedRobot(); r != nil {
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
				return v, func() tea.Msg { return actionMsg{action: action} }
			}
		}

	case viewChangedMsg:
		if msg.to == ViewRobots {
			v.loading = true
			return v, v.scanForRobots
		}
	}

	return v, nil
}

func (v *RobotsView) moveUp() {
	switch v.focus {
	case focusRobotList:
		if v.robotIdx > 0 {
			v.robotIdx--
			v.resetSelections()
		}
	case focusTaskRow:
		v.focus = focusRobotList
	case focusEnvRow:
		v.focus = focusTaskRow
	}
}

func (v *RobotsView) moveDown() {
	switch v.focus {
	case focusRobotList:
		if v.robotIdx < len(v.robots)-1 {
			v.robotIdx++
			v.resetSelections()
		} else if len(v.robots) > 0 {
			// At bottom of robot list, move to task row
			v.focus = focusTaskRow
		}
	case focusTaskRow:
		r := v.selectedRobot()
		if r != nil && len(r.envFiles) > 0 {
			v.focus = focusEnvRow
		}
	case focusEnvRow:
		// Already at bottom, do nothing
	}
}

func (v *RobotsView) moveLeft() {
	r := v.selectedRobot()
	if r == nil {
		return
	}

	switch v.focus {
	case focusTaskRow:
		if len(r.tasks) > 0 {
			v.taskIdx--
			if v.taskIdx < 0 {
				v.taskIdx = len(r.tasks) - 1
			}
		}
	case focusEnvRow:
		// -1 = none, 0 to len-1 = env files
		v.envIdx--
		if v.envIdx < -1 {
			v.envIdx = len(r.envFiles) - 1
		}
	}
}

func (v *RobotsView) moveRight() {
	r := v.selectedRobot()
	if r == nil {
		return
	}

	switch v.focus {
	case focusTaskRow:
		if len(r.tasks) > 0 {
			v.taskIdx = (v.taskIdx + 1) % len(r.tasks)
		}
	case focusEnvRow:
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

func (v *RobotsView) View() string {
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

	// Empty state
	if len(v.robots) == 0 {
		b.WriteString(vs.Subtext.Render("No robots found in current directory"))
		b.WriteString("\n\n")
		b.WriteString(vs.Label.Render("Create one"))
		b.WriteString(vs.Info.Render("rcc robot init"))
		return v.placeBox(boxStyle.Render(b.String()))
	}

	// Robot list section
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

	// Separator before config
	b.WriteString("\n")
	b.WriteString(vs.Separator.Render(strings.Repeat("-", contentWidth)))
	b.WriteString("\n\n")

	// Config section for selected robot
	r := v.selectedRobot()
	if r != nil {
		// Task row
		taskPrefix := "  "
		taskStyle := vs.Normal
		if v.focus == focusTaskRow {
			taskPrefix = "> "
			taskStyle = vs.Selected
		}
		b.WriteString(taskStyle.Render(taskPrefix))
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

		// Env row (only show if there are env files)
		if len(r.envFiles) > 0 {
			envPrefix := "  "
			envStyle := vs.Normal
			if v.focus == focusEnvRow {
				envPrefix = "> "
				envStyle = vs.Selected
			}
			b.WriteString(envStyle.Render(envPrefix))
			b.WriteString(vs.Label.Render("Env     "))

			// None option
			if v.envIdx == -1 {
				b.WriteString(vs.BadgeActive.Render("[none]"))
			} else {
				b.WriteString(vs.Badge.Render(" none "))
			}
			b.WriteString(" ")

			// Env files (limit display)
			maxEnvShow := 3
			for i, envFile := range r.envFiles {
				if i >= maxEnvShow {
					b.WriteString(vs.Subtext.Render(fmt.Sprintf("+%d", len(r.envFiles)-maxEnvShow)))
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
	}

	// Command preview separator
	b.WriteString("\n")
	b.WriteString(vs.Separator.Render(strings.Repeat("-", contentWidth)))
	b.WriteString("\n\n")

	// Command preview
	if r != nil {
		cmd := v.buildCommandPreview(r)
		b.WriteString(vs.Info.Render("  " + cmd))
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("\n")
	hints := []KeyHint{
		{"Enter", "run"},
		{"Arrows", "navigate"},
		{"R", "refresh"},
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
	return "Arrows:navigate Enter:run R:refresh"
}
