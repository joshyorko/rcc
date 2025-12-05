package interactive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joshyorko/rcc/robot"
)

type robotData struct {
	name      string
	path      string
	directory string
	tasks     []string
}

type RobotsView struct {
	styles     *Styles
	robots     []robotData
	width      int
	height     int
	cursor     int
	taskCursor int
	loading    bool
	err        error
}

func NewRobotsView(styles *Styles) *RobotsView {
	return &RobotsView{
		styles:  styles,
		robots:  []robotData{},
		width:   120,
		height:  30,
		loading: true,
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

			robots = append(robots, item)
		}

		return nil
	})

	return robotsLoadedMsg{robots: robots}
}

func (v *RobotsView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case robotsLoadedMsg:
		v.loading = false
		v.err = msg.err
		v.robots = msg.robots
		v.cursor = 0
		v.taskCursor = 0

	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if v.cursor < len(v.robots)-1 {
				v.cursor++
				v.taskCursor = 0
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
				v.taskCursor = 0
			}
		case "l", "right", "tab":
			if r := v.selectedRobot(); r != nil && len(r.tasks) > 1 {
				v.taskCursor = (v.taskCursor + 1) % len(r.tasks)
			}
		case "h", "left":
			if r := v.selectedRobot(); r != nil && len(r.tasks) > 1 {
				v.taskCursor--
				if v.taskCursor < 0 {
					v.taskCursor = len(r.tasks) - 1
				}
			}
		case "g":
			v.cursor = 0
			v.taskCursor = 0
		case "G":
			if len(v.robots) > 0 {
				v.cursor = len(v.robots) - 1
				v.taskCursor = 0
			}
		case "R":
			v.loading = true
			return v, v.scanForRobots
		case "r", "enter":
			if r := v.selectedRobot(); r != nil {
				action := ActionResult{
					Type:      ActionRunRobot,
					RobotPath: r.path,
				}
				if len(r.tasks) > 0 {
					action.RobotTask = r.tasks[v.taskCursor]
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

func (v *RobotsView) selectedRobot() *robotData {
	if v.cursor >= 0 && v.cursor < len(v.robots) {
		return &v.robots[v.cursor]
	}
	return nil
}

func (v *RobotsView) View() string {
	s := v.styles
	var content strings.Builder

	// Main container using Panel style
	panelStyle := s.Panel.Width(min(v.width-4, 70))

	// Loading state
	if v.loading {
		content.WriteString(s.Subtle.Render("[...] Scanning for robots..."))
		return panelStyle.Render(content.String())
	}

	// Error state
	if v.err != nil {
		content.WriteString(s.Error.Render("[!] Error: " + v.err.Error()))
		return panelStyle.Render(content.String())
	}

	// Empty state
	if len(v.robots) == 0 {
		content.WriteString(s.Subtle.Render("No robots found in current directory\n\n"))
		content.WriteString(s.Subtle.Render("Initialize a new robot:\n"))
		content.WriteString(s.Info.Render("  $ rcc robot init"))
		return panelStyle.Render(content.String())
	}

	// Header with count
	content.WriteString(s.PanelTitle.Render(fmt.Sprintf("ROBOTS [%d]", len(v.robots))))
	content.WriteString("\n")
	content.WriteString(s.Divider.Render(strings.Repeat("─", 50)))
	content.WriteString("\n\n")

	// Robot list
	for i, r := range v.robots {
		isSelected := i == v.cursor

		// Get relative path
		relPath, _ := filepath.Rel(".", r.directory)
		if relPath == "" || relPath == "." {
			relPath = "./"
		}

		// Truncate name if needed
		name := r.name
		if len(name) > 30 {
			name = name[:27] + "..."
		}

		// Build the row
		if isSelected {
			// Selected: use ListItemSelected style
			nameStr := "> " + name
			padded := fmt.Sprintf("%-35s", nameStr)
			content.WriteString(s.ListItemSelected.Render(padded))
			content.WriteString(" ")
			content.WriteString(s.Info.Render(relPath))
		} else {
			// Normal row
			content.WriteString(s.ListItem.Render(name))
			spaces := strings.Repeat(" ", max(1, 33-len(name)))
			content.WriteString(spaces)
			content.WriteString(s.Subtle.Render(relPath))
		}
		content.WriteString("\n")

		// Show tasks for selected robot
		if isSelected && len(r.tasks) > 0 {
			content.WriteString("\n")
			content.WriteString(s.Subtle.Render("  Tasks: "))
			for j, task := range r.tasks {
				if j == v.taskCursor {
					content.WriteString(s.ActiveTab.Render(task))
				} else {
					content.WriteString(s.Tab.Render(task))
				}
			}
			if len(r.tasks) > 1 {
				content.WriteString(s.Subtle.Render("  [h/l]"))
			}
			content.WriteString("\n")
		}
	}

	// Footer with action hint
	content.WriteString("\n")
	content.WriteString(s.Divider.Render(strings.Repeat("─", 50)))
	content.WriteString("\n")
	if r := v.selectedRobot(); r != nil && len(r.tasks) > 0 {
		content.WriteString(s.HelpKey.Render("[Enter]"))
		content.WriteString(s.HelpDesc.Render(" Run "))
		content.WriteString(s.Success.Render(r.tasks[v.taskCursor]))
		content.WriteString("  ")
		content.WriteString(s.HelpKey.Render("[R]"))
		content.WriteString(s.HelpDesc.Render(" Refresh"))
	} else {
		content.WriteString(s.HelpKey.Render("[R]"))
		content.WriteString(s.HelpDesc.Render(" Refresh  "))
		content.WriteString(s.HelpKey.Render("[j/k]"))
		content.WriteString(s.HelpDesc.Render(" Navigate"))
	}

	return panelStyle.Render(content.String())
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (v *RobotsView) Name() string {
	return "Robots"
}

func (v *RobotsView) ShortHelp() string {
	return "j/k:nav h/l:task r:run R:refresh"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
