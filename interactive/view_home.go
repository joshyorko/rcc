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
	theme := v.styles.theme
	vs := NewViewStyles(theme)

	// Dynamic box sizing
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

	// Header with RCC version (same as all other views)
	b.WriteString(RenderHeader(vs, "Dashboard", "", contentWidth))
	b.WriteString("\n")

	// Status line
	b.WriteString(vs.Success.Render("[OK] Ready"))
	b.WriteString("\n\n")

	// Info grid
	b.WriteString(vs.Label.Render("Version"))
	b.WriteString(vs.Accent.Render(v.info.Version))
	b.WriteString("\n")

	b.WriteString(vs.Label.Render("Platform"))
	b.WriteString(vs.Info.Render(v.info.Platform + "/" + v.info.Architecture))
	b.WriteString("\n")

	b.WriteString(vs.Label.Render("User"))
	b.WriteString(vs.Text.Render(v.info.Username))
	b.WriteString("\n")

	b.WriteString(vs.Label.Render("Host"))
	b.WriteString(vs.Text.Render(v.info.Hostname))
	b.WriteString("\n")

	b.WriteString(vs.Label.Render("Environments"))
	b.WriteString(vs.Success.Render(fmt.Sprintf("%d", v.info.EnvCount)))
	b.WriteString("\n")

	// Truncate holotree path
	htPath := v.info.HolotreeDir
	maxPathLen := contentWidth - 16
	if len(htPath) > maxPathLen {
		htPath = "..." + htPath[len(htPath)-(maxPathLen-3):]
	}
	b.WriteString(vs.Label.Render("Holotree"))
	b.WriteString(vs.Subtext.Render(htPath))
	b.WriteString("\n\n")

	// Navigation section
	b.WriteString(vs.Separator.Render(strings.Repeat("─", contentWidth)))
	b.WriteString("\n")
	b.WriteString(vs.Accent.Bold(true).Render("Navigation"))
	b.WriteString("\n\n")

	navItems := []struct {
		key  string
		desc string
	}{
		{"2", "Commands"},
		{"3", "Robots"},
		{"4", "Holotree"},
		{"5", "Logs"},
		{"6", "Remote"},
	}

	for _, nav := range navItems {
		b.WriteString("  ")
		b.WriteString(vs.KeyHint.Render(nav.key))
		b.WriteString(" ")
		b.WriteString(vs.Subtext.Render(nav.desc))
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("\n")
	hints := []KeyHint{
		{"1-6", "views"},
		{"?", "help"},
		{"q", "quit"},
	}
	b.WriteString(RenderFooter(vs, hints, contentWidth))

	return lipgloss.Place(v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		boxStyle.Render(b.String()),
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
