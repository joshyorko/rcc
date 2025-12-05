package interactive

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// CommandInfo represents a single command
type CommandInfo struct {
	Name        string
	Description string
	Usage       string
}

// CommandCategory represents a group of related commands
type CommandCategory struct {
	Name     string
	Commands []CommandInfo
	Expanded bool
}

// CommandsView displays available RCC commands in a tree structure
type CommandsView struct {
	styles      *Styles
	width       int
	height      int
	categories  []CommandCategory
	selectedCat int
	selectedCmd int
	inCategory  bool // true when navigating within a category
}

// NewCommandsView creates a new commands view
func NewCommandsView(styles *Styles) *CommandsView {
	return &CommandsView{
		styles:      styles,
		width:       120,
		height:      30,
		categories:  getCommandCategories(),
		selectedCat: 0,
		selectedCmd: 0,
		inCategory:  false,
	}
}

// getCommandCategories returns the organized RCC command structure
func getCommandCategories() []CommandCategory {
	return []CommandCategory{
		{
			Name:     "Robot",
			Expanded: true,
			Commands: []CommandInfo{
				{Name: "run", Description: "Run a robot task", Usage: "rcc run -r robot.yaml -t task"},
				{Name: "task", Description: "Run a specific task from robot.yaml", Usage: "rcc task script -r robot.yaml"},
				{Name: "testrun", Description: "Test run a robot", Usage: "rcc robot testrun"},
				{Name: "init", Description: "Initialize robot structure", Usage: "rcc robot init"},
				{Name: "bundle", Description: "Create robot bundle", Usage: "rcc robot bundle"},
				{Name: "unwrap", Description: "Unwrap a robot bundle", Usage: "rcc robot unwrap"},
			},
		},
		{
			Name:     "Holotree",
			Expanded: false,
			Commands: []CommandInfo{
				{Name: "list", Description: "List holotree environments", Usage: "rcc holotree list"},
				{Name: "catalogs", Description: "List holotree catalogs", Usage: "rcc holotree catalogs"},
				{Name: "variables", Description: "Show environment variables", Usage: "rcc holotree variables"},
				{Name: "check", Description: "Check holotree integrity", Usage: "rcc holotree check"},
				{Name: "delete", Description: "Delete a holotree space", Usage: "rcc holotree delete"},
				{Name: "venv", Description: "Create virtual environment", Usage: "rcc holotree venv"},
				{Name: "export", Description: "Export environment", Usage: "rcc holotree export"},
				{Name: "import", Description: "Import environment", Usage: "rcc holotree import"},
				{Name: "init", Description: "Initialize holotree", Usage: "rcc holotree init"},
				{Name: "prebuild", Description: "Prebuild environment", Usage: "rcc holotree prebuild"},
			},
		},
		{
			Name:     "Configuration",
			Expanded: false,
			Commands: []CommandInfo{
				{Name: "configure", Description: "Configuration settings", Usage: "rcc configure"},
				{Name: "settings", Description: "Show/modify settings", Usage: "rcc configure settings"},
				{Name: "credentials", Description: "Manage credentials", Usage: "rcc configure credentials"},
				{Name: "identity", Description: "Show identity info", Usage: "rcc configure identity"},
				{Name: "profile", Description: "Configure profile", Usage: "rcc configure profile"},
			},
		},
		{
			Name:     "Diagnostics",
			Expanded: false,
			Commands: []CommandInfo{
				{Name: "diag", Description: "Run full diagnostics", Usage: "rcc configure diagnostics"},
				{Name: "netdiag", Description: "Network diagnostics", Usage: "rcc configure netdiag"},
				{Name: "speed", Description: "Speed test", Usage: "rcc configure speed"},
				{Name: "longpaths", Description: "Check long paths support", Usage: "rcc configure longpaths"},
			},
		},
		{
			Name:     "Cloud",
			Expanded: false,
			Commands: []CommandInfo{
				{Name: "authorize", Description: "Authorize with cloud", Usage: "rcc cloud authorize"},
				{Name: "push", Description: "Push to cloud", Usage: "rcc cloud push"},
				{Name: "pull", Description: "Pull from cloud", Usage: "rcc pull"},
				{Name: "workspace", Description: "Workspace operations", Usage: "rcc cloud workspace"},
			},
		},
		{
			Name:     "Utilities",
			Expanded: false,
			Commands: []CommandInfo{
				{Name: "version", Description: "Show version info", Usage: "rcc version"},
				{Name: "man", Description: "Manual pages", Usage: "rcc man"},
				{Name: "feedback", Description: "Send feedback", Usage: "rcc feedback"},
				{Name: "interactive", Description: "Interactive mode", Usage: "rcc interactive"},
				{Name: "ui", Description: "Launch TUI dashboard", Usage: "rcc ui"},
			},
		},
	}
}

// Init implements View
func (v *CommandsView) Init() tea.Cmd {
	return nil
}

// Update implements View
func (v *CommandsView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			v.moveDown()
		case "k", "up":
			v.moveUp()
		case "l", "right", "enter":
			if cmd := v.expandOrEnter(); cmd != nil {
				return v, cmd
			}
		case "h", "left":
			v.collapseOrBack()
		case "g":
			v.selectedCat = 0
			v.selectedCmd = 0
			v.inCategory = false
		case "G":
			v.selectedCat = len(v.categories) - 1
			v.selectedCmd = 0
			v.inCategory = false
		}
	}
	return v, nil
}

func (v *CommandsView) moveDown() {
	if v.inCategory {
		cat := &v.categories[v.selectedCat]
		if v.selectedCmd < len(cat.Commands)-1 {
			v.selectedCmd++
		} else {
			// Move to next category
			v.inCategory = false
			if v.selectedCat < len(v.categories)-1 {
				v.selectedCat++
			}
			v.selectedCmd = 0
		}
	} else {
		if v.selectedCat < len(v.categories)-1 {
			v.selectedCat++
		}
	}
}

func (v *CommandsView) moveUp() {
	if v.inCategory {
		if v.selectedCmd > 0 {
			v.selectedCmd--
		} else {
			// Back to category header
			v.inCategory = false
		}
	} else {
		if v.selectedCat > 0 {
			v.selectedCat--
			// If previous category is expanded, go to its last command
			if v.categories[v.selectedCat].Expanded {
				v.inCategory = true
				v.selectedCmd = len(v.categories[v.selectedCat].Commands) - 1
			}
		}
	}
}

func (v *CommandsView) expandOrEnter() tea.Cmd {
	cat := &v.categories[v.selectedCat]
	if !v.inCategory {
		// Toggle expand/collapse of category
		cat.Expanded = !cat.Expanded
		if cat.Expanded && len(cat.Commands) > 0 {
			v.inCategory = true
			v.selectedCmd = 0
		}
		return nil
	}
	// Execute the selected command
	if cmd := v.GetSelectedCommand(); cmd != nil {
		action := ActionResult{
			Type:    ActionRunCommand,
			Command: cmd.Usage,
		}
		return func() tea.Msg { return actionMsg{action: action} }
	}
	return nil
}

func (v *CommandsView) collapseOrBack() {
	if v.inCategory {
		v.inCategory = false
	} else {
		v.categories[v.selectedCat].Expanded = false
	}
}

// View implements View
func (v *CommandsView) View() string {
	var b strings.Builder

	// Calculate available space for layout
	contentHeight := v.height - 8 // Reserve space for header and help
	treeWidth := v.width / 2
	if v.width < 80 {
		treeWidth = v.width - 4
	}
	detailWidth := v.width - treeWidth - 6

	// Title
	title := v.styles.PanelTitle.Render("Commands")
	subtitle := v.styles.Subtle.Render("Browse and execute RCC commands")
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(subtitle)
	b.WriteString("\n\n")

	// Build tree view
	treeContent := v.buildTreeView(treeWidth, contentHeight)

	// Build detail panel for selected command
	var detailContent string
	if cmd := v.GetSelectedCommand(); cmd != nil && v.width >= 80 {
		detailContent = v.buildDetailPanel(cmd, detailWidth, contentHeight)
	}

	// Render side-by-side panels for wide terminals
	if v.width >= 80 && detailContent != "" {
		// Use lipgloss JoinHorizontal to place panels side by side
		treePanelContent := v.styles.Panel.
			Width(treeWidth).
			Height(contentHeight).
			Render(treeContent)

		detailPanelContent := v.styles.Panel.
			Width(detailWidth).
			Height(contentHeight).
			Render(detailContent)

		panels := lipgloss.JoinHorizontal(lipgloss.Top, treePanelContent, " ", detailPanelContent)
		b.WriteString(panels)
	} else {
		// Single panel for narrow terminals
		panelContent := v.styles.Panel.
			Width(v.width - 4).
			Height(contentHeight).
			Render(treeContent)
		b.WriteString(panelContent)
	}

	b.WriteString("\n\n")

	// Help bar
	help := v.buildHelpBar()
	b.WriteString(help)

	return b.String()
}

// buildTreeView renders the command tree structure
func (v *CommandsView) buildTreeView(width, height int) string {
	var b strings.Builder

	for catIdx, cat := range v.categories {
		// Category header
		isSelectedCat := catIdx == v.selectedCat && !v.inCategory
		isLastCat := catIdx == len(v.categories)-1

		// Tree branch character
		branchChar := "+"
		if isLastCat {
			branchChar = "+"
		}

		// Expand/collapse icon
		expandIcon := "[+]"
		if cat.Expanded {
			expandIcon = "[-]"
		}

		// Build category line
		catLine := v.styles.TreeBranch.Render(branchChar+"─") + " "

		if isSelectedCat {
			catLine += v.styles.ListItemSelected.Render(expandIcon + " " + cat.Name)
		} else {
			catLine += v.styles.Title.Render(expandIcon+" ") + v.styles.TreeBranch.Render(cat.Name)
		}

		// Show command count
		cmdCount := v.styles.Subtle.Render(fmt.Sprintf(" (%d)", len(cat.Commands)))
		catLine += cmdCount

		b.WriteString(catLine)
		b.WriteString("\n")

		// Show commands if expanded
		if cat.Expanded {
			for cmdIdx, cmd := range cat.Commands {
				isSelectedCmd := catIdx == v.selectedCat && v.inCategory && cmdIdx == v.selectedCmd

				// Tree structure
				var prefix string
				if !isLastCat {
					prefix = v.styles.TreeBranch.Render("│")
				} else {
					prefix = " "
				}

				// Command branch character
				cmdBranch := "+"

				// Build command line
				cmdLine := prefix + v.styles.TreeBranch.Render("  "+cmdBranch+"─")

				if isSelectedCmd {
					// Selected command with highlight
					cmdLine += " " + v.styles.ListItemSelected.Render("> "+cmd.Name)
				} else {
					// Normal command
					cmdLine += " " + v.styles.TreeLeaf.Render(cmd.Name)
				}

				b.WriteString(cmdLine)
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

// buildDetailPanel renders the detail view for a selected command
func (v *CommandsView) buildDetailPanel(cmd *CommandInfo, width, height int) string {
	var b strings.Builder

	// Title
	title := v.styles.Highlight.Bold(true).Render(cmd.Name)
	b.WriteString(title)
	b.WriteString("\n\n")

	// Description
	descLabel := v.styles.Subtle.Render("Description:")
	b.WriteString(descLabel)
	b.WriteString("\n")
	desc := v.styles.ListItemDesc.Render("  " + cmd.Description)
	b.WriteString(desc)
	b.WriteString("\n\n")

	// Usage
	usageLabel := v.styles.Subtle.Render("Usage:")
	b.WriteString(usageLabel)
	b.WriteString("\n")

	// Usage in a code-like box
	usage := v.styles.Info.
		Background(v.styles.theme.Surface).
		Padding(0, 1).
		Render(cmd.Usage)
	b.WriteString("  " + usage)
	b.WriteString("\n\n")

	// Action hint
	hint := v.styles.Success.Render("Enter") + " " + v.styles.Subtle.Render("to execute")
	b.WriteString(hint)

	return b.String()
}

// buildHelpBar renders the bottom help/shortcuts bar
func (v *CommandsView) buildHelpBar() string {
	keys := []struct {
		key  string
		desc string
	}{
		{"k", "up"},
		{"j", "down"},
		{"l/enter", "expand"},
		{"h", "collapse"},
		{"enter", "execute"},
		{"g", "top"},
		{"G", "bottom"},
	}

	var helpItems []string
	for _, k := range keys {
		item := v.styles.HelpKey.Render(k.key) + " " + v.styles.HelpDesc.Render(k.desc)
		helpItems = append(helpItems, item)
	}

	return v.styles.Subtle.Render(strings.Join(helpItems, "  |  "))
}

// Name implements View
func (v *CommandsView) Name() string {
	return "Commands"
}

// ShortHelp implements View
func (v *CommandsView) ShortHelp() string {
	return "j/k:nav l/h:expand enter:select"
}

// GetSelectedCommand returns the currently selected command info
func (v *CommandsView) GetSelectedCommand() *CommandInfo {
	if v.inCategory && v.selectedCat < len(v.categories) {
		cat := v.categories[v.selectedCat]
		if v.selectedCmd < len(cat.Commands) {
			return &cat.Commands[v.selectedCmd]
		}
	}
	return nil
}
