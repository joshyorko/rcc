package interactive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/htfs"
)

// EnvironmentsView displays holotree environments and catalogs
type EnvironmentsView struct {
	styles          *Styles
	width           int
	height          int
	catalogs        []CatalogInfo
	selected        int
	tab             int // 0 = catalogs, 1 = spaces
	loadingCatalogs bool
}

// CatalogInfo holds detailed information about a catalog
type CatalogInfo struct {
	Name     string
	FullPath string
	SizeKB   int64
	Age      int // days since modified
	Platform string
}

// NewEnvironmentsView creates a new environments view
func NewEnvironmentsView(styles *Styles) *EnvironmentsView {
	return &EnvironmentsView{
		styles:          styles,
		width:           120,
		height:          30,
		catalogs:        []CatalogInfo{},
		selected:        0,
		tab:             0,
		loadingCatalogs: true,
	}
}

// Init implements View
func (v *EnvironmentsView) Init() tea.Cmd {
	return v.loadCatalogs
}

func (v *EnvironmentsView) loadCatalogs() tea.Msg {
	names := htfs.CatalogNames()
	catalogInfos := make([]CatalogInfo, 0, len(names))

	for _, name := range names {
		fullPath := filepath.Join(common.HololibCatalogLocation(), name)
		info := CatalogInfo{
			Name:     name,
			FullPath: fullPath,
			Platform: "unknown",
		}

		if stat, err := os.Stat(fullPath); err == nil {
			info.SizeKB = stat.Size() / 1024
			info.Age = int(time.Since(stat.ModTime()).Hours() / 24)
		}

		if strings.Contains(name, "win") || strings.Contains(name, "windows") {
			info.Platform = "windows"
		} else if strings.Contains(name, "darwin") || strings.Contains(name, "mac") {
			info.Platform = "darwin"
		} else if strings.Contains(name, "linux") {
			info.Platform = "linux"
		}

		catalogInfos = append(catalogInfos, info)
	}

	return catalogsLoadedMsg(catalogInfos)
}

type catalogsLoadedMsg []CatalogInfo

// Update implements View
func (v *EnvironmentsView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case catalogsLoadedMsg:
		v.catalogs = []CatalogInfo(msg)
		v.loadingCatalogs = false
		if len(v.catalogs) > 0 && v.selected >= len(v.catalogs) {
			v.selected = len(v.catalogs) - 1
		}
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			v.tab = (v.tab + 1) % 2
			v.selected = 0
		case "j", "down":
			if v.tab == 0 && v.selected < len(v.catalogs)-1 {
				v.selected++
			}
		case "k", "up":
			if v.tab == 0 && v.selected > 0 {
				v.selected--
			}
		case "g":
			v.selected = 0
		case "G":
			if len(v.catalogs) > 0 {
				v.selected = len(v.catalogs) - 1
			}
		case "R":
			v.loadingCatalogs = true
			return v, v.loadCatalogs
		case "d":
			if v.tab == 0 && v.selected >= 0 && v.selected < len(v.catalogs) {
				action := ActionResult{
					Type:  ActionDeleteEnv,
					EnvID: v.catalogs[v.selected].Name,
				}
				return v, func() tea.Msg { return actionMsg{action: action} }
			}
		}
	}
	return v, nil
}

// View implements View
func (v *EnvironmentsView) View() string {
	theme := v.styles.theme
	vs := NewViewStyles(theme)

	// Dynamic box sizing
	boxWidth := v.width - 8
	if boxWidth < 60 {
		boxWidth = 60
	}
	if boxWidth > 140 {
		boxWidth = 140
	}
	contentWidth := boxWidth - 6

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 2).
		Width(boxWidth)

	var b strings.Builder

	// Header with RCC version
	subtitle := ""
	if !v.loadingCatalogs {
		subtitle = fmt.Sprintf("(%d catalogs)", len(v.catalogs))
	}
	b.WriteString(RenderHeader(vs, "Holotree", subtitle, contentWidth))

	// Tab bar
	tabs := []string{"Catalogs", "Spaces"}
	for i, tab := range tabs {
		if i == v.tab {
			b.WriteString(vs.BadgeActive.Render(" " + tab + " "))
		} else {
			b.WriteString(vs.Badge.Render(" " + tab + " "))
		}
		b.WriteString(" ")
	}
	b.WriteString(vs.Subtext.Render(" Tab to switch"))
	b.WriteString("\n\n")

	// Content based on tab
	if v.tab == 0 {
		b.WriteString(v.renderCatalogsContent(vs, contentWidth))
	} else {
		b.WriteString(v.renderSpacesContent(vs, contentWidth))
	}

	// Footer
	b.WriteString("\n")
	hints := []KeyHint{
		{"Tab", "switch"},
		{"j/k", "nav"},
		{"d", "delete"},
		{"R", "refresh"},
	}
	b.WriteString(RenderFooter(vs, hints, contentWidth))

	return lipgloss.Place(v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		boxStyle.Render(b.String()),
	)
}

func (v *EnvironmentsView) renderCatalogsContent(vs ViewStyles, contentWidth int) string {
	var b strings.Builder

	if v.loadingCatalogs {
		b.WriteString(vs.Subtext.Render("Loading catalogs..."))
		return b.String()
	}

	if len(v.catalogs) == 0 {
		b.WriteString(vs.Subtext.Render("No catalogs found in holotree"))
		b.WriteString("\n\n")
		b.WriteString(vs.Label.Render("Tip"))
		b.WriteString(vs.Text.Render("Run a robot to create environments"))
		return b.String()
	}

	// Catalog list
	for i, catalog := range v.catalogs {
		isSelected := i == v.selected

		// Truncate name if needed
		displayName := catalog.Name
		if len(displayName) > 40 {
			displayName = displayName[:37] + "..."
		}

		// Age display
		ageStr := fmt.Sprintf("%dd", catalog.Age)
		if catalog.Age == 0 {
			ageStr = "today"
		}

		// Platform badge
		platformBadge := ""
		switch catalog.Platform {
		case "linux":
			platformBadge = vs.Success.Render("[LNX]")
		case "darwin":
			platformBadge = vs.Info.Render("[MAC]")
		case "windows":
			platformBadge = vs.Warning.Render("[WIN]")
		default:
			platformBadge = vs.Subtext.Render("[???]")
		}

		if isSelected {
			b.WriteString(vs.Selected.Render("> " + displayName))
		} else {
			b.WriteString(vs.Normal.Render("  " + displayName))
		}
		b.WriteString("  ")
		b.WriteString(platformBadge)
		b.WriteString("  ")
		b.WriteString(vs.Subtext.Render(ageStr))
		b.WriteString("\n")

		// Show details for selected catalog
		if isSelected {
			b.WriteString("\n")
			b.WriteString(vs.Label.Render("Size"))
			b.WriteString(vs.Accent.Render(fmt.Sprintf("%d KB", catalog.SizeKB)))
			b.WriteString("\n")

			// Truncate path
			pathStr := catalog.FullPath
			if len(pathStr) > contentWidth-16 {
				half := (contentWidth - 19) / 2
				pathStr = pathStr[:half] + "..." + pathStr[len(pathStr)-half:]
			}
			b.WriteString(vs.Label.Render("Path"))
			b.WriteString(vs.Subtext.Render(pathStr))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (v *EnvironmentsView) renderSpacesContent(vs ViewStyles, contentWidth int) string {
	var b strings.Builder

	b.WriteString(vs.Subtext.Render("Holotree Spaces"))
	b.WriteString("\n\n")
	b.WriteString(vs.Text.Render("Spaces are isolated holotree environments used by"))
	b.WriteString("\n")
	b.WriteString(vs.Text.Render("different controllers and automation tasks."))
	b.WriteString("\n\n")
	b.WriteString(vs.Info.Render("Space management coming soon."))

	return b.String()
}

// Name implements View
func (v *EnvironmentsView) Name() string {
	return "Holotree"
}

// ShortHelp implements View
func (v *EnvironmentsView) ShortHelp() string {
	return "tab:switch j/k:nav d:delete R:refresh"
}
