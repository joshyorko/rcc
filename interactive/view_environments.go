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
	styles            *Styles
	width             int
	height            int
	catalogs          []CatalogInfo
	spaces            []SpaceInfo
	blueprints        []BlueprintInfo
	selected          int
	spaceSelected     int
	blueprintSelected int
	tab               int // 0 = catalogs, 1 = spaces, 2 = blueprints
	loadingCatalogs   bool
	loadingSpaces     bool
	// Input mode for import
	inputMode   bool
	inputBuffer string
	// Search/filter
	searchMode   bool
	searchBuffer string
	searchFilter string
}

// BlueprintInfo holds information about a unique blueprint
type BlueprintInfo struct {
	Hash       string
	SpaceCount int
	Controllers []string
}

// CatalogInfo holds detailed information about a catalog
type CatalogInfo struct {
	Name     string
	FullPath string
	SizeKB   int64
	Age      int // days since modified
	Platform string
}

// SpaceInfo holds detailed information about a holotree space
type SpaceInfo struct {
	Identity   string
	Controller string
	Space      string
	Blueprint  string
	Path       string
	LastUsed   string
	IdleDays   int
	UseCount   string
}

// NewEnvironmentsView creates a new environments view
func NewEnvironmentsView(styles *Styles) *EnvironmentsView {
	return &EnvironmentsView{
		styles:            styles,
		width:             120,
		height:            30,
		catalogs:          []CatalogInfo{},
		spaces:            []SpaceInfo{},
		blueprints:        []BlueprintInfo{},
		selected:          0,
		spaceSelected:     0,
		blueprintSelected: 0,
		tab:               0,
		loadingCatalogs:   true,
		loadingSpaces:     true,
	}
}

// Init implements View
func (v *EnvironmentsView) Init() tea.Cmd {
	return tea.Batch(v.loadCatalogs, v.loadSpaces)
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
type spacesLoadedMsg struct {
	spaces     []SpaceInfo
	blueprints []BlueprintInfo
}

func (v *EnvironmentsView) loadSpaces() tea.Msg {
	_, roots := htfs.LoadCatalogs()
	spaces := make([]SpaceInfo, 0)
	blueprintMap := make(map[string]*BlueprintInfo)

	for _, space := range roots.Spaces() {
		info := SpaceInfo{
			Identity:   space.Identity,
			Controller: space.Controller,
			Space:      space.Space,
			Blueprint:  space.Blueprint,
			Path:       space.Path,
		}

		// Get usage stats from .use file
		usefile := fmt.Sprintf("%s.use", space.Path)
		if stat, err := os.Stat(usefile); err == nil {
			info.UseCount = fmt.Sprintf("%d", stat.Size())
			delta := time.Since(stat.ModTime()).Hours() / 24.0
			info.IdleDays = int(delta)
			if delta < 1 {
				info.LastUsed = "today"
			} else {
				info.LastUsed = fmt.Sprintf("%dd ago", int(delta))
			}
		} else {
			info.LastUsed = "N/A"
			info.UseCount = "0"
		}

		spaces = append(spaces, info)

		// Aggregate blueprint info
		if bp, exists := blueprintMap[space.Blueprint]; exists {
			bp.SpaceCount++
			// Add controller if not already present
			found := false
			for _, c := range bp.Controllers {
				if c == space.Controller {
					found = true
					break
				}
			}
			if !found {
				bp.Controllers = append(bp.Controllers, space.Controller)
			}
		} else {
			blueprintMap[space.Blueprint] = &BlueprintInfo{
				Hash:        space.Blueprint,
				SpaceCount:  1,
				Controllers: []string{space.Controller},
			}
		}
	}

	// Convert map to slice
	blueprints := make([]BlueprintInfo, 0, len(blueprintMap))
	for _, bp := range blueprintMap {
		blueprints = append(blueprints, *bp)
	}

	return spacesLoadedMsg{spaces: spaces, blueprints: blueprints}
}

// Update implements View
func (v *EnvironmentsView) Update(msg tea.Msg) (View, tea.Cmd) {
	// Handle search mode
	if v.searchMode {
		return v.handleSearchMode(msg)
	}
	// Handle input mode for import
	if v.inputMode {
		return v.handleInputMode(msg)
	}

	switch msg := msg.(type) {
	case catalogsLoadedMsg:
		v.catalogs = []CatalogInfo(msg)
		v.loadingCatalogs = false
		if len(v.catalogs) > 0 && v.selected >= len(v.catalogs) {
			v.selected = len(v.catalogs) - 1
		}
	case spacesLoadedMsg:
		v.spaces = msg.spaces
		v.blueprints = msg.blueprints
		v.loadingSpaces = false
		if len(v.spaces) > 0 && v.spaceSelected >= len(v.spaces) {
			v.spaceSelected = len(v.spaces) - 1
		}
		if len(v.blueprints) > 0 && v.blueprintSelected >= len(v.blueprints) {
			v.blueprintSelected = len(v.blueprints) - 1
		}
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			v.tab = (v.tab + 1) % 3
		case "j", "down":
			switch v.tab {
			case 0:
				if v.selected < len(v.catalogs)-1 {
					v.selected++
				}
			case 1:
				if v.spaceSelected < len(v.spaces)-1 {
					v.spaceSelected++
				}
			case 2:
				if v.blueprintSelected < len(v.blueprints)-1 {
					v.blueprintSelected++
				}
			}
		case "k", "up":
			switch v.tab {
			case 0:
				if v.selected > 0 {
					v.selected--
				}
			case 1:
				if v.spaceSelected > 0 {
					v.spaceSelected--
				}
			case 2:
				if v.blueprintSelected > 0 {
					v.blueprintSelected--
				}
			}
		case "g":
			switch v.tab {
			case 0:
				v.selected = 0
			case 1:
				v.spaceSelected = 0
			case 2:
				v.blueprintSelected = 0
			}
		case "G":
			switch v.tab {
			case 0:
				if len(v.catalogs) > 0 {
					v.selected = len(v.catalogs) - 1
				}
			case 1:
				if len(v.spaces) > 0 {
					v.spaceSelected = len(v.spaces) - 1
				}
			case 2:
				if len(v.blueprints) > 0 {
					v.blueprintSelected = len(v.blueprints) - 1
				}
			}
		case "R":
			v.loadingCatalogs = true
			v.loadingSpaces = true
			return v, tea.Batch(v.loadCatalogs, v.loadSpaces)
		case "d":
			if v.tab == 0 && v.selected >= 0 && v.selected < len(v.catalogs) {
				action := ActionResult{
					Type:  ActionDeleteEnv,
					EnvID: v.catalogs[v.selected].Name,
				}
				return v, func() tea.Msg { return actionMsg{action: action} }
			}
		case "e":
			// Export selected catalog
			if v.tab == 0 && v.selected >= 0 && v.selected < len(v.catalogs) {
				action := ActionResult{
					Type:       ActionExportCatalog,
					EnvID:      v.catalogs[v.selected].Name,
					OutputPath: "hololib.zip",
				}
				return v, func() tea.Msg { return actionMsg{action: action} }
			}
		case "i":
			// Start import input mode
			v.inputMode = true
			v.inputBuffer = ""
		case "c":
			// Check holotree integrity
			action := ActionResult{
				Type: ActionCheckIntegrity,
			}
			return v, func() tea.Msg { return actionMsg{action: action} }
		case "/":
			// Enter search mode
			v.searchMode = true
			v.searchBuffer = v.searchFilter
		case "esc":
			// Clear filter
			if v.searchFilter != "" {
				v.searchFilter = ""
				v.selected = 0
				v.spaceSelected = 0
				v.blueprintSelected = 0
			}
		}
	}
	return v, nil
}

func (v *EnvironmentsView) handleSearchMode(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			v.searchMode = false
			v.searchBuffer = ""
		case "enter":
			v.searchFilter = v.searchBuffer
			v.searchMode = false
			v.searchBuffer = ""
			v.selected = 0
			v.spaceSelected = 0
			v.blueprintSelected = 0
		case "backspace":
			if len(v.searchBuffer) > 0 {
				v.searchBuffer = v.searchBuffer[:len(v.searchBuffer)-1]
			}
		default:
			if len(msg.String()) == 1 {
				v.searchBuffer += msg.String()
			}
		}
	}
	return v, nil
}

func (v *EnvironmentsView) handleInputMode(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			v.inputMode = false
			v.inputBuffer = ""
		case "enter":
			if v.inputBuffer != "" {
				action := ActionResult{
					Type:      ActionImportCatalog,
					InputPath: v.inputBuffer,
				}
				v.inputMode = false
				v.inputBuffer = ""
				return v, func() tea.Msg { return actionMsg{action: action} }
			}
		case "backspace":
			if len(v.inputBuffer) > 0 {
				v.inputBuffer = v.inputBuffer[:len(v.inputBuffer)-1]
			}
		default:
			if len(msg.String()) == 1 {
				v.inputBuffer += msg.String()
			}
		}
	}
	return v, nil
}

// filteredCatalogs returns catalogs matching the search filter
func (v *EnvironmentsView) filteredCatalogs() []CatalogInfo {
	if v.searchFilter == "" {
		return v.catalogs
	}
	filter := strings.ToLower(v.searchFilter)
	result := make([]CatalogInfo, 0)
	for _, c := range v.catalogs {
		if strings.Contains(strings.ToLower(c.Name), filter) ||
			strings.Contains(strings.ToLower(c.Platform), filter) {
			result = append(result, c)
		}
	}
	return result
}

// filteredSpaces returns spaces matching the search filter
func (v *EnvironmentsView) filteredSpaces() []SpaceInfo {
	if v.searchFilter == "" {
		return v.spaces
	}
	filter := strings.ToLower(v.searchFilter)
	result := make([]SpaceInfo, 0)
	for _, s := range v.spaces {
		if strings.Contains(strings.ToLower(s.Controller), filter) ||
			strings.Contains(strings.ToLower(s.Space), filter) ||
			strings.Contains(strings.ToLower(s.Blueprint), filter) {
			result = append(result, s)
		}
	}
	return result
}

// filteredBlueprints returns blueprints matching the search filter
func (v *EnvironmentsView) filteredBlueprints() []BlueprintInfo {
	if v.searchFilter == "" {
		return v.blueprints
	}
	filter := strings.ToLower(v.searchFilter)
	result := make([]BlueprintInfo, 0)
	for _, b := range v.blueprints {
		if strings.Contains(strings.ToLower(b.Hash), filter) {
			result = append(result, b)
		}
		for _, c := range b.Controllers {
			if strings.Contains(strings.ToLower(c), filter) {
				result = append(result, b)
				break
			}
		}
	}
	return result
}

// View implements View
func (v *EnvironmentsView) View() string {
	// Handle search mode view
	if v.searchMode {
		return v.renderSearchInput()
	}
	// Handle input mode view
	if v.inputMode {
		return v.renderImportInput()
	}

	theme := v.styles.theme
	vs := NewViewStyles(theme)

	// Dynamic box sizing
	boxWidth := v.width - 8
	if boxWidth < 60 {
		boxWidth = 60
	}
	contentWidth := boxWidth - 6

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 2).
		Width(boxWidth)

	var b strings.Builder

	// Header with counts
	subtitle := ""
	if !v.loadingCatalogs && !v.loadingSpaces {
		subtitle = fmt.Sprintf("(%d catalogs, %d spaces)", len(v.catalogs), len(v.spaces))
	} else if !v.loadingCatalogs {
		subtitle = fmt.Sprintf("(%d catalogs)", len(v.catalogs))
	}
	b.WriteString(RenderHeader(vs, "Holotree", subtitle, contentWidth))

	// Tab bar
	tabs := []string{"Catalogs", "Spaces", "Blueprints"}
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
	switch v.tab {
	case 0:
		b.WriteString(v.renderCatalogsContent(vs, contentWidth))
	case 1:
		b.WriteString(v.renderSpacesContent(vs, contentWidth))
	case 2:
		b.WriteString(v.renderBlueprintsContent(vs, contentWidth))
	}

	// Show search filter if active
	if v.searchFilter != "" {
		b.WriteString(vs.Info.Render(fmt.Sprintf("Filter: %q", v.searchFilter)))
		b.WriteString("  ")
		b.WriteString(vs.Subtext.Render("(Esc to clear)"))
		b.WriteString("\n")
	}

	// Footer with new keys
	b.WriteString("\n")
	hints := []KeyHint{
		{"Tab", "switch"},
		{"j/k", "nav"},
		{"/", "search"},
		{"e", "export"},
		{"i", "import"},
		{"c", "check"},
		{"d", "delete"},
		{"R", "refresh"},
	}
	b.WriteString(RenderFooter(vs, hints, contentWidth))

	return lipgloss.Place(v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		boxStyle.Render(b.String()),
	)
}

func (v *EnvironmentsView) renderSearchInput() string {
	theme := v.styles.theme
	vs := NewViewStyles(theme)

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

	b.WriteString(RenderHeader(vs, "Search", "", contentWidth))
	b.WriteString("\n")

	b.WriteString(vs.Accent.Bold(true).Render("FILTER"))
	b.WriteString("\n\n")

	b.WriteString(vs.Label.Render("Search "))
	b.WriteString(vs.Selected.Render(v.searchBuffer + "_"))
	b.WriteString("\n\n")

	b.WriteString(vs.Subtext.Render("Filter by name, platform, controller, or blueprint"))
	b.WriteString("\n\n")

	hints := []KeyHint{
		{"Enter", "apply"},
		{"Esc", "cancel"},
	}
	b.WriteString(RenderFooter(vs, hints, contentWidth))

	return lipgloss.Place(v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		boxStyle.Render(b.String()),
	)
}

func (v *EnvironmentsView) renderImportInput() string {
	theme := v.styles.theme
	vs := NewViewStyles(theme)

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

	b.WriteString(RenderHeader(vs, "Import Catalog", "", contentWidth))
	b.WriteString("\n")

	b.WriteString(vs.Accent.Bold(true).Render("HOLOLIB PATH"))
	b.WriteString("\n\n")

	b.WriteString(vs.Label.Render("Path "))
	b.WriteString(vs.Selected.Render(v.inputBuffer + "_"))
	b.WriteString("\n\n")

	b.WriteString(vs.Subtext.Render("Enter path to hololib.zip file or URL"))
	b.WriteString("\n")
	b.WriteString(vs.Subtext.Render("Examples:"))
	b.WriteString("\n")
	b.WriteString(vs.Subtext.Render("  /path/to/hololib.zip"))
	b.WriteString("\n")
	b.WriteString(vs.Subtext.Render("  https://example.com/hololib.zip"))
	b.WriteString("\n\n")

	hints := []KeyHint{
		{"Enter", "import"},
		{"Esc", "cancel"},
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

	catalogs := v.filteredCatalogs()

	if len(catalogs) == 0 {
		if v.searchFilter != "" {
			b.WriteString(vs.Subtext.Render(fmt.Sprintf("No catalogs matching %q", v.searchFilter)))
		} else {
			b.WriteString(vs.Subtext.Render("No catalogs found in holotree"))
		}
		b.WriteString("\n\n")
		b.WriteString(vs.Label.Render("Tip "))
		b.WriteString(vs.Text.Render("Run a robot to create environments"))
		return b.String()
	}

	// Catalog list
	for i, catalog := range catalogs {
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

	if v.loadingSpaces {
		b.WriteString(vs.Subtext.Render("Loading spaces..."))
		return b.String()
	}

	spaces := v.filteredSpaces()

	if len(spaces) == 0 {
		if v.searchFilter != "" {
			b.WriteString(vs.Subtext.Render(fmt.Sprintf("No spaces matching %q", v.searchFilter)))
		} else {
			b.WriteString(vs.Subtext.Render("No holotree spaces found"))
		}
		b.WriteString("\n\n")
		b.WriteString(vs.Label.Render("Tip "))
		b.WriteString(vs.Text.Render("Run a robot to create holotree spaces"))
		return b.String()
	}

	// Space list
	maxVisible := 8
	for i, space := range spaces {
		if i >= maxVisible {
			b.WriteString(vs.Subtext.Render(fmt.Sprintf("  ... +%d more (use j/k to scroll)", len(spaces)-maxVisible)))
			b.WriteString("\n")
			break
		}

		isSelected := i == v.spaceSelected

		// Build display: Controller/Space (shortened identity)
		displayName := fmt.Sprintf("%s/%s", space.Controller, space.Space)
		if len(displayName) > 30 {
			displayName = displayName[:27] + "..."
		}

		// Idle days coloring
		idleStr := space.LastUsed
		idleStyle := vs.Success
		if space.IdleDays > 30 {
			idleStyle = vs.Warning
		} else if space.IdleDays > 90 {
			idleStyle = vs.Error
		}

		if isSelected {
			b.WriteString(vs.Selected.Render("> " + displayName))
		} else {
			b.WriteString(vs.Normal.Render("  " + displayName))
		}
		b.WriteString("  ")
		b.WriteString(idleStyle.Render(idleStr))
		b.WriteString("  ")
		b.WriteString(vs.Subtext.Render("x" + space.UseCount))
		b.WriteString("\n")

		// Show details for selected space
		if isSelected {
			b.WriteString("\n")

			// Identity (truncated)
			b.WriteString(vs.Label.Render("Identity  "))
			id := space.Identity
			if len(id) > 20 {
				id = id[:17] + "..."
			}
			b.WriteString(vs.Accent.Render(id))
			b.WriteString("\n")

			// Blueprint
			b.WriteString(vs.Label.Render("Blueprint "))
			bp := space.Blueprint
			if len(bp) > 20 {
				bp = bp[:17] + "..."
			}
			b.WriteString(vs.Info.Render(bp))
			b.WriteString("\n")

			// Path (truncated)
			b.WriteString(vs.Label.Render("Path      "))
			pathStr := space.Path
			if len(pathStr) > contentWidth-16 {
				half := (contentWidth - 19) / 2
				if half > 0 {
					pathStr = pathStr[:half] + "..." + pathStr[len(pathStr)-half:]
				}
			}
			b.WriteString(vs.Subtext.Render(pathStr))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (v *EnvironmentsView) renderBlueprintsContent(vs ViewStyles, contentWidth int) string {
	var b strings.Builder

	if v.loadingSpaces {
		b.WriteString(vs.Subtext.Render("Loading blueprints..."))
		return b.String()
	}

	blueprints := v.filteredBlueprints()

	if len(blueprints) == 0 {
		if v.searchFilter != "" {
			b.WriteString(vs.Subtext.Render(fmt.Sprintf("No blueprints matching %q", v.searchFilter)))
		} else {
			b.WriteString(vs.Subtext.Render("No blueprints found"))
		}
		b.WriteString("\n\n")
		b.WriteString(vs.Label.Render("Tip "))
		b.WriteString(vs.Text.Render("Run a robot to create holotree spaces"))
		return b.String()
	}

	// Blueprint list
	for i, bp := range blueprints {
		isSelected := i == v.blueprintSelected

		// Truncate hash for display
		displayHash := bp.Hash
		if len(displayHash) > 16 {
			displayHash = displayHash[:16]
		}

		// Space count
		spaceStr := fmt.Sprintf("%d spaces", bp.SpaceCount)
		if bp.SpaceCount == 1 {
			spaceStr = "1 space"
		}

		if isSelected {
			b.WriteString(vs.Selected.Render("> " + displayHash))
		} else {
			b.WriteString(vs.Normal.Render("  " + displayHash))
		}
		b.WriteString("  ")
		b.WriteString(vs.Info.Render(spaceStr))
		b.WriteString("\n")

		// Show details for selected blueprint
		if isSelected {
			b.WriteString("\n")

			// Full hash
			b.WriteString(vs.Label.Render("Hash "))
			b.WriteString(vs.Accent.Render(bp.Hash))
			b.WriteString("\n")

			// Controllers
			b.WriteString(vs.Label.Render("Used by "))
			if len(bp.Controllers) <= 3 {
				b.WriteString(vs.Text.Render(strings.Join(bp.Controllers, ", ")))
			} else {
				b.WriteString(vs.Text.Render(strings.Join(bp.Controllers[:3], ", ")))
				b.WriteString(vs.Subtext.Render(fmt.Sprintf(" +%d more", len(bp.Controllers)-3)))
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

// Name implements View
func (v *EnvironmentsView) Name() string {
	return "Holotree"
}

// ShortHelp implements View
func (v *EnvironmentsView) ShortHelp() string {
	return "tab:switch j/k:nav e:export i:import c:check d:delete R:refresh"
}
