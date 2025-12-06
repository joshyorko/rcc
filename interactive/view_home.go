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
	styles *Styles
	info   SystemInfo
	width  int
	height int
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

	return systemInfoMsg(info)
}

type systemInfoMsg SystemInfo

// Update implements View
func (v *HomeView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case systemInfoMsg:
		v.info = SystemInfo(msg)
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
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
	left.WriteString("\n")
	left.WriteString("Debug: Plain Text Check")
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

	// --- Right Column: Quick Nav ---
	right := strings.Builder{}
	right.WriteString(vs.Accent.Bold(true).Render("NAVIGATION"))
	right.WriteString("\n\n")

	navs := []struct{ key, label string }{
		{"2", "Commands"},
		{"3", "Robots"},
		{"4", "Environments"},
		{"5", "Logs"},
		{"6", "Remote Stats"},
	}

	for _, n := range navs {
		right.WriteString(vs.KeyHint.Render(n.key))
		right.WriteString(" ")
		right.WriteString(vs.Text.Render(n.label))
		right.WriteString("\n")
	}

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
	return "1-6:switch views"
}
