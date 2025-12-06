package interactive

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joshyorko/rcc/common"
)

// ViewType represents the different views available in the TUI
type ViewType int

const (
	ViewHome ViewType = iota
	ViewCommands
	ViewRobots
	ViewEnvironments
	ViewHistory
	ViewDiagnostics
	ViewLogs
	ViewRemote
)

// ActionType represents the type of action to perform after TUI exits
type ActionType int

const (
	ActionNone ActionType = iota
	ActionRunRobot
	ActionRunCommand
	ActionDeleteEnv
	ActionExportCatalog
	ActionImportCatalog
	ActionCheckIntegrity
	ActionToggleServer
)

// ActionResult holds the result of a TUI action selection
type ActionResult struct {
	Type         ActionType
	RobotPath    string   // For ActionRunRobot
	RobotTask    string   // For ActionRunRobot (optional)
	EnvFile      string   // For ActionRunRobot - environment JSON file (optional)
	Command      string   // For ActionRunCommand
	EnvID        string   // For ActionDeleteEnv, ActionExportCatalog
	OutputPath   string   // For ActionExportCatalog
	InputPath    string   // For ActionImportCatalog
	ReturnToView ViewType // View to return to after action completes
}

// actionMsg is sent when user triggers an action
type actionMsg struct {
	action ActionResult
}

// confirmMsg is used for confirmation dialogs
type confirmMsg struct {
	confirmed bool
	action    ActionResult
}

// View interface that all views must implement
type View interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (View, tea.Cmd)
	View() string
	Name() string
	ShortHelp() string
}

// App is the main application model for the interactive TUI
type App struct {
	activeView    ViewType
	views         []View
	width         int
	height        int
	styles        *Styles
	quitting      bool
	showHelp      bool
	startTime     time.Time
	pendingAction *ActionResult
	showConfirm   bool
	confirmPrompt string
	activeToast   *Toast
	nextToastID   int64
}

// NewApp creates a new interactive application
func NewApp() *App {
	styles := NewStyles()

	app := &App{
		activeView: ViewHome,
		styles:     styles,
		width:      120,
		height:     30,
		startTime:  time.Now(),
	}

	// Initialize views
	app.views = []View{
		NewHomeView(styles),
		NewCommandsView(styles),
		NewRobotsView(styles),
		NewEnvironmentsView(styles),
		NewHistoryView(styles),
		NewDiagnosticsView(styles),
		NewLogsView(styles),
		NewRemoteView(styles),
	}

	return app
}

// Init implements tea.Model
func (a *App) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, v := range a.views {
		if cmd := v.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// viewChangedMsg is sent when the active view changes
type viewChangedMsg struct {
	from ViewType
	to   ViewType
}

// Update implements tea.Model
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Global key bindings first
		if cmd, handled := a.handleGlobalKeys(msg); handled {
			if cmd != nil {
				return a, cmd
			}
			if a.quitting {
				return a, tea.Quit
			}
			return a, nil
		}

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

	case actionMsg:
		return a.handleAction(msg)

	case confirmMsg:
		return a.handleConfirm(msg)

	case ToastMsg:
		return a.handleToast(msg)

	case ToastTimeoutMsg:
		if a.activeToast != nil && a.activeToast.ID == msg.ID {
			a.activeToast = nil
		}
		return a, nil
	}

	// Handle confirmation dialog keys if active
	if a.showConfirm {
		return a.handleConfirmKeys(msg)
	}

	// Dispatch to views
	cmds = append(cmds, a.updateViews(msg)...)

	return a, tea.Batch(cmds...)
}

func (a *App) handleGlobalKeys(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, keys.Quit):
		a.quitting = true
		return tea.Quit, true

	case key.Matches(msg, keys.Help):
		a.showHelp = !a.showHelp
		// If closing help, we don't need to do anything else
		return nil, true
	}

	// View switching
	var targetView ViewType = -1

	switch {
	case key.Matches(msg, keys.ViewHome):
		targetView = ViewHome
	case key.Matches(msg, keys.ViewCommands):
		targetView = ViewCommands
	case key.Matches(msg, keys.ViewRobots):
		targetView = ViewRobots
	case key.Matches(msg, keys.ViewEnvs):
		targetView = ViewEnvironments
	case key.Matches(msg, keys.ViewHistory):
		targetView = ViewHistory
	case key.Matches(msg, keys.ViewDiagnostics):
		targetView = ViewDiagnostics
	case key.Matches(msg, keys.ViewLogs):
		targetView = ViewLogs
	case key.Matches(msg, keys.ViewRemote):
		targetView = ViewRemote
	}

	if targetView != -1 {
		if a.activeView != targetView {
			oldView := a.activeView
			a.activeView = targetView
			return func() tea.Msg { return viewChangedMsg{oldView, targetView} }, true
		}
		return nil, true
	}

	return nil, false
}

func (a *App) handleAction(msg actionMsg) (tea.Model, tea.Cmd) {
	switch msg.action.Type {
	case ActionRunRobot, ActionRunCommand, ActionCheckIntegrity, ActionExportCatalog, ActionImportCatalog:
		// These actions exit TUI and run externally
		a.pendingAction = &msg.action
		a.quitting = true
		return a, tea.Quit
	case ActionDeleteEnv:
		a.showConfirm = true
		a.confirmPrompt = fmt.Sprintf("Delete environment '%s'?", msg.action.EnvID)
		a.pendingAction = &msg.action
		return a, nil
	}
	return a, nil
}

func (a *App) handleConfirm(msg confirmMsg) (tea.Model, tea.Cmd) {
	a.showConfirm = false
	if msg.confirmed && a.pendingAction != nil {
		a.quitting = true
		return a, tea.Quit
	}
	a.pendingAction = nil
	return a, nil
}

func (a *App) handleConfirmKeys(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "y", "Y", "enter":
			return a, func() tea.Msg { return confirmMsg{confirmed: true, action: *a.pendingAction} }
		case "n", "N", "escape", "q":
			return a, func() tea.Msg { return confirmMsg{confirmed: false} }
		}
	}
	return a, nil
}

func (a *App) handleToast(msg ToastMsg) (tea.Model, tea.Cmd) {
	a.nextToastID++
	id := a.nextToastID
	a.activeToast = &Toast{
		ID:        id,
		Type:      msg.Type,
		Message:   msg.Message,
		StartTime: time.Now(),
		Duration:  msg.Duration,
	}
	return a, tea.Tick(msg.Duration, func(t time.Time) tea.Msg {
		return ToastTimeoutMsg{ID: id}
	})
}

func (a *App) updateViews(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd

	// Key messages only go to active view
	if _, isKeyMsg := msg.(tea.KeyMsg); isKeyMsg {
		if int(a.activeView) < len(a.views) {
			newView, viewCmd := a.views[a.activeView].Update(msg)
			a.views[a.activeView] = newView
			if viewCmd != nil {
				cmds = append(cmds, viewCmd)
			}
		}
		return cmds
	}

	// Other messages go to all views
	for i := range a.views {
		newView, viewCmd := a.views[i].Update(msg)
		a.views[i] = newView
		if viewCmd != nil {
			cmds = append(cmds, viewCmd)
		}
	}
	return cmds
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
	if a.showConfirm {
		content = a.renderConfirmDialog(contentHeight)
	} else if a.showHelp {
		content = a.renderHelp(contentHeight)
	} else {
		content = a.renderContent(contentHeight)
	}
	menu := a.renderMenu()

	return lipgloss.JoinVertical(lipgloss.Left, header, content, menu)
}

func (a *App) renderConfirmDialog(height int) string {
	var b strings.Builder

	// Center the dialog
	b.WriteString("\n\n")
	b.WriteString(a.styles.Info.Render("####") + "  " + a.styles.PanelTitle.Render("Confirm") + "  " + a.styles.Info.Render("####"))
	b.WriteString("\n\n")
	b.WriteString("    " + a.styles.Warning.Render(a.confirmPrompt))
	b.WriteString("\n\n")
	b.WriteString("    " + a.styles.HelpKey.Render("<y/Enter>") + " " + a.styles.HelpDesc.Render("Yes"))
	b.WriteString("    " + a.styles.HelpKey.Render("<n/Esc>") + " " + a.styles.HelpDesc.Render("No"))
	b.WriteString("\n")

	contentStyle := lipgloss.NewStyle().
		Width(a.width).
		Height(height).
		PaddingLeft(1).
		PaddingRight(1)

	return contentStyle.Render(b.String())
}

func (a *App) renderHeader() string {
	// === Row 1: Logo and Status ===
	logo := a.renderLogo()
	status := a.renderStatus()

	// If toast is active, replace status or overlay?
	// Let's replace the gap with toast if it fits
	var toast string
	if a.activeToast != nil {
		style := a.styles.ToastInfo
		switch a.activeToast.Type {
		case ToastSuccess:
			style = a.styles.ToastSuccess
		case ToastWarning:
			style = a.styles.ToastWarning
		case ToastError:
			style = a.styles.ToastError
		}
		toast = style.Render(a.activeToast.Message)
	}

	// Calculate gap
	logoWidth := lipgloss.Width(logo)
	statusWidth := lipgloss.Width(status)
	toastWidth := lipgloss.Width(toast)

	gap := a.width - logoWidth - statusWidth
	if gap < 1 {
		gap = 1
	}

	// Render
	var topRow string
	if toast != "" && toastWidth < gap {
		// Align toast to right, next to status
		innerGap := gap - toastWidth - 1 // -1 for padding
		if innerGap < 0 {
			innerGap = 0
		}
		topRow = lipgloss.JoinHorizontal(lipgloss.Top, logo, strings.Repeat(" ", innerGap), toast, " ", status)
	} else {
		topRow = lipgloss.JoinHorizontal(lipgloss.Top, logo, strings.Repeat(" ", gap), status)
	}

	// === Row 2: Breadcrumbs ===
	crumbs := a.renderCrumbs()

	// === Row 3: Divider ===
	divider := a.styles.Divider.Render(strings.Repeat("─", a.width))

	return lipgloss.JoinVertical(lipgloss.Left, topRow, crumbs, divider)
}

func (a *App) renderLogo() string {
	title := a.styles.LogoText.Render(" RCC ")
	subtitle := a.styles.LogoSubtle.Render("Interactive")

	return lipgloss.JoinHorizontal(lipgloss.Center, title, subtitle)
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
	viewNames := []string{"Home", "Commands", "Robots", "Holotree", "History", "Diagnostics", "Logs", "Remote"}
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
		{"5", "History - View robot run history"},
		{"6", "Diagnostics - System health checks"},
		{"7", "Logs - View logs"},
		{"8", "Remote - Remote server management"},
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
		{"4", "Holotree"},
		{"5", "History"},
		{"6", "Diag"},
		{"7", "Logs"},
		{"8", "Remote"},
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
	case ViewHistory:
		viewHints = append(viewHints,
			struct{ key, desc string }{"j/k", "Nav"},
			struct{ key, desc string }{"r", "Re-run"},
			struct{ key, desc string }{"c", "Clear"},
			struct{ key, desc string }{"R", "Refresh"},
		)
	case ViewDiagnostics:
		viewHints = append(viewHints,
			struct{ key, desc string }{"d", "Full diag"},
			struct{ key, desc string }{"R", "Refresh"},
		)
	case ViewLogs:
		viewHints = append(viewHints,
			struct{ key, desc string }{"j/k", "Scroll"},
			struct{ key, desc string }{"g/G", "Top/Bot"},
			struct{ key, desc string }{"c", "Clear"},
		)
	case ViewRemote:
		viewHints = append(viewHints,
			struct{ key, desc string }{"tab", "Switch"},
			struct{ key, desc string }{"j/k", "Nav"},
			struct{ key, desc string }{"e", "Export"},
			struct{ key, desc string }{"R", "Refresh"},
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

// Run starts the interactive application and returns any action the user selected
func Run() (*ActionResult, error) {
	return RunWithStartView(ViewHome)
}

// RunWithStartView starts the interactive application at a specific view
func RunWithStartView(startView ViewType) (*ActionResult, error) {
	app := NewApp()
	app.activeView = startView
	p := tea.NewProgram(
		app,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	model, err := p.Run()
	if err != nil {
		return nil, err
	}

	// Check if user selected an action
	if finalApp, ok := model.(*App); ok && finalApp.pendingAction != nil {
		return finalApp.pendingAction, nil
	}

	return nil, nil
}
