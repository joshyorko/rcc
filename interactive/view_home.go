package interactive

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/htfs"
)

// HomeView displays the main dashboard with system status and quick actions
type HomeView struct {
	styles       *Styles
	info         SystemInfo
	width        int
	height       int
	quickAction  int // Selected quick action
	remoteOrigin string
}

// SystemInfo holds system information for display
type SystemInfo struct {
	Version      string
	Platform     string
	Architecture string
	Hostname     string
	Username     string
	HolotreeDir  string
	EnvCount     int
	RobotCount   int // Placeholder for future
}

// NewHomeView creates a new home view
func NewHomeView(styles *Styles) *HomeView {
	return &HomeView{
		styles: styles,
		width:  120,
		height: 30,
	}
}

// Init implements View
func (v *HomeView) Init() tea.Cmd {
	return v.loadSystemInfo
}

type homeInfoMsg struct {
	info   SystemInfo
	remote string
}

// loadSystemInfo loads system information
func (v *HomeView) loadSystemInfo() tea.Msg {
	info := SystemInfo{
		Version:      common.Version,
		Platform:     runtime.GOOS,
		Architecture: runtime.GOARCH,
	}

	if hostname, err := os.Hostname(); err == nil {
		info.Hostname = hostname
	}
	if username := os.Getenv("USER"); username != "" {
		info.Username = username
	} else if username := os.Getenv("USERNAME"); username != "" {
		info.Username = username
	}

	info.HolotreeDir = common.HololibLocation()
	catalogs := htfs.CatalogNames()
	info.EnvCount = len(catalogs)

	return homeInfoMsg{info: info, remote: common.RccRemoteOrigin()}
}

// Update implements View
func (v *HomeView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case homeInfoMsg:
		v.info = msg.info
		v.remoteOrigin = msg.remote
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if v.quickAction < 2 { // 3 quick actions
				v.quickAction++
			}
		case "k", "up":
			if v.quickAction > 0 {
				v.quickAction--
			}
		case "enter":
			return v.handleQuickAction()
		case "R":
			return v, v.loadSystemInfo
		}
	}
	return v, nil
}

func (v *HomeView) handleQuickAction() (View, tea.Cmd) {
	switch v.quickAction {
	case 0: // Run diagnostics
		action := ActionResult{
			Type: ActionCheckIntegrity,
		}
		return v, func() tea.Msg { return actionMsg{action: action} }
	case 1: // Pull catalogs (if remote configured)
		if v.remoteOrigin != "" {
			action := ActionResult{
				Type:    ActionRunCommand,
				Command: fmt.Sprintf("rcc holotree pull -o %s", v.remoteOrigin),
			}
			return v, func() tea.Msg { return actionMsg{action: action} }
		}
	case 2: // Create new robot
		action := ActionResult{
			Type:    ActionRunCommand,
			Command: "rcc robot init",
		}
		return v, func() tea.Msg { return actionMsg{action: action} }
	}
	return v, nil
}

// View implements View
func (v *HomeView) View() string {
	vs := NewViewStyles(v.styles.theme)

	// Layout calculations
	totalWidth := v.width - 4
	if totalWidth < 60 {
		totalWidth = 60
	}

	// Create a grid layout
	// Left: System Info
	// Middle: Context / RCC Status
	// Right: Quick Actions / Nav

	colWidth := (totalWidth - 4) / 3
	if colWidth < 25 {
		colWidth = 25
	}

	// Panel Styles
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(v.styles.theme.Border).
		Padding(1, 1).
		Width(colWidth).
		Height(v.height - 8) // Reserve space for header/footer

	// --- Left Column: System ---
	left := strings.Builder{}
	left.WriteString(vs.Accent.Bold(true).Render("SYSTEM"))
	left.WriteString("\n\n")

	left.WriteString(vs.Label.Render("Host"))
	left.WriteString("\n")
	left.WriteString(vs.Text.Render(v.info.Hostname))
	left.WriteString("\n\n")

	left.WriteString(vs.Label.Render("User"))
	left.WriteString("\n")
	left.WriteString(vs.Text.Render(v.info.Username))
	left.WriteString("\n\n")

	left.WriteString(vs.Label.Render("Platform"))
	left.WriteString("\n")
	left.WriteString(vs.Info.Render(v.info.Platform + "/" + v.info.Architecture))
	left.WriteString("\n\n")

	left.WriteString(vs.Label.Render("RCC Version"))
	left.WriteString("\n")
	left.WriteString(vs.Accent.Render(v.info.Version))

	// --- Middle Column: Context ---
	mid := strings.Builder{}
	mid.WriteString(vs.Accent.Bold(true).Render("CONTEXT"))
	mid.WriteString("\n\n")

	mid.WriteString(vs.Label.Render("Holotree"))
	mid.WriteString("\n")
	// Clean path for display
	htPath := v.info.HolotreeDir
	if len(htPath) > colWidth-2 {
		htPath = "..." + htPath[len(htPath)-(colWidth-5):]
	}
	mid.WriteString(vs.Subtext.Render(htPath))
	mid.WriteString("\n\n")

	mid.WriteString(vs.Label.Render("Environments"))
	mid.WriteString("\n")
	mid.WriteString(vs.Success.Render(fmt.Sprintf("%d stored", v.info.EnvCount)))
	mid.WriteString("\n\n")

	mid.WriteString(vs.Label.Render("Status"))
	mid.WriteString("\n")
	mid.WriteString(vs.Success.Render("● Operational"))

	// --- Right Column: Quick Actions ---
	right := strings.Builder{}
	right.WriteString(vs.Accent.Bold(true).Render("QUICK ACTIONS"))
	right.WriteString("\n\n")

	quickActions := []struct{ label, desc string }{
		{"Check Integrity", "Verify holotree"},
		{"Pull Catalogs", "From remote server"},
		{"Create Robot", "Initialize new robot"},
	}

	for i, qa := range quickActions {
		if i == v.quickAction {
			right.WriteString(vs.Selected.Render("> " + qa.label))
		} else {
			right.WriteString(vs.Normal.Render("  " + qa.label))
		}
		right.WriteString("\n")
		if i == 1 && v.remoteOrigin == "" {
			right.WriteString(vs.Subtext.Render("    (no remote)"))
			right.WriteString("\n")
		}
	}

	right.WriteString("\n")
	right.WriteString(vs.Subtext.Render("Enter to run, j/k nav"))
	right.WriteString("\n\n")

	right.WriteString(vs.Accent.Bold(true).Render("VIEWS"))
	right.WriteString("\n")
	right.WriteString(vs.Subtext.Render("1-7 to switch"))

	// Join columns
	content := lipgloss.JoinHorizontal(lipgloss.Top,
		panelStyle.Render(left.String()),
		panelStyle.Render(mid.String()),
		panelStyle.Render(right.String()),
	)

	// Combine into full view
	return lipgloss.Place(v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		content,
	)
}

// Name implements View
func (v *HomeView) Name() string {
	return "Home"
}

// ShortHelp implements View
func (v *HomeView) ShortHelp() string {
	return "1-7:views j/k:nav Enter:action R:refresh"
}
