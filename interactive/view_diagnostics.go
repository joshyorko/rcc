package interactive

import (
	"fmt"
	"os"
	"os/user"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/conda"
	"github.com/joshyorko/rcc/htfs"
)

// DiagnosticsView displays system diagnostics and health checks
type DiagnosticsView struct {
	styles  *Styles
	width   int
	height  int
	info    DiagnosticsInfo
	loading bool
}

// DiagnosticsInfo holds diagnostic information
type DiagnosticsInfo struct {
	// System
	Hostname   string
	Username   string
	OS         string
	Arch       string
	HomeDir    string
	WorkingDir string

	// RCC
	RCCVersion       string
	RCCPath          string
	MicromambaVer    string
	ProductHome      string
	ControllerID     string
	RemoteOrigin     string

	// Holotree
	HolotreePath     string
	CatalogCount     int
	SpaceCount       int
	SharedEnabled    bool

	// Status checks
	LongPathSupport  bool
	HolotreeWritable bool
}

// NewDiagnosticsView creates a new diagnostics view
func NewDiagnosticsView(styles *Styles) *DiagnosticsView {
	return &DiagnosticsView{
		styles:  styles,
		width:   120,
		height:  30,
		loading: true,
	}
}

// Init implements View
func (v *DiagnosticsView) Init() tea.Cmd {
	return v.loadDiagnostics
}

type diagnosticsLoadedMsg DiagnosticsInfo

func (v *DiagnosticsView) loadDiagnostics() tea.Msg {
	info := DiagnosticsInfo{}

	// System info
	info.Hostname, _ = os.Hostname()
	if who, err := user.Current(); err == nil {
		info.Username = who.Username
	}
	info.OS = runtime.GOOS
	info.Arch = runtime.GOARCH
	info.HomeDir, _ = os.UserHomeDir()
	info.WorkingDir, _ = os.Getwd()

	// RCC info
	info.RCCVersion = common.Version
	info.RCCPath = common.BinRcc()
	info.MicromambaVer = conda.MicromambaVersion()
	info.ProductHome = common.Product.Home()
	info.ControllerID = common.ControllerIdentity()
	info.RemoteOrigin = common.RccRemoteOrigin()

	// Holotree info
	info.HolotreePath = common.Product.HoloLocation()
	info.CatalogCount = len(htfs.CatalogNames())

	_, roots := htfs.LoadCatalogs()
	info.SpaceCount = len(roots.Spaces())

	// Check shared holotree
	sharedMarker := common.SharedMarkerLocation()
	if _, err := os.Stat(sharedMarker); err == nil {
		info.SharedEnabled = true
	}

	// Check long path support
	info.LongPathSupport = conda.HasLongPathSupport()

	// Check holotree writable
	testFile := fmt.Sprintf("%s/.rcc_test_%d", info.ProductHome, os.Getpid())
	if f, err := os.Create(testFile); err == nil {
		f.Close()
		os.Remove(testFile)
		info.HolotreeWritable = true
	}

	return diagnosticsLoadedMsg(info)
}

// Update implements View
func (v *DiagnosticsView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case diagnosticsLoadedMsg:
		v.info = DiagnosticsInfo(msg)
		v.loading = false
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "R":
			v.loading = true
			return v, v.loadDiagnostics
		case "d":
			// Run full diagnostics
			action := ActionResult{
				Type:    ActionRunCommand,
				Command: "rcc configure diagnostics",
			}
			return v, func() tea.Msg { return actionMsg{action: action} }
		}
	}
	return v, nil
}

// View implements View
func (v *DiagnosticsView) View() string {
	theme := v.styles.theme
	vs := NewViewStyles(theme)

	boxWidth := v.width - 8
	if boxWidth < 60 {
		boxWidth = 60
	}
	if boxWidth > 120 {
		boxWidth = 120
	}
	contentWidth := boxWidth - 6

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 2).
		Width(boxWidth)

	var b strings.Builder

	b.WriteString(RenderHeader(vs, "Diagnostics", "System Health", contentWidth))

	if v.loading {
		b.WriteString(vs.Subtext.Render("Loading diagnostics..."))
	} else {
		// System section
		b.WriteString(vs.Accent.Bold(true).Render("SYSTEM"))
		b.WriteString("\n\n")

		b.WriteString(vs.Label.Render("Host     "))
		b.WriteString(vs.Text.Render(v.info.Hostname))
		b.WriteString("\n")

		b.WriteString(vs.Label.Render("User     "))
		b.WriteString(vs.Text.Render(v.info.Username))
		b.WriteString("\n")

		b.WriteString(vs.Label.Render("Platform "))
		b.WriteString(vs.Text.Render(fmt.Sprintf("%s/%s", v.info.OS, v.info.Arch)))
		b.WriteString("\n\n")

		// RCC section
		b.WriteString(vs.Accent.Bold(true).Render("RCC"))
		b.WriteString("\n\n")

		b.WriteString(vs.Label.Render("Version    "))
		b.WriteString(vs.Info.Render(v.info.RCCVersion))
		b.WriteString("\n")

		b.WriteString(vs.Label.Render("Micromamba "))
		ver := v.info.MicromambaVer
		if ver == "" {
			ver = "(not found)"
		}
		b.WriteString(vs.Text.Render(ver))
		b.WriteString("\n")

		b.WriteString(vs.Label.Render("Controller "))
		b.WriteString(vs.Text.Render(v.info.ControllerID))
		b.WriteString("\n")

		if v.info.RemoteOrigin != "" {
			b.WriteString(vs.Label.Render("Remote     "))
			origin := v.info.RemoteOrigin
			if len(origin) > 40 {
				origin = origin[:37] + "..."
			}
			b.WriteString(vs.Info.Render(origin))
			b.WriteString("\n")
		}
		b.WriteString("\n")

		// Holotree section
		b.WriteString(vs.Accent.Bold(true).Render("HOLOTREE"))
		b.WriteString("\n\n")

		b.WriteString(vs.Label.Render("Catalogs "))
		b.WriteString(vs.Text.Render(fmt.Sprintf("%d", v.info.CatalogCount)))
		b.WriteString("\n")

		b.WriteString(vs.Label.Render("Spaces   "))
		b.WriteString(vs.Text.Render(fmt.Sprintf("%d", v.info.SpaceCount)))
		b.WriteString("\n")

		b.WriteString(vs.Label.Render("Shared   "))
		if v.info.SharedEnabled {
			b.WriteString(vs.Success.Render("enabled"))
		} else {
			b.WriteString(vs.Subtext.Render("disabled"))
		}
		b.WriteString("\n\n")

		// Status checks
		b.WriteString(vs.Accent.Bold(true).Render("STATUS"))
		b.WriteString("\n\n")

		b.WriteString(vs.Label.Render("Long paths "))
		if v.info.LongPathSupport {
			b.WriteString(vs.Success.Render("[OK]"))
		} else {
			b.WriteString(vs.Warning.Render("[WARN]"))
		}
		b.WriteString("\n")

		b.WriteString(vs.Label.Render("Writable   "))
		if v.info.HolotreeWritable {
			b.WriteString(vs.Success.Render("[OK]"))
		} else {
			b.WriteString(vs.Error.Render("[FAIL]"))
		}
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("\n")
	hints := []KeyHint{
		{"d", "full diag"},
		{"R", "refresh"},
	}
	b.WriteString(RenderFooter(vs, hints, contentWidth))

	return lipgloss.Place(v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		boxStyle.Render(b.String()),
	)
}

// Name implements View
func (v *DiagnosticsView) Name() string {
	return "Diagnostics"
}

// ShortHelp implements View
func (v *DiagnosticsView) ShortHelp() string {
	return "d:full-diagnostics R:refresh"
}
