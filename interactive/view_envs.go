package interactive

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/joshyorko/rcc/htfs"
)

// EnvViewMode represents the current view mode
type EnvViewMode int

const (
	EnvModeCatalogs EnvViewMode = iota
	EnvModeSpaces
	EnvModeDetail
	EnvModeConfirmDelete
)

// SpaceInfo holds information about a holotree space
type SpaceInfo struct {
	Name       string
	Path       string
	MetaFile   string
	Controller string
	Space      string
	Platform   string
	Blueprint  string
}

// CatalogInfo holds information about a catalog
type CatalogInfo struct {
	Name     string
	Hash     string
	Platform string
	FullPath string
}

// EnvironmentsView displays holotree environments
type EnvironmentsView struct {
	styles        *Styles
	catalogs      []CatalogInfo
	spaces        []SpaceInfo
	cursor        int
	mode          EnvViewMode
	statusMsg     string
	detailTab     int // 0 = info, 1 = files
	scrollY       int
	selectedSpace string // space being deleted
}

// NewEnvironmentsView creates a new environments view
func NewEnvironmentsView(styles *Styles) *EnvironmentsView {
	v := &EnvironmentsView{
		styles: styles,
		mode:   EnvModeCatalogs,
	}
	v.loadData()
	return v
}

// Init implements View
func (v *EnvironmentsView) Init() tea.Cmd {
	return nil
}

// loadData loads both catalogs and spaces
func (v *EnvironmentsView) loadData() {
	v.loadCatalogs()
	v.loadSpaces()
}

// loadCatalogs loads the holotree catalogs
func (v *EnvironmentsView) loadCatalogs() {
	v.catalogs = nil

	catalogNames := htfs.CatalogNames()
	for _, name := range catalogNames {
		// Parse catalog name (format: hashv12.platform)
		parts := strings.Split(name, ".")
		platform := ""
		if len(parts) > 1 {
			platform = parts[len(parts)-1]
		}

		// Extract hash (remove version suffix)
		hash := parts[0]
		if idx := strings.Index(hash, "v"); idx > 0 {
			hash = hash[:idx]
		}

		v.catalogs = append(v.catalogs, CatalogInfo{
			Name:     name,
			Hash:     hash,
			Platform: platform,
		})
	}
}

// loadSpaces loads the holotree spaces
func (v *EnvironmentsView) loadSpaces() {
	v.spaces = nil

	// Get catalog roots to access spaces
	_, roots := htfs.LoadCatalogs()
	spaces := roots.Spaces()

	for _, root := range spaces {
		if root == nil || root.Info == nil {
			continue
		}
		v.spaces = append(v.spaces, SpaceInfo{
			Name:       root.Identity,
			Path:       root.Path,
			Controller: root.Controller,
			Space:      root.Space,
			Platform:   root.Platform,
			Blueprint:  root.Blueprint,
		})
	}
}

// envRefreshMsg signals refresh completed
type envRefreshMsg struct{}

// envDeleteMsg signals delete operation result
type envDeleteMsg struct {
	err error
}

// Update implements View
func (v *EnvironmentsView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case envRefreshMsg:
		v.loadData()
		v.statusMsg = "Refreshed"
		return v, nil

	case envDeleteMsg:
		if msg.err != nil {
			v.statusMsg = "Delete failed: " + msg.err.Error()
		} else {
			v.statusMsg = "Space deleted successfully"
			v.loadData()
		}
		v.mode = EnvModeCatalogs
		return v, nil

	case tea.KeyMsg:
		switch v.mode {
		case EnvModeConfirmDelete:
			return v.updateConfirmMode(msg)
		case EnvModeDetail:
			return v.updateDetailMode(msg)
		default:
			return v.updateListMode(msg)
		}
	}
	return v, nil
}

func (v *EnvironmentsView) updateListMode(msg tea.KeyMsg) (View, tea.Cmd) {
	listLen := v.currentListLength()

	switch {
	case key.Matches(msg, keys.Down):
		if v.cursor < listLen-1 {
			v.cursor++
		}
	case key.Matches(msg, keys.Up):
		if v.cursor > 0 {
			v.cursor--
		}
	case key.Matches(msg, keys.Top):
		v.cursor = 0
	case key.Matches(msg, keys.Bottom):
		if listLen > 0 {
			v.cursor = listLen - 1
		}
	case key.Matches(msg, keys.Refresh):
		v.loadData()
		v.statusMsg = "Refreshed"

	case msg.String() == "tab":
		// Switch between catalogs and spaces view
		if v.mode == EnvModeCatalogs {
			v.mode = EnvModeSpaces
		} else {
			v.mode = EnvModeCatalogs
		}
		v.cursor = 0
		v.statusMsg = ""

	case key.Matches(msg, keys.Select), key.Matches(msg, keys.Right):
		// Enter detail mode for spaces
		if v.mode == EnvModeSpaces && v.cursor < len(v.spaces) {
			v.mode = EnvModeDetail
			v.detailTab = 0
			v.scrollY = 0
		}

	case msg.String() == "d":
		// Delete - only for spaces mode
		if v.mode == EnvModeSpaces && v.cursor < len(v.spaces) {
			v.selectedSpace = v.spaces[v.cursor].Name
			v.mode = EnvModeConfirmDelete
		}

	case msg.String() == "e":
		// Export catalog (run rcc holotree export)
		if v.mode == EnvModeCatalogs && v.cursor < len(v.catalogs) {
			return v, v.exportCatalog()
		}

	case msg.String() == "c":
		// Check holotree integrity
		return v, v.runHolotreeCheck()
	}
	return v, nil
}

func (v *EnvironmentsView) updateConfirmMode(msg tea.KeyMsg) (View, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		// Confirm delete
		return v, v.deleteSpace(v.selectedSpace)
	case "n", "N", "esc", "q":
		// Cancel delete
		v.mode = EnvModeSpaces
		v.selectedSpace = ""
		v.statusMsg = "Delete cancelled"
	}
	return v, nil
}

func (v *EnvironmentsView) updateDetailMode(msg tea.KeyMsg) (View, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back), key.Matches(msg, keys.Left), msg.String() == "q":
		v.mode = EnvModeSpaces
	case key.Matches(msg, keys.Down):
		v.scrollY++
	case key.Matches(msg, keys.Up):
		if v.scrollY > 0 {
			v.scrollY--
		}
	case msg.String() == "tab":
		v.detailTab = (v.detailTab + 1) % 2
		v.scrollY = 0
	}
	return v, nil
}

func (v *EnvironmentsView) currentListLength() int {
	if v.mode == EnvModeSpaces {
		return len(v.spaces)
	}
	return len(v.catalogs)
}

// deleteSpace removes a holotree space
func (v *EnvironmentsView) deleteSpace(spaceName string) tea.Cmd {
	return func() tea.Msg {
		_, roots := htfs.LoadCatalogs()
		spaces := roots.Spaces()
		err := spaces.RemoveHolotreeSpace(spaceName)
		return envDeleteMsg{err: err}
	}
}

// exportCatalog opens rcc holotree export command
func (v *EnvironmentsView) exportCatalog() tea.Cmd {
	if v.cursor >= len(v.catalogs) {
		return nil
	}

	catalog := v.catalogs[v.cursor]
	executable, err := os.Executable()
	if err != nil {
		executable = "rcc"
	}

	// Create export filename
	exportFile := fmt.Sprintf("holotree-export-%s.zip", catalog.Hash[:8])

	c := exec.Command(executable, "holotree", "export", "-z", exportFile)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return envDeleteMsg{err: fmt.Errorf("export failed: %v", err)}
		}
		return envRefreshMsg{}
	})
}

// runHolotreeCheck runs integrity check
func (v *EnvironmentsView) runHolotreeCheck() tea.Cmd {
	executable, err := os.Executable()
	if err != nil {
		executable = "rcc"
	}

	c := exec.Command(executable, "holotree", "check")
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return envRefreshMsg{}
	})
}

// View implements View
func (v *EnvironmentsView) View() string {
	switch v.mode {
	case EnvModeConfirmDelete:
		return v.renderConfirmDelete()
	case EnvModeDetail:
		return v.renderDetailView()
	default:
		return v.renderListView()
	}
}

func (v *EnvironmentsView) renderListView() string {
	var b strings.Builder

	// Header
	header := v.styles.Info.Render("####") + "  " + v.styles.PanelTitle.Render("Holotree Environments") + "  " + v.styles.Info.Render("####")
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(v.styles.Subtle.Render("    Cached Python environments and spaces"))
	b.WriteString("\n\n")

	// Tabs
	b.WriteString(v.renderTabs())
	b.WriteString("\n\n")

	// Content based on mode
	if v.mode == EnvModeSpaces {
		b.WriteString(v.renderSpacesList())
	} else {
		b.WriteString(v.renderCatalogsList())
	}

	// Status message
	if v.statusMsg != "" {
		b.WriteString("\n")
		b.WriteString(v.styles.Info.Render("    " + v.statusMsg))
		b.WriteString("\n")
	}

	return b.String()
}

func (v *EnvironmentsView) renderTabs() string {
	catalogTab := " Catalogs "
	spacesTab := " Spaces "

	var tabs []string
	if v.mode == EnvModeCatalogs {
		tabs = append(tabs, v.styles.ActiveTab.Render(catalogTab))
		tabs = append(tabs, v.styles.Tab.Render(spacesTab))
	} else {
		tabs = append(tabs, v.styles.Tab.Render(catalogTab))
		tabs = append(tabs, v.styles.ActiveTab.Render(spacesTab))
	}

	return "    " + strings.Join(tabs, "  ")
}

func (v *EnvironmentsView) renderCatalogsList() string {
	var b strings.Builder

	if len(v.catalogs) == 0 {
		b.WriteString(v.styles.Warning.Render("    No catalogs found."))
		b.WriteString("\n\n")
		b.WriteString(v.styles.Subtle.Render("    Run a robot to create environment catalogs."))
		return b.String()
	}

	// Table header
	header := fmt.Sprintf("    %-16s %-12s %s", "HASH", "PLATFORM", "CATALOG")
	b.WriteString(v.styles.TableHeader.Render(header))
	b.WriteString("\n")

	// Catalog list
	for i, catalog := range v.catalogs {
		hash := catalog.Hash
		if len(hash) > 14 {
			hash = hash[:14] + ".."
		}

		platform := catalog.Platform
		if len(platform) > 10 {
			platform = platform[:10] + ".."
		}

		row := fmt.Sprintf("    %-16s %-12s %s", hash, platform, catalog.Name)

		if i == v.cursor {
			b.WriteString(v.styles.ListItemSelected.Render(row))
		} else if i%2 == 0 {
			b.WriteString(v.styles.TableRow.Render(row))
		} else {
			b.WriteString(v.styles.TableRowAlt.Render(row))
		}
		b.WriteString("\n")
	}

	// Summary
	b.WriteString("\n")
	b.WriteString(v.styles.Subtle.Render(fmt.Sprintf("    Total: %d catalogs", len(v.catalogs))))

	return b.String()
}

func (v *EnvironmentsView) renderSpacesList() string {
	var b strings.Builder

	if len(v.spaces) == 0 {
		b.WriteString(v.styles.Warning.Render("    No spaces found."))
		b.WriteString("\n\n")
		b.WriteString(v.styles.Subtle.Render("    Spaces are created when running robots in named holotree spaces."))
		return b.String()
	}

	// Table header
	header := fmt.Sprintf("    %-20s %-12s %-12s %s", "CONTROLLER", "SPACE", "PLATFORM", "NAME")
	b.WriteString(v.styles.TableHeader.Render(header))
	b.WriteString("\n")

	// Space list
	for i, space := range v.spaces {
		controller := space.Controller
		if len(controller) > 18 {
			controller = controller[:18] + ".."
		}

		spaceName := space.Space
		if len(spaceName) > 10 {
			spaceName = spaceName[:10] + ".."
		}

		platform := space.Platform
		if len(platform) > 10 {
			platform = platform[:10] + ".."
		}

		name := space.Name
		if len(name) > 20 {
			name = name[:20] + ".."
		}

		row := fmt.Sprintf("    %-20s %-12s %-12s %s", controller, spaceName, platform, name)

		if i == v.cursor {
			b.WriteString(v.styles.ListItemSelected.Render(row))
		} else if i%2 == 0 {
			b.WriteString(v.styles.TableRow.Render(row))
		} else {
			b.WriteString(v.styles.TableRowAlt.Render(row))
		}
		b.WriteString("\n")
	}

	// Details panel for selected space
	if v.cursor < len(v.spaces) {
		space := v.spaces[v.cursor]
		b.WriteString("\n")

		detailHeader := v.styles.Info.Render("####") + "  " + v.styles.PanelTitle.Render("Space Details") + "  " + v.styles.Info.Render("####")
		b.WriteString(detailHeader)
		b.WriteString("\n\n")

		b.WriteString("    " + v.styles.Subtle.Render("Name:       ") + v.styles.Highlight.Render(space.Name) + "\n")
		b.WriteString("    " + v.styles.Subtle.Render("Path:       ") + v.styles.Info.Render(v.truncatePath(space.Path, 50)) + "\n")
		b.WriteString("    " + v.styles.Subtle.Render("Controller: ") + v.styles.Warning.Render(space.Controller) + "\n")
		b.WriteString("    " + v.styles.Subtle.Render("Space:      ") + v.styles.Success.Render(space.Space) + "\n")
		b.WriteString("    " + v.styles.Subtle.Render("Platform:   ") + v.styles.Subtle.Render(space.Platform) + "\n")
		if space.Blueprint != "" {
			blueprintShort := space.Blueprint
			if len(blueprintShort) > 16 {
				blueprintShort = blueprintShort[:16] + ".."
			}
			b.WriteString("    " + v.styles.Subtle.Render("Blueprint:  ") + v.styles.Info.Render(blueprintShort) + "\n")
		}
	}

	// Summary
	b.WriteString("\n")
	b.WriteString(v.styles.Subtle.Render(fmt.Sprintf("    Total: %d spaces", len(v.spaces))))

	return b.String()
}

func (v *EnvironmentsView) renderConfirmDelete() string {
	var b strings.Builder

	// Header
	header := v.styles.Info.Render("####") + "  " + v.styles.Warning.Render("Confirm Delete") + "  " + v.styles.Info.Render("####")
	b.WriteString(header)
	b.WriteString("\n\n")

	b.WriteString(v.styles.Warning.Render("    ⚠ WARNING: This action cannot be undone!"))
	b.WriteString("\n\n")

	b.WriteString("    " + v.styles.Subtle.Render("You are about to delete space:") + "\n")
	b.WriteString("    " + v.styles.Error.Render(v.selectedSpace) + "\n\n")

	b.WriteString("    " + v.styles.Subtle.Render("This will remove:") + "\n")
	b.WriteString("      " + v.styles.Subtle.Render("• The environment directory") + "\n")
	b.WriteString("      " + v.styles.Subtle.Render("• Associated metadata files") + "\n")
	b.WriteString("      " + v.styles.Subtle.Render("• Lock files") + "\n\n")

	// Find the space to show path
	for _, space := range v.spaces {
		if space.Name == v.selectedSpace {
			b.WriteString("    " + v.styles.Subtle.Render("Path: ") + v.styles.Info.Render(space.Path) + "\n\n")
			break
		}
	}

	// Prompt
	b.WriteString("\n")
	prompt := v.styles.HelpKey.Render("    <y>") + v.styles.HelpDesc.Render(" Confirm delete  ") +
		v.styles.HelpKey.Render("<n/Esc>") + v.styles.HelpDesc.Render(" Cancel")
	b.WriteString(prompt)

	return b.String()
}

func (v *EnvironmentsView) renderDetailView() string {
	if v.cursor >= len(v.spaces) {
		return "No space selected"
	}

	space := v.spaces[v.cursor]
	var b strings.Builder

	// Header
	header := v.styles.Title.Render(space.Name) + "  " + v.styles.Subtle.Render(filepath.Dir(space.Path))
	b.WriteString(header)
	b.WriteString("\n\n")

	// Tabs
	tabs := []string{"Info", "Actions"}
	var tabStrs []string
	for i, name := range tabs {
		if i == v.detailTab {
			tabStrs = append(tabStrs, v.styles.ActiveTab.Render(" "+name+" "))
		} else {
			tabStrs = append(tabStrs, v.styles.Tab.Render(" "+name+" "))
		}
	}
	b.WriteString(strings.Join(tabStrs, "  "))
	b.WriteString("\n\n")

	// Content based on tab
	switch v.detailTab {
	case 0:
		b.WriteString(v.renderSpaceInfo(space))
	case 1:
		b.WriteString(v.renderSpaceActions(space))
	}

	return b.String()
}

func (v *EnvironmentsView) renderSpaceInfo(space SpaceInfo) string {
	var b strings.Builder

	// Info section
	infoHeader := v.styles.PanelTitle.Render("Environment Information")
	b.WriteString(infoHeader)
	b.WriteString("\n\n")

	rows := []struct{ label, value string }{
		{"Identity", space.Name},
		{"Path", space.Path},
		{"Controller", space.Controller},
		{"Space", space.Space},
		{"Platform", space.Platform},
		{"Blueprint", space.Blueprint},
	}

	for _, row := range rows {
		label := v.styles.Subtle.Render(fmt.Sprintf("  %-12s ", row.label))
		value := v.styles.Highlight.Render(row.value)
		b.WriteString(label + value + "\n")
	}

	return b.String()
}

func (v *EnvironmentsView) renderSpaceActions(space SpaceInfo) string {
	var b strings.Builder

	actionsHeader := v.styles.PanelTitle.Render("Available Actions")
	b.WriteString(actionsHeader)
	b.WriteString("\n\n")

	actions := []struct{ key, desc string }{
		{"d", "Delete this space"},
		{"c", "Run integrity check"},
		{"R", "Refresh environment list"},
	}

	for _, action := range actions {
		keyStr := v.styles.HelpKey.Render("  <" + action.key + "> ")
		descStr := v.styles.HelpDesc.Render(action.desc)
		b.WriteString(keyStr + descStr + "\n")
	}

	return b.String()
}

func (v *EnvironmentsView) truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	return "..." + path[len(path)-(maxLen-3):]
}

// Name implements View
func (v *EnvironmentsView) Name() string {
	return "Environments"
}

// ShortHelp implements View
func (v *EnvironmentsView) ShortHelp() string {
	switch v.mode {
	case EnvModeConfirmDelete:
		return "y:confirm  n/Esc:cancel"
	case EnvModeDetail:
		return "tab:switch  j/k:scroll  q:back"
	case EnvModeSpaces:
		return "j/k:nav  tab:catalogs  d:delete  enter:detail  R:refresh"
	default:
		return "j/k:nav  tab:spaces  e:export  c:check  R:refresh"
	}
}
