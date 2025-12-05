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

// CommandsView displays available RCC commands
type CommandsView struct {
	styles      *Styles
	width       int
	height      int
	categories  []CommandCategory
	selectedCat int
	selectedCmd int
	inCategory  bool
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
				{Name: "task", Description: "Run task from robot.yaml", Usage: "rcc task script -r robot.yaml"},
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
				{Name: "list", Description: "List environments", Usage: "rcc holotree list"},
				{Name: "catalogs", Description: "List catalogs", Usage: "rcc holotree catalogs"},
				{Name: "variables", Description: "Show env variables", Usage: "rcc holotree variables"},
				{Name: "check", Description: "Check integrity", Usage: "rcc holotree check"},
				{Name: "delete", Description: "Delete a space", Usage: "rcc holotree delete"},
				{Name: "export", Description: "Export environment", Usage: "rcc holotree export"},
				{Name: "import", Description: "Import environment", Usage: "rcc holotree import"},
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
			},
		},
		{
			Name:     "Diagnostics",
			Expanded: false,
			Commands: []CommandInfo{
				{Name: "diag", Description: "Run full diagnostics", Usage: "rcc configure diagnostics"},
				{Name: "netdiag", Description: "Network diagnostics", Usage: "rcc configure netdiag"},
				{Name: "speed", Description: "Speed test", Usage: "rcc configure speed"},
			},
		},
		{
			Name:     "Cloud",
			Expanded: false,
			Commands: []CommandInfo{
				{Name: "authorize", Description: "Authorize with cloud", Usage: "rcc cloud authorize"},
				{Name: "push", Description: "Push to cloud", Usage: "rcc cloud push"},
				{Name: "pull", Description: "Pull from cloud", Usage: "rcc pull"},
			},
		},
		{
			Name:     "Utilities",
			Expanded: false,
			Commands: []CommandInfo{
				{Name: "version", Description: "Show version info", Usage: "rcc version"},
				{Name: "man", Description: "Manual pages", Usage: "rcc man"},
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
			v.inCategory = false
		}
	} else {
		if v.selectedCat > 0 {
			v.selectedCat--
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
		cat.Expanded = !cat.Expanded
		if cat.Expanded && len(cat.Commands) > 0 {
			v.inCategory = true
			v.selectedCmd = 0
		}
		return nil
	}
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
	theme := v.styles.theme
	vs := NewViewStyles(theme)

	// Dynamic box sizing
	boxWidth := v.width - 8
	if boxWidth < 70 {
		boxWidth = 70
	}
	if boxWidth > 120 {
		boxWidth = 120
	}
	contentWidth := boxWidth - 6

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 2).
		Width(boxWidth)

	var b strings.Builder

	// Header with RCC version
	totalCmds := 0
	for _, cat := range v.categories {
		totalCmds += len(cat.Commands)
	}
	b.WriteString(RenderHeader(vs, "Commands", fmt.Sprintf("(%d available)", totalCmds), contentWidth))
	b.WriteString("\n")

	// Category list
	for catIdx, cat := range v.categories {
		isSelectedCat := catIdx == v.selectedCat && !v.inCategory

		// Category header with expand indicator
		expandIcon := "+"
		if cat.Expanded {
			expandIcon = "-"
		}

		catHeader := fmt.Sprintf("[%s] %s", expandIcon, cat.Name)
		cmdCount := fmt.Sprintf(" (%d)", len(cat.Commands))

		if isSelectedCat {
			b.WriteString(vs.Selected.Render(catHeader))
			b.WriteString(vs.Subtext.Render(cmdCount))
		} else {
			b.WriteString(vs.Normal.Render(catHeader))
			b.WriteString(vs.Subtext.Render(cmdCount))
		}
		b.WriteString("\n")

		// Commands if expanded
		if cat.Expanded {
			for cmdIdx, cmd := range cat.Commands {
				isSelectedCmd := catIdx == v.selectedCat && v.inCategory && cmdIdx == v.selectedCmd

				// Indent and format command
				if isSelectedCmd {
					b.WriteString("  ")
					b.WriteString(vs.BadgeActive.Render(cmd.Name))
					b.WriteString(" ")
					b.WriteString(vs.Text.Render(cmd.Description))
				} else {
					b.WriteString("  ")
					b.WriteString(vs.Badge.Render(cmd.Name))
					b.WriteString(" ")
					b.WriteString(vs.Subtext.Render(cmd.Description))
				}
				b.WriteString("\n")
			}
		}
	}

	// Selected command detail panel
	if cmd := v.GetSelectedCommand(); cmd != nil {
		b.WriteString("\n")
		b.WriteString(vs.Separator.Render(strings.Repeat("─", contentWidth)))
		b.WriteString("\n\n")

		b.WriteString(vs.Accent.Bold(true).Render("Selected Command"))
		b.WriteString("\n\n")

		b.WriteString(vs.Label.Render("Command"))
		b.WriteString(vs.Accent.Render(cmd.Name))
		b.WriteString("\n")

		b.WriteString(vs.Label.Render("Description"))
		b.WriteString(vs.Text.Render(cmd.Description))
		b.WriteString("\n")

		b.WriteString(vs.Label.Render("Usage"))
		b.WriteString(vs.Info.Render(cmd.Usage))
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("\n")
	hints := []KeyHint{
		{"j/k", "nav"},
		{"l/h", "expand"},
		{"Enter", "run"},
		{"g/G", "top/bot"},
	}
	b.WriteString(RenderFooter(vs, hints, contentWidth))

	return lipgloss.Place(v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		boxStyle.Render(b.String()),
	)
}

// Name implements View
func (v *CommandsView) Name() string {
	return "Commands"
}

// ShortHelp implements View
func (v *CommandsView) ShortHelp() string {
	return "j/k:nav l/h:expand enter:run"
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
