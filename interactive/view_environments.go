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

		// Get file stats
		if stat, err := os.Stat(fullPath); err == nil {
			info.SizeKB = stat.Size() / 1024
			info.Age = int(time.Since(stat.ModTime()).Hours() / 24)
		}

		// Try to determine platform from filename pattern
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
		case "R":
			// Refresh catalogs
			v.loadingCatalogs = true
			return v, v.loadCatalogs
		case "d":
			// Delete selected catalog
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
	var sections []string

	// Header with title
	sections = append(sections, v.renderHeader())

	// Tab bar
	sections = append(sections, v.renderTabs())

	// Content area based on selected tab
	if v.tab == 0 {
		sections = append(sections, v.renderCatalogsPanel())
	} else {
		sections = append(sections, v.renderSpacesPanel())
	}

	// Help footer
	sections = append(sections, "")
	sections = append(sections, v.renderHelp())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (v *EnvironmentsView) renderHeader() string {
	title := v.styles.Title.Render("Holotree")
	subtitle := v.styles.Subtle.Render("Manage isolated Python environments and catalogs")

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		subtitle,
		"",
	)
}

func (v *EnvironmentsView) renderTabs() string {
	tabs := []string{"Catalogs", "Spaces"}

	var tabStrings []string
	for i, tab := range tabs {
		if i == v.tab {
			tabStrings = append(tabStrings, v.styles.ActiveTab.Render(tab))
		} else {
			tabStrings = append(tabStrings, v.styles.Tab.Render(tab))
		}
	}

	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, tabStrings...)
	divider := v.styles.Divider.Render(strings.Repeat("─", v.width-4))

	return lipgloss.JoinVertical(lipgloss.Left, tabBar, divider, "")
}

func (v *EnvironmentsView) renderCatalogsPanel() string {
	if v.loadingCatalogs {
		loading := v.styles.Spinner.Render("Loading catalogs...")
		return v.styles.Panel.Width(v.width - 4).Render(loading)
	}

	if len(v.catalogs) == 0 {
		emptyMsg := v.styles.Subtle.Render("No catalogs found in holotree.\n\n") +
			v.styles.Info.Render("Catalogs are created automatically when you build or use environments.")
		return v.styles.Panel.Width(v.width - 4).Render(emptyMsg)
	}

	// Split view: catalog list on left, details on right
	listWidth := 60
	detailsWidth := v.width - listWidth - 8

	catalogList := v.renderCatalogList(listWidth)
	catalogDetails := v.renderCatalogDetails(detailsWidth)

	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		catalogList,
		"  ",
		catalogDetails,
	)

	return content
}

func (v *EnvironmentsView) renderCatalogList(width int) string {
	var content []string

	// Header
	content = append(content, v.styles.PanelTitle.Render("Catalog List"))
	content = append(content, "")

	// Table header
	headerCols := []string{
		v.styles.TableHeader.Width(8).Render(""),
		v.styles.TableHeader.Width(35).Render("Catalog ID"),
		v.styles.TableHeader.Width(10).Render("Age"),
	}
	header := lipgloss.JoinHorizontal(lipgloss.Left, headerCols...)
	content = append(content, header)
	content = append(content, v.styles.Divider.Render(strings.Repeat("─", width-4)))

	// Table rows
	for i, catalog := range v.catalogs {
		var prefix string
		var rowStyle lipgloss.Style

		if i == v.selected {
			prefix = "> "
			rowStyle = v.styles.ListItemSelected
		} else {
			prefix = "  "
			if i%2 == 0 {
				rowStyle = v.styles.TableRow
			} else {
				rowStyle = v.styles.TableRowAlt
			}
		}

		// Truncate long catalog names
		displayName := catalog.Name
		if len(displayName) > 32 {
			displayName = displayName[:29] + "..."
		}

		ageStr := fmt.Sprintf("%dd", catalog.Age)
		if catalog.Age == 0 {
			ageStr = "today"
		}

		cols := []string{
			rowStyle.Width(8).Render(prefix),
			rowStyle.Width(35).Render(displayName),
			rowStyle.Width(10).Render(ageStr),
		}

		row := lipgloss.JoinHorizontal(lipgloss.Left, cols...)
		content = append(content, row)
	}

	// Footer stats
	content = append(content, "")
	content = append(content, v.styles.Divider.Render(strings.Repeat("─", width-4)))
	stats := v.styles.Subtle.Render(fmt.Sprintf("Total: %d catalogs", len(v.catalogs)))
	content = append(content, stats)

	listContent := lipgloss.JoinVertical(lipgloss.Left, content...)
	return v.styles.Panel.Width(width).Render(listContent)
}

func (v *EnvironmentsView) renderCatalogDetails(width int) string {
	var content []string

	// Header
	content = append(content, v.styles.PanelTitle.Render("Catalog Details"))
	content = append(content, "")

	if v.selected < 0 || v.selected >= len(v.catalogs) {
		content = append(content, v.styles.Subtle.Render("Select a catalog to view details"))
	} else {
		catalog := v.catalogs[v.selected]

		// Platform badge
		var platformColor lipgloss.Style
		switch catalog.Platform {
		case "linux":
			platformColor = v.styles.Success
		case "darwin":
			platformColor = v.styles.Info
		case "windows":
			platformColor = v.styles.Warning
		default:
			platformColor = v.styles.Subtle
		}

		platform := platformColor.Render("[" + strings.ToUpper(catalog.Platform) + "]")
		content = append(content, platform)
		content = append(content, "")

		// Catalog info
		details := []struct {
			label string
			value string
			style lipgloss.Style
		}{
			{"Name", catalog.Name, v.styles.Highlight},
			{"Size", fmt.Sprintf("%d KB", catalog.SizeKB), v.styles.Accent},
			{"Age", fmt.Sprintf("%d days", catalog.Age), v.styles.Info},
		}

		for _, detail := range details {
			line := v.styles.StatusKey.Render(detail.label+": ") +
				detail.style.Render(detail.value)
			content = append(content, line)
		}

		content = append(content, "")
		content = append(content, v.styles.Divider.Render(strings.Repeat("─", width-4)))
		content = append(content, "")

		// Path info
		content = append(content, v.styles.StatusKey.Render("Location:"))
		pathStr := catalog.FullPath
		if len(pathStr) > width-8 {
			// Truncate from middle
			half := (width - 11) / 2
			pathStr = pathStr[:half] + "..." + pathStr[len(pathStr)-half:]
		}
		content = append(content, v.styles.Subtle.Render(pathStr))

		content = append(content, "")
		content = append(content, "")

		// Actions
		content = append(content, v.styles.StatusKey.Render("Actions:"))
		deleteAction := v.styles.Warning.Render("  d") +
			v.styles.Subtle.Render(" - Delete this catalog")
		content = append(content, deleteAction)

		refreshAction := v.styles.Info.Render("  R") +
			v.styles.Subtle.Render(" - Refresh catalog list")
		content = append(content, refreshAction)
	}

	detailsContent := lipgloss.JoinVertical(lipgloss.Left, content...)
	return v.styles.Panel.Width(width).Render(detailsContent)
}

func (v *EnvironmentsView) renderSpacesPanel() string {
	var content []string

	// Header
	content = append(content, v.styles.PanelTitle.Render("Holotree Spaces"))
	content = append(content, "")

	// Placeholder content
	content = append(content, v.styles.Subtle.Render("Space management coming soon."))
	content = append(content, "")
	content = append(content, v.styles.Info.Render(
		"Spaces are isolated holotree environments used by different\n"+
			"controllers and automation tasks. Each space maintains its\n"+
			"own set of Python environments based on catalog blueprints.",
	))

	spacesContent := lipgloss.JoinVertical(lipgloss.Left, content...)
	return v.styles.Panel.Width(v.width - 4).Render(spacesContent)
}

func (v *EnvironmentsView) renderHelp() string {
	helps := []struct {
		key  string
		desc string
	}{
		{"tab", "Switch views"},
		{"j/k ↑↓", "Navigate"},
		{"d", "Delete"},
		{"R", "Refresh"},
		{"q", "Back"},
	}

	var helpItems []string
	for _, h := range helps {
		item := v.styles.HelpKey.Render(h.key) +
			v.styles.Subtle.Render(" "+h.desc)
		helpItems = append(helpItems, item)
	}

	return v.styles.Subtle.Render("  ") +
		strings.Join(helpItems, v.styles.Subtle.Render("  •  "))
}

// Name implements View
func (v *EnvironmentsView) Name() string {
	return "Holotree"
}

// ShortHelp implements View
func (v *EnvironmentsView) ShortHelp() string {
	return "tab:switch j/k:nav d:delete R:refresh"
}
