package interactive

import (
	"fmt"
	"os"
	"runtime"

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
		width:  120, // default, will be updated
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

	// Count environments
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
	var sections []string

	// Banner/Welcome header
	sections = append(sections, v.renderBanner())

	// System info cards in a row
	sections = append(sections, v.renderSystemCards())

	// Quick Actions section
	sections = append(sections, v.renderQuickActions())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (v *HomeView) renderBanner() string {
	// Create a clean centered banner without ugly "####" markers
	var lines []string

	// ASCII art logo - simple, clean RCC
	logo := []string{
		" ____   ____ ____ ",
		"|  _ \\ / ___|/ ___|",
		"| |_) | |   | |    ",
		"|  _ <| |___| |___ ",
		"|_| \\_\\\\____|\\____|",
	}

	for _, line := range logo {
		lines = append(lines, v.styles.Title.Render(line))
	}

	// Tagline
	tagline := v.styles.Subtle.Render("Repeatable - Contained - Code")

	// Version and status
	versionLine := v.styles.StatusKey.Render("Version: ") +
		v.styles.Highlight.Render(v.info.Version) +
		v.styles.Subtle.Render("  │  ") +
		v.styles.StatusKey.Render("Platform: ") +
		v.styles.Info.Render(v.info.Platform+"/"+v.info.Architecture)

	statusLine := v.styles.Success.Render("[OK] System Ready")

	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Width(40).Align(lipgloss.Center).Render(tagline))
	lines = append(lines, lipgloss.NewStyle().Width(60).Align(lipgloss.Center).Render(versionLine))
	lines = append(lines, lipgloss.NewStyle().Width(40).Align(lipgloss.Center).Render(statusLine))
	lines = append(lines, "")

	return lipgloss.JoinVertical(lipgloss.Center, lines...)
}

func (v *HomeView) renderSystemCards() string {
	// Create individual status cards with rounded borders
	cardWidth := 35

	// User/Host Card
	userCard := v.createStatusCard(
		"System",
		[]statusItem{
			{label: "User", value: v.info.Username, style: v.styles.Accent},
			{label: "Host", value: v.info.Hostname, style: v.styles.Info},
		},
		cardWidth,
	)

	// Environment Card
	envCard := v.createStatusCard(
		"Environment",
		[]statusItem{
			{label: "Environments", value: fmt.Sprintf("%d", v.info.EnvCount), style: v.styles.Success},
			{label: "Catalogs", value: fmt.Sprintf("%d", v.info.EnvCount), style: v.styles.Warning},
		},
		cardWidth,
	)

	// Holotree Card
	holotreePath := v.info.HolotreeDir
	if len(holotreePath) > 25 {
		holotreePath = "..." + holotreePath[len(holotreePath)-22:]
	}
	holotreeCard := v.createStatusCard(
		"Holotree",
		[]statusItem{
			{label: "Location", value: holotreePath, style: v.styles.Highlight},
		},
		cardWidth,
	)

	// Arrange cards in a row
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		userCard,
		"  ",
		envCard,
		"  ",
		holotreeCard,
	)
}

type statusItem struct {
	label string
	value string
	style lipgloss.Style
}

func (v *HomeView) createStatusCard(title string, items []statusItem, width int) string {
	var content []string

	// Title
	content = append(content, v.styles.PanelTitle.Render(title))
	content = append(content, "")

	// Items
	for _, item := range items {
		line := v.styles.StatusKey.Render(item.label+": ") +
			item.style.Render(item.value)
		content = append(content, line)
	}

	cardContent := lipgloss.JoinVertical(lipgloss.Left, content...)

	// Wrap in panel with rounded border
	return v.styles.Panel.
		Width(width).
		Render(cardContent)
}

func (v *HomeView) renderQuickActions() string {
	// Create a panel for quick actions
	var content []string

	content = append(content, v.styles.PanelTitle.Render("Quick Actions"))
	content = append(content, "")

	actions := []struct {
		key  string
		desc string
	}{
		{"2", "Browse Commands"},
		{"3", "View Robots"},
		{"4", "Manage Holotree"},
		{"5", "View Logs"},
		{"6", "Remote Catalogs"},
	}

	for _, a := range actions {
		line := "  " +
			v.styles.HelpKey.Render(a.key) +
			v.styles.Subtle.Render(" | ") +
			v.styles.HelpDesc.Render(a.desc)
		content = append(content, line)
	}

	actionContent := lipgloss.JoinVertical(lipgloss.Left, content...)

	// Wrap in panel
	return v.styles.Panel.
		Width(60).
		Render(actionContent)
}

// Name implements View
func (v *HomeView) Name() string {
	return "Home"
}

// ShortHelp implements View
func (v *HomeView) ShortHelp() string {
	return "1-5:switch views"
}
