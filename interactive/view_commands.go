package interactive

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// CommandNode represents a command in the tree
type CommandNode struct {
	Name        string
	Description string
	Use         string
	Children    []*CommandNode
	Expanded    bool
}

// CommandsView displays the command tree
type CommandsView struct {
	styles   *Styles
	root     *CommandNode
	items    []*flatItem // flattened visible items
	cursor   int
	selected *CommandNode
}

type flatItem struct {
	node  *CommandNode
	depth int
}

// NewCommandsView creates a new commands view
func NewCommandsView(styles *Styles) *CommandsView {
	v := &CommandsView{
		styles: styles,
		root:   buildCommandTree(),
	}
	v.flatten()
	return v
}

// buildCommandTree builds the command tree from rcc commands
func buildCommandTree() *CommandNode {
	return &CommandNode{
		Name:        "rcc",
		Description: "RCC command-line tool",
		Expanded:    true,
		Children: []*CommandNode{
			{
				Name:        "robot",
				Description: "Robot management",
				Expanded:    true,
				Children: []*CommandNode{
					{Name: "run", Description: "Run a robot task", Use: "rcc robot run -r robot.yaml -t task"},
					{Name: "init", Description: "Initialize a new robot", Use: "rcc robot init"},
					{Name: "bundle", Description: "Create a self-contained bundle", Use: "rcc robot bundle -r robot.yaml -o bundle.py"},
					{Name: "test", Description: "Run robot tests", Use: "rcc robot test"},
					{Name: "unwrap", Description: "Unwrap a robot package", Use: "rcc robot unwrap package.zip"},
				},
			},
			{
				Name:        "holotree",
				Description: "Environment management",
				Children: []*CommandNode{
					{Name: "variables", Description: "Show environment variables", Use: "rcc holotree variables -r robot.yaml"},
					{Name: "catalogs", Description: "List environment catalogs", Use: "rcc holotree catalogs"},
					{Name: "export", Description: "Export environment to zip", Use: "rcc holotree export -z export.zip"},
					{Name: "import", Description: "Import environment from zip", Use: "rcc holotree import -z import.zip"},
					{Name: "delete", Description: "Delete environment", Use: "rcc holotree delete <hash>"},
					{Name: "venv", Description: "Create virtual environment", Use: "rcc holotree venv -r robot.yaml"},
					{Name: "check", Description: "Check environment integrity", Use: "rcc holotree check"},
					{Name: "prebuild", Description: "Prebuild environment", Use: "rcc holotree prebuild -r robot.yaml"},
				},
			},
			{
				Name:        "run",
				Description: "Quick run (shortcut)",
				Use:         "rcc run -r robot.yaml -t task",
			},
			{
				Name:        "interactive",
				Description: "Launch this TUI (current)",
				Use:         "rcc interactive",
			},
			{
				Name:        "configure",
				Description: "Configuration",
				Children: []*CommandNode{
					{Name: "identity", Description: "Configure identity", Use: "rcc configure identity"},
					{Name: "settings", Description: "View/modify settings", Use: "rcc configure settings"},
					{Name: "credentials", Description: "Manage credentials", Use: "rcc configure credentials"},
				},
			},
			{
				Name:        "cloud",
				Description: "Cloud interaction",
				Children: []*CommandNode{
					{Name: "authorize", Description: "Authorize with cloud", Use: "rcc cloud authorize"},
					{Name: "push", Description: "Push to cloud", Use: "rcc cloud push"},
					{Name: "pull", Description: "Pull from cloud", Use: "rcc cloud pull"},
				},
			},
			{
				Name:        "task",
				Description: "Task management",
				Children: []*CommandNode{
					{Name: "run", Description: "Run a task", Use: "rcc task run"},
					{Name: "script", Description: "Run a script task", Use: "rcc task script"},
				},
			},
			{
				Name:        "diagnostics",
				Description: "System diagnostics",
				Use:         "rcc diagnostics",
			},
			{
				Name:        "version",
				Description: "Show version",
				Use:         "rcc version",
			},
		},
	}
}

// flatten creates a flat list of visible items
func (v *CommandsView) flatten() {
	v.items = nil
	v.flattenNode(v.root, 0)
}

func (v *CommandsView) flattenNode(node *CommandNode, depth int) {
	if node == nil {
		return
	}
	v.items = append(v.items, &flatItem{node: node, depth: depth})
	if node.Expanded {
		for _, child := range node.Children {
			v.flattenNode(child, depth+1)
		}
	}
}

// Init implements View
func (v *CommandsView) Init() tea.Cmd {
	return nil
}

// Update implements View
func (v *CommandsView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Down):
			if v.cursor < len(v.items)-1 {
				v.cursor++
			}
		case key.Matches(msg, keys.Up):
			if v.cursor > 0 {
				v.cursor--
			}
		case key.Matches(msg, keys.Right), key.Matches(msg, keys.Select):
			if v.cursor < len(v.items) {
				item := v.items[v.cursor]
				if len(item.node.Children) > 0 {
					item.node.Expanded = true
					v.flatten()
				} else {
					v.selected = item.node
				}
			}
		case key.Matches(msg, keys.Left):
			if v.cursor < len(v.items) {
				item := v.items[v.cursor]
				if item.node.Expanded && len(item.node.Children) > 0 {
					item.node.Expanded = false
					v.flatten()
				}
			}
		case key.Matches(msg, keys.Top):
			v.cursor = 0
		case key.Matches(msg, keys.Bottom):
			v.cursor = len(v.items) - 1
		}
	}
	return v, nil
}

// View implements View
func (v *CommandsView) View() string {
	var b strings.Builder

	b.WriteString(v.styles.Subtitle.Render("Command Tree"))
	b.WriteString("\n")
	b.WriteString(v.styles.Subtle.Render("Navigate with j/k, expand/collapse with l/h"))
	b.WriteString("\n\n")

	for i, item := range v.items {
		// Indentation
		indent := strings.Repeat("  ", item.depth)

		// Tree branch character
		branch := ""
		if item.depth > 0 {
			if len(item.node.Children) > 0 {
				if item.node.Expanded {
					branch = "▼ "
				} else {
					branch = "▶ "
				}
			} else {
				branch = "  "
			}
		}

		// Name and description
		name := item.node.Name
		desc := item.node.Description

		var line string
		if i == v.cursor {
			line = v.styles.ListItemSelected.Render(indent + branch + name)
			if desc != "" {
				line += " " + v.styles.Subtle.Render(desc)
			}
		} else {
			if len(item.node.Children) > 0 {
				line = v.styles.TreeBranch.Render(indent+branch) + v.styles.Subtitle.Render(name)
			} else {
				line = v.styles.TreeBranch.Render(indent+branch) + v.styles.TreeLeaf.Render(name)
			}
			if desc != "" {
				line += " " + v.styles.ListItemDesc.Render(desc)
			}
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	// Show selected command details
	if v.selected != nil && v.selected.Use != "" {
		b.WriteString("\n")
		b.WriteString(v.styles.Panel.Render(
			v.styles.PanelTitle.Render("Selected Command") + "\n\n" +
				v.styles.HelpKey.Render("Usage: ") + v.styles.Highlight.Render(v.selected.Use),
		))
	}

	return b.String()
}

// Name implements View
func (v *CommandsView) Name() string {
	return "Commands"
}

// ShortHelp implements View
func (v *CommandsView) ShortHelp() string {
	return "j/k:nav  l/h:expand  enter:select"
}
