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

// HomeView displays the main dashboard
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
	var b strings.Builder

	// Banner/Welcome header
	b.WriteString(v.renderBanner())
	b.WriteString("\n\n")

	// System info in a full-width layout
	b.WriteString(v.renderSystemSection())
	b.WriteString("\n\n")

	// Quick Actions
	b.WriteString(v.renderQuickActions())

	return b.String()
}

func (v *HomeView) renderBanner() string {
	banner := `
    ____  ______________
   / __ \/ ____/ ____/   Repeatable, Contained Code
  / /_/ / /   / /        ` + v.styles.Highlight.Render("v"+v.info.Version) + `
 / _, _/ /___/ /___      Platform: ` + v.styles.Info.Render(v.info.Platform+"/"+v.info.Architecture) + `
/_/ |_|\____/\____/      Ready for automation
`
	return v.styles.Title.Render(banner)
}

func (v *HomeView) renderSystemSection() string {
	var b strings.Builder

	// Section header with RCC-style progress indicator
	header := v.styles.Info.Render("####") + "  " + v.styles.PanelTitle.Render("System Status") + "  " + v.styles.Info.Render("####")
	b.WriteString(header)
	b.WriteString("\n\n")

	// Two-column layout for stats
	col1Width := 40

	// Left column: System info
	leftLines := []string{
		v.formatRow("User", fmt.Sprintf("%s@%s", v.info.Username, v.info.Hostname)),
		v.formatRow("Holotree", v.truncatePath(v.info.HolotreeDir, 25)),
	}

	// Right column: Stats
	rightLines := []string{
		v.formatStat("Environments", v.info.EnvCount, v.styles.Success),
		v.formatStat("Catalogs", v.info.EnvCount, v.styles.Info),
	}

	// Combine columns
	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	for i := 0; i < maxLines; i++ {
		left := ""
		right := ""
		if i < len(leftLines) {
			left = leftLines[i]
		}
		if i < len(rightLines) {
			right = rightLines[i]
		}
		// Pad left column to fixed width
		leftPadded := left + strings.Repeat(" ", col1Width-lipgloss.Width(left))
		b.WriteString("    " + leftPadded + "    " + right + "\n")
	}

	return b.String()
}

func (v *HomeView) formatRow(label, value string) string {
	labelStr := v.styles.Subtle.Render(label + ": ")
	valueStr := v.styles.Highlight.Render(value)
	return labelStr + valueStr
}

func (v *HomeView) formatStat(label string, value int, style lipgloss.Style) string {
	labelStr := v.styles.Subtle.Render(label + ": ")
	valueStr := style.Render(fmt.Sprintf("%d", value))
	return labelStr + valueStr
}

func (v *HomeView) truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	return "..." + path[len(path)-(maxLen-3):]
}

func (v *HomeView) renderQuickActions() string {
	var b strings.Builder

	header := v.styles.Info.Render("####") + "  " + v.styles.PanelTitle.Render("Quick Actions") + "  " + v.styles.Info.Render("####")
	b.WriteString(header)
	b.WriteString("\n\n")

	// Actions in a grid
	actions := []struct {
		key  string
		desc string
	}{
		{"2", "Browse Commands"},
		{"3", "View Robots"},
		{"4", "Manage Environments"},
		{"5", "View Logs"},
	}

	for _, a := range actions {
		keyStr := v.styles.HelpKey.Render("  <" + a.key + "> ")
		descStr := v.styles.HelpDesc.Render(a.desc)
		b.WriteString(keyStr + descStr + "\n")
	}

	return b.String()
}

// Name implements View
func (v *HomeView) Name() string {
	return "Home"
}

// ShortHelp implements View
func (v *HomeView) ShortHelp() string {
	return "1-5:switch views"
}
