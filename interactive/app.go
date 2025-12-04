package interactive

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joshyorko/rcc/common"
)

// ViewType represents the different views available
type ViewType int

const (
	ViewHome ViewType = iota
	ViewCommands
	ViewRobots
	ViewEnvironments
	ViewLogs
)

// View interface that all views must implement
type View interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (View, tea.Cmd)
	View() string
	Name() string
	ShortHelp() string
}

// App is the main application model
type App struct {
	activeView ViewType
	views      []View
	width      int
	height     int
	styles     *Styles
	quitting   bool
	showHelp   bool
	spinner    spinner.Model
	startTime  time.Time
}

// NewApp creates a new interactive application
func NewApp() *App {
	styles := NewStyles()

	// Create spinner with nice animation
	s := spinner.New()
	s.Spinner = spinner.Spinner{
		Frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		FPS:    time.Second / 10,
	}
	s.Style = styles.Spinner

	app := &App{
		activeView: ViewHome,
		styles:     styles,
		width:      120,
		height:     30,
		spinner:    s,
		startTime:  time.Now(),
	}

	// Initialize views
	app.views = []View{
		NewHomeView(styles),
		NewCommandsView(styles),
		NewRobotsView(styles),
		NewEnvironmentsView(styles),
		NewLogsView(styles),
	}

	return app
}

// Init implements tea.Model
func (a *App) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, a.spinner.Tick)
	for _, v := range a.views {
		if cmd := v.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// Update implements tea.Model
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Global key bindings
		switch {
		case key.Matches(msg, keys.Quit):
			a.quitting = true
			return a, tea.Quit

		case key.Matches(msg, keys.Help):
			a.showHelp = !a.showHelp
			return a, nil

		case key.Matches(msg, keys.ViewHome):
			a.activeView = ViewHome
			return a, nil

		case key.Matches(msg, keys.ViewCommands):
			a.activeView = ViewCommands
			return a, nil

		case key.Matches(msg, keys.ViewRobots):
			a.activeView = ViewRobots
			return a, nil

		case key.Matches(msg, keys.ViewEnvs):
			a.activeView = ViewEnvironments
			return a, nil

		case key.Matches(msg, keys.ViewLogs):
			a.activeView = ViewLogs
			return a, nil
		}

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

	case spinner.TickMsg:
		a.spinner, cmd = a.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Pass to active view
	if int(a.activeView) < len(a.views) {
		newView, viewCmd := a.views[a.activeView].Update(msg)
		a.views[a.activeView] = newView
		if viewCmd != nil {
			cmds = append(cmds, viewCmd)
		}
	}

	return a, tea.Batch(cmds...)
}

// View implements tea.Model
func (a *App) View() string {
	if a.quitting {
		return ""
	}

	// Layout heights
	headerHeight := 4 // logo + crumbs + divider
	menuHeight := 3   // help menu at bottom
	contentHeight := a.height - headerHeight - menuHeight

	// Build layout sections
	header := a.renderHeader()
	var content string
	if a.showHelp {
		content = a.renderHelp(contentHeight)
	} else {
		content = a.renderContent(contentHeight)
	}
	menu := a.renderMenu()

	return lipgloss.JoinVertical(lipgloss.Left, header, content, menu)
}

func (a *App) renderHeader() string {
	// === Row 1: Logo and Status ===
	logo := a.renderLogo()
	status := a.renderStatus()

	// Calculate gap
	logoWidth := lipgloss.Width(logo)
	statusWidth := lipgloss.Width(status)
	gap := a.width - logoWidth - statusWidth
	if gap < 1 {
		gap = 1
	}

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, logo, strings.Repeat(" ", gap), status)

	// === Row 2: Breadcrumbs ===
	crumbs := a.renderCrumbs()

	// === Row 3: Divider ===
	divider := a.styles.Divider.Render(strings.Repeat("─", a.width))

	return lipgloss.JoinVertical(lipgloss.Left, topRow, crumbs, divider)
}

func (a *App) renderLogo() string {
	// RCC ASCII-ish logo with spinner
	spinnerView := a.spinner.View()
	title := a.styles.LogoText.Render(" RCC ")
	subtitle := a.styles.LogoSubtle.Render("Interactive")

	return lipgloss.JoinHorizontal(lipgloss.Center, spinnerView, title, subtitle)
}

func (a *App) renderStatus() string {
	// Version | Uptime
	elapsed := time.Since(a.startTime).Round(time.Second)

	version := a.styles.StatusKey.Render("ver:") + a.styles.StatusValue.Render(common.Version)
	uptime := a.styles.StatusKey.Render(" up:") + a.styles.StatusValue.Render(elapsed.String())

	return version + uptime + " "
}

func (a *App) renderCrumbs() string {
	// Navigation breadcrumbs like k9s: <rcc> <view>
	viewNames := []string{"Home", "Commands", "Robots", "Envs", "Logs"}
	currentView := viewNames[int(a.activeView)]

	root := a.styles.CrumbInactive.Render(" <rcc> ")
	active := a.styles.CrumbActive.Render(fmt.Sprintf(" <%s> ", strings.ToLower(currentView)))

	return root + active
}

func (a *App) renderContent(height int) string {
	// Render active view content
	content := ""
	if int(a.activeView) < len(a.views) {
		content = a.views[a.activeView].View()
	}

	// Create content box with padding
	contentStyle := lipgloss.NewStyle().
		Width(a.width).
		Height(height).
		PaddingLeft(1).
		PaddingRight(1)

	return contentStyle.Render(content)
}

func (a *App) renderHelp(height int) string {
	var b strings.Builder

	// Header
	header := a.styles.Info.Render("####") + "  " + a.styles.PanelTitle.Render("Help") + "  " + a.styles.Info.Render("####")
	b.WriteString(header)
	b.WriteString("\n\n")

	// Navigation section
	b.WriteString(a.styles.PanelTitle.Render("    Navigation"))
	b.WriteString("\n\n")
	navKeys := []struct{ key, desc string }{
		{"1", "Home - Dashboard view"},
		{"2", "Commands - Browse available commands"},
		{"3", "Robots - View and run detected robots"},
		{"4", "Environments - Manage holotree environments"},
		{"5", "Logs - View logs"},
	}
	for _, k := range navKeys {
		b.WriteString("      " + a.styles.HelpKey.Render("<"+k.key+">") + " " + a.styles.HelpDesc.Render(k.desc) + "\n")
	}
	b.WriteString("\n")

	// Movement section
	b.WriteString(a.styles.PanelTitle.Render("    Movement"))
	b.WriteString("\n\n")
	moveKeys := []struct{ key, desc string }{
		{"j/↓", "Move down"},
		{"k/↑", "Move up"},
		{"h/←", "Collapse / Back"},
		{"l/→", "Expand / Enter"},
		{"g", "Go to top"},
		{"G", "Go to bottom"},
		{"Enter", "Select / Confirm"},
	}
	for _, k := range moveKeys {
		b.WriteString("      " + a.styles.HelpKey.Render("<"+k.key+">") + " " + a.styles.HelpDesc.Render(k.desc) + "\n")
	}
	b.WriteString("\n")

	// Actions section
	b.WriteString(a.styles.PanelTitle.Render("    Actions"))
	b.WriteString("\n\n")
	actionKeys := []struct{ key, desc string }{
		{"r", "Run selected robot"},
		{"e", "Edit file (opens $EDITOR)"},
		{"R", "Refresh current view"},
		{"d", "Delete (with confirmation)"},
		{"/", "Search"},
	}
	for _, k := range actionKeys {
		b.WriteString("      " + a.styles.HelpKey.Render("<"+k.key+">") + " " + a.styles.HelpDesc.Render(k.desc) + "\n")
	}
	b.WriteString("\n")

	// Global section
	b.WriteString(a.styles.PanelTitle.Render("    Global"))
	b.WriteString("\n\n")
	globalKeys := []struct{ key, desc string }{
		{"?", "Toggle this help"},
		{"q", "Quit"},
		{"Ctrl+C", "Force quit"},
	}
	for _, k := range globalKeys {
		b.WriteString("      " + a.styles.HelpKey.Render("<"+k.key+">") + " " + a.styles.HelpDesc.Render(k.desc) + "\n")
	}

	// Create content box with padding
	contentStyle := lipgloss.NewStyle().
		Width(a.width).
		Height(height).
		PaddingLeft(1).
		PaddingRight(1)

	return contentStyle.Render(b.String())
}

func (a *App) renderMenu() string {
	// Divider above menu
	divider := a.styles.Divider.Render(strings.Repeat("─", a.width))

	// Build help hints in a grid like k9s
	hints := a.buildHints()

	return lipgloss.JoinVertical(lipgloss.Left, divider, hints)
}

func (a *App) buildHints() string {
	// Global hints
	globalHints := []struct {
		key  string
		desc string
	}{
		{"1", "Home"},
		{"2", "Cmds"},
		{"3", "Robots"},
		{"4", "Envs"},
		{"5", "Logs"},
		{"?", "Help"},
		{"q", "Quit"},
	}

	// View-specific hints based on active view
	viewHints := []struct {
		key  string
		desc string
	}{}

	switch a.activeView {
	case ViewHome:
		viewHints = append(viewHints, struct{ key, desc string }{"r", "Run"})
	case ViewCommands:
		viewHints = append(viewHints,
			struct{ key, desc string }{"j/k", "Nav"},
			struct{ key, desc string }{"l/h", "Expand"},
			struct{ key, desc string }{"enter", "Select"},
		)
	case ViewRobots:
		viewHints = append(viewHints,
			struct{ key, desc string }{"j/k", "Nav"},
			struct{ key, desc string }{"R", "Refresh"},
			struct{ key, desc string }{"r", "Run"},
		)
	case ViewEnvironments:
		viewHints = append(viewHints,
			struct{ key, desc string }{"j/k", "Nav"},
			struct{ key, desc string }{"tab", "Switch"},
			struct{ key, desc string }{"d", "Delete"},
			struct{ key, desc string }{"c", "Check"},
		)
	case ViewLogs:
		viewHints = append(viewHints,
			struct{ key, desc string }{"j/k", "Scroll"},
			struct{ key, desc string }{"g/G", "Top/Bot"},
			struct{ key, desc string }{"c", "Clear"},
		)
	}

	// Combine and format hints
	var parts []string
	for _, h := range viewHints {
		parts = append(parts, a.formatHint(h.key, h.desc))
	}
	parts = append(parts, a.styles.MenuSeparator.Render(" │ "))
	for _, h := range globalHints {
		parts = append(parts, a.formatHint(h.key, h.desc))
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
}

func (a *App) formatHint(key, desc string) string {
	k := a.styles.MenuKey.Render("<" + key + ">")
	d := a.styles.MenuDesc.Render(desc)
	return k + d + " "
}

// Run starts the interactive application
func Run() error {
	p := tea.NewProgram(
		NewApp(),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	_, err := p.Run()
	return err
}
