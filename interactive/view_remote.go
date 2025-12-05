package interactive

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joshyorko/rcc/htfs"
)

// RemoteInfo holds information about a remote RCC server
type RemoteInfo struct {
	URL        string
	Status     string // "connected", "disconnected", "checking"
	Catalogs   []string
	LastCheck  time.Time
	Error      string
	ServerInfo string
}

// RemoteView displays RCC remote server management
type RemoteView struct {
	styles       *Styles
	width        int
	height       int
	remote       RemoteInfo
	localCatalogs []string
	selected     int
	tab          int // 0 = Remote, 1 = Local, 2 = Server
	loading      bool
	serverRunning bool
}

// NewRemoteView creates a new remote view
func NewRemoteView(styles *Styles) *RemoteView {
	// Get remote origin from environment
	remoteURL := os.Getenv("RCC_REMOTE_ORIGIN")
	if remoteURL == "" {
		remoteURL = "not configured"
	}

	return &RemoteView{
		styles:   styles,
		width:    120,
		height:   30,
		remote: RemoteInfo{
			URL:    remoteURL,
			Status: "disconnected",
		},
		localCatalogs: []string{},
		selected:      0,
		tab:           0,
		loading:       true,
	}
}

// Init implements View
func (v *RemoteView) Init() tea.Cmd {
	return tea.Batch(v.loadLocalCatalogs, v.checkRemoteStatus)
}

// remoteStatusMsg contains remote server status
type remoteStatusMsg struct {
	connected  bool
	catalogs   []string
	serverInfo string
	err        error
}

// localCatalogsMsg contains local catalog list
type localCatalogsMsg struct {
	catalogs []string
}

func (v *RemoteView) loadLocalCatalogs() tea.Msg {
	catalogs := htfs.CatalogNames()
	return localCatalogsMsg{catalogs: catalogs}
}

func (v *RemoteView) checkRemoteStatus() tea.Msg {
	remoteURL := os.Getenv("RCC_REMOTE_ORIGIN")
	if remoteURL == "" {
		return remoteStatusMsg{connected: false, err: fmt.Errorf("RCC_REMOTE_ORIGIN not set")}
	}

	// Try to connect to remote server
	client := &http.Client{Timeout: 5 * time.Second}

	// Check the /parts/ endpoint which is the catalog query endpoint
	partsURL := strings.TrimSuffix(remoteURL, "/") + "/parts/"
	resp, err := client.Get(partsURL)
	if err != nil {
		return remoteStatusMsg{connected: false, err: err}
	}
	defer resp.Body.Close()

	// If we get any response, server is running
	return remoteStatusMsg{
		connected:  true,
		serverInfo: fmt.Sprintf("HTTP %d", resp.StatusCode),
	}
}

// Update implements View
func (v *RemoteView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case localCatalogsMsg:
		v.localCatalogs = msg.catalogs
		v.loading = false
	case remoteStatusMsg:
		v.loading = false
		if msg.err != nil {
			v.remote.Status = "disconnected"
			v.remote.Error = msg.err.Error()
		} else if msg.connected {
			v.remote.Status = "connected"
			v.remote.Catalogs = msg.catalogs
			v.remote.ServerInfo = msg.serverInfo
			v.remote.Error = ""
		}
		v.remote.LastCheck = time.Now()
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			v.tab = (v.tab + 1) % 3
			v.selected = 0
		case "j", "down":
			v.moveDown()
		case "k", "up":
			v.moveUp()
		case "R":
			// Refresh
			v.loading = true
			return v, tea.Batch(v.loadLocalCatalogs, v.checkRemoteStatus)
		case "e":
			// Export selected catalog
			if v.tab == 1 && v.selected < len(v.localCatalogs) {
				action := ActionResult{
					Type:  ActionExportCatalog,
					EnvID: v.localCatalogs[v.selected],
				}
				return v, func() tea.Msg { return actionMsg{action: action} }
			}
		case "p":
			// Pull from remote (placeholder)
			if v.tab == 0 && v.remote.Status == "connected" {
				// TODO: Implement pull action
			}
		case "s":
			// Start/stop local server
			if v.tab == 2 {
				action := ActionResult{
					Type: ActionToggleServer,
				}
				return v, func() tea.Msg { return actionMsg{action: action} }
			}
		}
	}
	return v, nil
}

func (v *RemoteView) moveDown() {
	maxItems := 0
	switch v.tab {
	case 0: // Remote catalogs
		maxItems = len(v.remote.Catalogs)
	case 1: // Local catalogs
		maxItems = len(v.localCatalogs)
	case 2: // Server options
		maxItems = 3
	}
	if v.selected < maxItems-1 {
		v.selected++
	}
}

func (v *RemoteView) moveUp() {
	if v.selected > 0 {
		v.selected--
	}
}

// View implements View
func (v *RemoteView) View() string {
	var b strings.Builder

	// Header
	title := v.styles.PanelTitle.Render("Remote")
	subtitle := v.styles.Subtle.Render(" Catalog Sharing & Remote Server Management")
	b.WriteString(title + subtitle)
	b.WriteString("\n\n")

	// Connection status bar
	b.WriteString(v.renderConnectionStatus())
	b.WriteString("\n\n")

	// Tab bar
	b.WriteString(v.renderTabBar())
	b.WriteString("\n")
	b.WriteString(v.styles.Divider.Render(strings.Repeat("─", min(v.width-4, 80))))
	b.WriteString("\n\n")

	// Content based on tab
	switch v.tab {
	case 0:
		b.WriteString(v.renderRemoteCatalogs())
	case 1:
		b.WriteString(v.renderLocalCatalogs())
	case 2:
		b.WriteString(v.renderServerControls())
	}

	// Footer
	b.WriteString("\n")
	b.WriteString(v.renderFooter())

	// Wrap in panel
	panelStyle := lipgloss.NewStyle().
		Padding(1, 2).
		Width(min(v.width-4, 90))

	return panelStyle.Render(b.String())
}

func (v *RemoteView) renderConnectionStatus() string {
	var b strings.Builder

	// Status indicator
	statusIcon := "[ ]"
	statusStyle := v.styles.Subtle
	statusText := "Disconnected"

	if v.loading {
		statusIcon = "[.]"
		statusStyle = v.styles.Info
		statusText = "Checking..."
	} else if v.remote.Status == "connected" {
		statusIcon := "[+]"
		statusStyle = v.styles.Success
		statusText = "Connected"
		_ = statusIcon // Use within this block
	} else if v.remote.Error != "" {
		statusIcon = "[x]"
		statusStyle = v.styles.Error
		statusText = "Error"
	}

	// Build status line
	b.WriteString(statusStyle.Render(statusIcon + " " + statusText))
	b.WriteString("  ")
	b.WriteString(v.styles.Subtle.Render("Remote: "))

	urlStyle := v.styles.Info
	if v.remote.URL == "not configured" {
		urlStyle = v.styles.Warning
	}
	b.WriteString(urlStyle.Render(v.remote.URL))

	if v.remote.ServerInfo != "" {
		b.WriteString("  ")
		b.WriteString(v.styles.Subtle.Render("(" + v.remote.ServerInfo + ")"))
	}

	return b.String()
}

func (v *RemoteView) renderTabBar() string {
	tabs := []string{"Remote Catalogs", "Local Catalogs", "Server"}
	var parts []string

	for i, tab := range tabs {
		style := v.styles.Tab
		if i == v.tab {
			style = v.styles.ActiveTab
		}
		parts = append(parts, style.Render(" "+tab+" "))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func (v *RemoteView) renderRemoteCatalogs() string {
	var b strings.Builder

	if v.remote.Status != "connected" {
		// Not connected - show help
		b.WriteString(v.styles.Subtle.Render("  Not connected to remote server"))
		b.WriteString("\n\n")

		if v.remote.URL == "not configured" {
			b.WriteString(v.styles.Warning.Render("  [!] RCC_REMOTE_ORIGIN not set"))
			b.WriteString("\n\n")
			b.WriteString(v.styles.Subtle.Render("  To connect to a remote server:"))
			b.WriteString("\n")
			b.WriteString(v.styles.Info.Render("    export RCC_REMOTE_ORIGIN=https://your-server:8443"))
			b.WriteString("\n")
		} else if v.remote.Error != "" {
			b.WriteString(v.styles.Error.Render("  Error: " + v.remote.Error))
			b.WriteString("\n")
		}

		b.WriteString("\n")
		b.WriteString(v.styles.Subtle.Render("  Press 'R' to retry connection"))
		return b.String()
	}

	// Connected - show catalogs
	if len(v.remote.Catalogs) == 0 {
		b.WriteString(v.styles.Subtle.Render("  No catalogs available on remote server"))
		b.WriteString("\n")
		b.WriteString(v.styles.Subtle.Render("  The server may not have any catalogs yet"))
		return b.String()
	}

	// Header
	b.WriteString(v.styles.TableHeader.Render("  CATALOG ID"))
	b.WriteString("\n")

	for i, cat := range v.remote.Catalogs {
		prefix := "  "
		style := v.styles.TableRow
		if i%2 == 1 {
			style = v.styles.TableRowAlt
		}
		if i == v.selected {
			prefix = v.styles.Success.Render("> ")
			style = v.styles.ListItemSelected
		}
		b.WriteString(prefix + style.Render(cat) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(v.styles.Subtle.Render(fmt.Sprintf("  %d catalog(s) available", len(v.remote.Catalogs))))

	return b.String()
}

func (v *RemoteView) renderLocalCatalogs() string {
	var b strings.Builder

	if v.loading {
		b.WriteString(v.styles.Subtle.Render("  Loading local catalogs..."))
		return b.String()
	}

	if len(v.localCatalogs) == 0 {
		b.WriteString(v.styles.Subtle.Render("  No local catalogs found"))
		b.WriteString("\n\n")
		b.WriteString(v.styles.Subtle.Render("  Build an environment to create a catalog:"))
		b.WriteString("\n")
		b.WriteString(v.styles.Info.Render("    rcc holotree vars -r robot.yaml"))
		return b.String()
	}

	// Header
	b.WriteString(v.styles.TableHeader.Render("  CATALOG ID                              ACTION"))
	b.WriteString("\n")

	for i, cat := range v.localCatalogs {
		prefix := "  "
		style := v.styles.TableRow
		if i%2 == 1 {
			style = v.styles.TableRowAlt
		}
		if i == v.selected {
			prefix = v.styles.Success.Render("> ")
			style = v.styles.ListItemSelected
		}

		// Truncate catalog ID if too long
		displayCat := cat
		if len(displayCat) > 38 {
			displayCat = displayCat[:35] + "..."
		}

		line := fmt.Sprintf("%-40s", displayCat)
		if i == v.selected {
			line += v.styles.HelpKey.Render("<e>") + v.styles.HelpDesc.Render(" export")
		}

		b.WriteString(prefix + style.Render(line) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(v.styles.Subtle.Render(fmt.Sprintf("  %d local catalog(s)", len(v.localCatalogs))))

	return b.String()
}

func (v *RemoteView) renderServerControls() string {
	var b strings.Builder

	// Server status
	serverStatus := "Stopped"
	serverIcon := "[ ]"
	serverStyle := v.styles.Subtle
	if v.serverRunning {
		serverStatus = "Running"
		serverIcon = "[+]"
		serverStyle = v.styles.Success
	}

	b.WriteString(v.styles.PanelTitle.Render("  Local RCC Remote Server"))
	b.WriteString("\n\n")

	b.WriteString("  Status: ")
	b.WriteString(serverStyle.Render(serverIcon + " " + serverStatus))
	b.WriteString("\n\n")

	// Server options
	options := []struct {
		key  string
		desc string
		info string
	}{
		{"s", "Start/Stop Server", "Launch rccremote to serve catalogs"},
		{"c", "Configure Server", "Set hostname, port, and domain"},
		{"l", "View Server Logs", "Show server output"},
	}

	for i, opt := range options {
		prefix := "  "
		style := v.styles.ListItem
		if i == v.selected {
			prefix = v.styles.Success.Render("> ")
			style = v.styles.ListItemSelected
		}

		line := v.styles.HelpKey.Render("<"+opt.key+">") + " " + style.Render(opt.desc)
		b.WriteString(prefix + line + "\n")
		if i == v.selected {
			b.WriteString("      " + v.styles.Subtle.Render(opt.info) + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(v.styles.Subtle.Render("  Server listens on: "))
	b.WriteString(v.styles.Info.Render("localhost:4653"))
	b.WriteString("\n")
	b.WriteString(v.styles.Subtle.Render("  Requires shared holotree to be enabled"))

	return b.String()
}

func (v *RemoteView) renderFooter() string {
	var hints []string

	hints = append(hints, v.styles.HelpKey.Render("<tab>")+" "+v.styles.HelpDesc.Render("switch"))
	hints = append(hints, v.styles.HelpKey.Render("<j/k>")+" "+v.styles.HelpDesc.Render("nav"))
	hints = append(hints, v.styles.HelpKey.Render("<R>")+" "+v.styles.HelpDesc.Render("refresh"))

	switch v.tab {
	case 0:
		if v.remote.Status == "connected" {
			hints = append(hints, v.styles.HelpKey.Render("<p>")+" "+v.styles.HelpDesc.Render("pull"))
		}
	case 1:
		hints = append(hints, v.styles.HelpKey.Render("<e>")+" "+v.styles.HelpDesc.Render("export"))
	case 2:
		hints = append(hints, v.styles.HelpKey.Render("<s>")+" "+v.styles.HelpDesc.Render("start/stop"))
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, hints...)
}

// Name implements View
func (v *RemoteView) Name() string {
	return "Remote"
}

// ShortHelp implements View
func (v *RemoteView) ShortHelp() string {
	return "tab:switch j/k:nav e:export R:refresh"
}

// Note: min() helper is defined in view_robots.go
