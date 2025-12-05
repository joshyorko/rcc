package interactive

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/htfs"
)

// RemoteView displays RCC remote server management - focused on CLIENT operations
type RemoteView struct {
	styles          *Styles
	width           int
	height          int
	tab             int // 0 = Connect, 1 = Servers, 2 = Pull, 3 = Host
	selected        int
	loading         bool
	profiles        *ServerProfiles
	currentOrigin   string
	connectionOK    bool
	lastCheck       time.Time
	checkError      string
	localCatalogs   []string
	remoteCatalogs  []string
	sharedEnabled   bool
	inputMode       bool
	inputBuffer     string
	inputField      string // "name", "url", "auth"
	editingProfile  *ServerProfile

	// Host tab - server management
	serverBinaryPath string
	serverInstalled  bool
	serverRunning    bool
	serverPID        int
	serverHostname   string
	serverPort       int
	serverDomain     string
	downloadProgress string
}

// NewRemoteView creates a new remote view
func NewRemoteView(styles *Styles) *RemoteView {
	profiles, _ := LoadServerProfiles()

	// Determine rccremote binary path
	binPath := getRccremotePath()

	// Check if installed
	installed := false
	if _, err := os.Stat(binPath); err == nil {
		installed = true
	}

	return &RemoteView{
		styles:           styles,
		width:            120,
		height:           30,
		tab:              0,
		selected:         0,
		loading:          true,
		profiles:         profiles,
		currentOrigin:    common.RccRemoteOrigin(),
		sharedEnabled:    isSharedHolotreeEnabled(),
		serverBinaryPath: binPath,
		serverInstalled:  installed,
		serverHostname:   "0.0.0.0",
		serverPort:       4653,
		serverDomain:     "personal",
	}
}

// isSharedHolotreeEnabled checks if shared holotree is enabled
func isSharedHolotreeEnabled() bool {
	// Check the system-level shared holotree marker
	// This is at /opt/robocorp/ht/shared.yes on Linux
	sharedMarker := common.SharedMarkerLocation()
	if _, err := os.Stat(sharedMarker); err == nil {
		return true
	}
	// Also check the user-level marker as fallback
	userFile := common.HoloInitUserFile()
	if _, err := os.Stat(userFile); err == nil {
		return true
	}
	return false
}

// getRccremotePath returns the path where rccremote binary should be stored
func getRccremotePath() string {
	binDir := common.BinLocation()
	if runtime.GOOS == "windows" {
		return filepath.Join(binDir, "rccremote.exe")
	}
	return filepath.Join(binDir, "rccremote")
}

// getGitHubAssetName returns the asset name for current platform
func getGitHubAssetName() string {
	switch runtime.GOOS {
	case "windows":
		return "rccremote-windows64.exe"
	case "darwin":
		return "rccremote-darwin64"
	default:
		return "rccremote-linux64"
	}
}

// Message types for server management
type serverDownloadMsg struct {
	success bool
	err     string
}

type serverStartMsg struct {
	success bool
	pid     int
	err     string
}

type serverStopMsg struct {
	success bool
	err     string
}

type serverStatusMsg struct {
	running bool
	pid     int
}

// Init implements View
func (v *RemoteView) Init() tea.Cmd {
	return tea.Batch(v.checkConnection, v.loadCatalogs, v.checkServerStatus)
}

// checkServerStatus checks if rccremote is running
func (v *RemoteView) checkServerStatus() tea.Msg {
	// Check if our PID file exists
	pidFile := filepath.Join(common.Product.Home(), "rccremote.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return serverStatusMsg{running: false}
	}

	var pid int
	fmt.Sscanf(string(data), "%d", &pid)
	if pid > 0 {
		// Check if process is still running
		process, err := os.FindProcess(pid)
		if err == nil && process != nil {
			// On Unix, FindProcess always succeeds, need to send signal 0 to check
			if runtime.GOOS != "windows" {
				err = process.Signal(syscall.Signal(0))
				if err == nil {
					return serverStatusMsg{running: true, pid: pid}
				}
			} else {
				// On Windows, try to open the process
				return serverStatusMsg{running: true, pid: pid}
			}
		}
	}
	// PID file exists but process not running - clean up
	os.Remove(pidFile)
	return serverStatusMsg{running: false}
}

// downloadRccremote downloads rccremote from GitHub releases
func (v *RemoteView) downloadRccremote() tea.Msg {
	// Get latest release info from GitHub API
	client := &http.Client{Timeout: 30 * time.Second}

	// Fetch latest release
	apiURL := "https://api.github.com/repos/joshyorko/rcc/releases/latest"
	resp, err := client.Get(apiURL)
	if err != nil {
		return serverDownloadMsg{success: false, err: "Failed to fetch release info: " + err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return serverDownloadMsg{success: false, err: fmt.Sprintf("GitHub API returned %d", resp.StatusCode)}
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return serverDownloadMsg{success: false, err: "Failed to parse release info: " + err.Error()}
	}

	// Find the right asset for this platform
	assetName := getGitHubAssetName()
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return serverDownloadMsg{success: false, err: "No release asset found for " + assetName}
	}

	// Download the binary
	resp, err = client.Get(downloadURL)
	if err != nil {
		return serverDownloadMsg{success: false, err: "Download failed: " + err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return serverDownloadMsg{success: false, err: fmt.Sprintf("Download returned %d", resp.StatusCode)}
	}

	// Ensure bin directory exists
	binDir := common.BinLocation()
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return serverDownloadMsg{success: false, err: "Failed to create bin directory: " + err.Error()}
	}

	// Write to file
	binPath := getRccremotePath()
	out, err := os.Create(binPath)
	if err != nil {
		return serverDownloadMsg{success: false, err: "Failed to create file: " + err.Error()}
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return serverDownloadMsg{success: false, err: "Failed to write file: " + err.Error()}
	}

	// Make executable on Unix
	if runtime.GOOS != "windows" {
		if err := os.Chmod(binPath, 0755); err != nil {
			return serverDownloadMsg{success: false, err: "Failed to make executable: " + err.Error()}
		}
	}

	return serverDownloadMsg{success: true}
}

// startServer starts rccremote as a fully detached process
func (v *RemoteView) startServer() tea.Msg {
	if !v.serverInstalled {
		return serverStartMsg{success: false, err: "rccremote not installed"}
	}

	if !v.sharedEnabled {
		return serverStartMsg{success: false, err: "Shared holotree must be enabled first"}
	}

	// Create log file for server output
	logFile := filepath.Join(common.Product.Home(), "rccremote.log")

	// Use nohup-style approach: redirect to file, detach completely
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command(v.serverBinaryPath,
			"-hostname", v.serverHostname,
			"-port", fmt.Sprintf("%d", v.serverPort),
			"-domain", v.serverDomain,
		)
	} else {
		// On Unix, use shell to properly detach with nohup behavior
		shellCmd := fmt.Sprintf("nohup %s -hostname %s -port %d -domain %s > %s 2>&1 & echo $!",
			v.serverBinaryPath, v.serverHostname, v.serverPort, v.serverDomain, logFile)
		cmd = exec.Command("sh", "-c", shellCmd)
	}

	output, err := cmd.Output()
	if err != nil {
		return serverStartMsg{success: false, err: err.Error()}
	}

	// Parse PID from output (the echo $! part)
	var pid int
	fmt.Sscanf(strings.TrimSpace(string(output)), "%d", &pid)

	if pid > 0 {
		// Write PID file
		pidFile := filepath.Join(common.Product.Home(), "rccremote.pid")
		os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", pid)), 0644)
	}

	// Give server a moment to start, then verify it's running
	time.Sleep(500 * time.Millisecond)

	// Check if process is actually running
	if pid > 0 {
		if process, err := os.FindProcess(pid); err == nil {
			// On Unix, send signal 0 to check if process exists
			if runtime.GOOS != "windows" {
				if err := process.Signal(syscall.Signal(0)); err != nil {
					// Process not running - read log for error
					if logData, err := os.ReadFile(logFile); err == nil && len(logData) > 0 {
						errMsg := string(logData)
						if len(errMsg) > 200 {
							errMsg = errMsg[:200] + "..."
						}
						return serverStartMsg{success: false, err: "Server exited: " + errMsg}
					}
					return serverStartMsg{success: false, err: "Server failed to start"}
				}
			}
		}
	}

	return serverStartMsg{success: true, pid: pid}
}

// stopServer stops the running rccremote
func (v *RemoteView) stopServer() tea.Msg {
	if v.serverPID <= 0 {
		return serverStopMsg{success: false, err: "No server running"}
	}

	process, err := os.FindProcess(v.serverPID)
	if err != nil {
		return serverStopMsg{success: false, err: err.Error()}
	}

	if err := process.Kill(); err != nil {
		return serverStopMsg{success: false, err: err.Error()}
	}

	// Remove PID file
	pidFile := filepath.Join(common.Product.Home(), "rccremote.pid")
	os.Remove(pidFile)

	return serverStopMsg{success: true}
}

type connectionCheckMsg struct {
	ok       bool
	err      error
	catalogs []string
}

type remoteCatalogsMsg struct {
	catalogs []string
}

func (v *RemoteView) checkConnection() tea.Msg {
	origin := common.RccRemoteOrigin()
	if origin == "" {
		return connectionCheckMsg{ok: false, err: fmt.Errorf("RCC_REMOTE_ORIGIN not set")}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	partsURL := strings.TrimSuffix(origin, "/") + "/parts/"
	resp, err := client.Get(partsURL)
	if err != nil {
		return connectionCheckMsg{ok: false, err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return connectionCheckMsg{ok: false, err: fmt.Errorf("HTTP %d", resp.StatusCode)}
	}

	return connectionCheckMsg{ok: true}
}

func (v *RemoteView) loadCatalogs() tea.Msg {
	return remoteCatalogsMsg{catalogs: htfs.CatalogNames()}
}

// Update implements View
func (v *RemoteView) Update(msg tea.Msg) (View, tea.Cmd) {
	// Handle input mode separately
	if v.inputMode {
		return v.handleInputMode(msg)
	}

	switch msg := msg.(type) {
	case serverStatusMsg:
		v.serverRunning = msg.running
		v.serverPID = msg.pid

	case serverDownloadMsg:
		v.loading = false
		v.downloadProgress = ""
		if msg.success {
			v.serverInstalled = true
			v.downloadProgress = "Download complete!"
		} else {
			v.downloadProgress = "Error: " + msg.err
		}

	case serverStartMsg:
		v.loading = false
		if msg.success {
			v.serverRunning = true
			v.serverPID = msg.pid
		} else {
			v.downloadProgress = "Start failed: " + msg.err
		}

	case serverStopMsg:
		v.loading = false
		if msg.success {
			v.serverRunning = false
			v.serverPID = 0
		} else {
			v.downloadProgress = "Stop failed: " + msg.err
		}

	case connectionCheckMsg:
		v.loading = false
		v.lastCheck = time.Now()
		v.connectionOK = msg.ok
		if msg.err != nil {
			v.checkError = msg.err.Error()
		} else {
			v.checkError = ""
		}
		v.remoteCatalogs = msg.catalogs
	case remoteCatalogsMsg:
		v.localCatalogs = msg.catalogs
		v.loading = false
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			v.tab = (v.tab + 1) % 4
			v.selected = 0
		case "shift+tab":
			v.tab--
			if v.tab < 0 {
				v.tab = 3
			}
			v.selected = 0
		case "j", "down":
			v.selected++
			max := v.getMaxItems()
			if v.selected >= max {
				v.selected = max - 1
			}
			if v.selected < 0 {
				v.selected = 0
			}
		case "k", "up":
			if v.selected > 0 {
				v.selected--
			}
		case "g":
			v.selected = 0
		case "G":
			v.selected = v.getMaxItems() - 1
			if v.selected < 0 {
				v.selected = 0
			}
		case "R":
			v.loading = true
			v.currentOrigin = common.RccRemoteOrigin()
			v.sharedEnabled = isSharedHolotreeEnabled()
			v.serverInstalled = false
			if _, err := os.Stat(v.serverBinaryPath); err == nil {
				v.serverInstalled = true
			}
			return v, tea.Batch(v.checkConnection, v.loadCatalogs, v.checkServerStatus)
		case "a":
			if v.tab == 1 { // Servers tab - add new
				v.inputMode = true
				v.inputField = "name"
				v.inputBuffer = ""
				v.editingProfile = &ServerProfile{}
			}
		case "d":
			if v.tab == 1 && v.selected < len(v.profiles.Profiles) {
				v.profiles.RemoveProfile(v.profiles.Profiles[v.selected].Name)
				SaveServerProfiles(v.profiles)
				if v.selected >= len(v.profiles.Profiles) {
					v.selected = len(v.profiles.Profiles) - 1
				}
				if v.selected < 0 {
					v.selected = 0
				}
			}
		case "enter":
			return v.handleEnter()
		case "p":
			if v.tab == 2 && v.currentOrigin != "" && v.connectionOK {
				// Pull catalogs
				action := ActionResult{
					Type:    ActionRunCommand,
					Command: "rcc holotree pull",
				}
				return v, func() tea.Msg { return actionMsg{action: action} }
			}
		case "s":
			if v.tab == 1 && v.selected < len(v.profiles.Profiles) {
				// Set as default
				profile := v.profiles.Profiles[v.selected]
				v.profiles.SetDefault(profile.Name)
				SaveServerProfiles(v.profiles)
			}
		case "t":
			if v.tab == 1 && v.selected < len(v.profiles.Profiles) {
				// Test connection to selected profile
				profile := v.profiles.Profiles[v.selected]
				v.loading = true
				return v, func() tea.Msg {
					client := &http.Client{Timeout: 5 * time.Second}
					partsURL := strings.TrimSuffix(profile.URL, "/") + "/parts/"
					resp, err := client.Get(partsURL)
					if err != nil {
						return connectionCheckMsg{ok: false, err: err}
					}
					defer resp.Body.Close()
					if resp.StatusCode != 200 {
						return connectionCheckMsg{ok: false, err: fmt.Errorf("HTTP %d", resp.StatusCode)}
					}
					return connectionCheckMsg{ok: true}
				}
			}
		}
	}
	return v, nil
}

func (v *RemoteView) handleInputMode(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			v.inputMode = false
			v.editingProfile = nil
			v.inputBuffer = ""
		case "enter":
			// Save current field and move to next or finish
			switch v.inputField {
			case "name":
				v.editingProfile.Name = v.inputBuffer
				v.inputField = "url"
				v.inputBuffer = "https://"
			case "url":
				v.editingProfile.URL = v.inputBuffer
				v.inputField = "auth"
				v.inputBuffer = ""
			case "auth":
				v.editingProfile.AuthToken = v.inputBuffer
				// Save the profile
				v.profiles.AddProfile(*v.editingProfile)
				SaveServerProfiles(v.profiles)
				v.inputMode = false
				v.editingProfile = nil
				v.inputBuffer = ""
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

func (v *RemoteView) handleEnter() (View, tea.Cmd) {
	switch v.tab {
	case 1: // Servers - set as default and test
		if v.selected < len(v.profiles.Profiles) {
			profile := v.profiles.Profiles[v.selected]
			v.profiles.SetDefault(profile.Name)
			SaveServerProfiles(v.profiles)
			// Test connection
			v.loading = true
			return v, func() tea.Msg {
				client := &http.Client{Timeout: 5 * time.Second}
				partsURL := strings.TrimSuffix(profile.URL, "/") + "/parts/"
				resp, err := client.Get(partsURL)
				if err != nil {
					return connectionCheckMsg{ok: false, err: err}
				}
				defer resp.Body.Close()
				if resp.StatusCode != 200 {
					return connectionCheckMsg{ok: false, err: fmt.Errorf("HTTP %d", resp.StatusCode)}
				}
				return connectionCheckMsg{ok: true}
			}
		}
	case 2: // Pull - trigger pull with origin from default profile
		origin := v.getActiveOrigin()
		if origin != "" {
			action := ActionResult{
				Type:    ActionRunCommand,
				Command: fmt.Sprintf("rcc holotree pull -o %s --force", origin),
			}
			return v, func() tea.Msg { return actionMsg{action: action} }
		}
	case 3: // Host - server management
		if v.selected == 0 {
			// Download/Install
			if !v.serverInstalled {
				v.loading = true
				v.downloadProgress = "Downloading rccremote..."
				return v, v.downloadRccremote
			} else {
				v.downloadProgress = "Already installed"
			}
		} else if v.selected == 1 {
			// Start/Stop server
			if v.serverRunning {
				v.loading = true
				return v, v.stopServer
			} else if !v.serverInstalled {
				v.downloadProgress = "Error: Download rccremote first"
			} else if !v.sharedEnabled {
				v.downloadProgress = "Error: Enable shared holotree first (sudo rcc holotree shared --enable)"
			} else {
				v.loading = true
				v.downloadProgress = "Starting server..."
				return v, v.startServer
			}
		}
	}
	return v, nil
}

// getActiveOrigin returns the URL to use for remote operations
func (v *RemoteView) getActiveOrigin() string {
	// First check env var
	if v.currentOrigin != "" {
		return v.currentOrigin
	}
	// Then check default profile
	if defaultProfile := v.profiles.GetDefault(); defaultProfile != nil {
		return defaultProfile.URL
	}
	return ""
}

func (v *RemoteView) getMaxItems() int {
	switch v.tab {
	case 0:
		return 2
	case 1:
		return len(v.profiles.Profiles)
	case 2:
		return len(v.localCatalogs) + 1
	case 3:
		return 2 // Download, Start/Stop
	}
	return 0
}

// View implements View
func (v *RemoteView) View() string {
	theme := v.styles.theme
	vs := NewViewStyles(theme)

	boxWidth := v.width - 8
	if boxWidth < 70 {
		boxWidth = 70
	}
	if boxWidth > 130 {
		boxWidth = 130
	}
	contentWidth := boxWidth - 6

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 2).
		Width(boxWidth)

	var b strings.Builder

	b.WriteString(RenderHeader(vs, "Remote", "Catalog Sharing", contentWidth))

	// Tab bar
	tabs := []string{"Connect", "Servers", "Pull", "Host"}
	for i, tab := range tabs {
		if i == v.tab {
			b.WriteString(vs.BadgeActive.Render(" " + tab + " "))
		} else {
			b.WriteString(vs.Badge.Render(" " + tab + " "))
		}
		b.WriteString(" ")
	}
	b.WriteString("\n")
	b.WriteString(vs.Separator.Render(strings.Repeat("-", contentWidth)))
	b.WriteString("\n\n")

	// Content
	switch v.tab {
	case 0:
		b.WriteString(v.renderConnectTab(vs, contentWidth))
	case 1:
		b.WriteString(v.renderServersTab(vs, contentWidth))
	case 2:
		b.WriteString(v.renderPullTab(vs, contentWidth))
	case 3:
		b.WriteString(v.renderHostTab(vs, contentWidth))
	}

	// Footer
	b.WriteString("\n\n")
	hints := v.getHints()
	b.WriteString(RenderFooter(vs, hints, contentWidth))

	return lipgloss.Place(v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		boxStyle.Render(b.String()),
	)
}

func (v *RemoteView) getHints() []KeyHint {
	hints := []KeyHint{
		{"Tab", "switch"},
		{"j/k", "nav"},
		{"R", "refresh"},
	}
	switch v.tab {
	case 1:
		hints = append(hints, KeyHint{"a", "add"})
		if len(v.profiles.Profiles) > 0 {
			hints = append(hints, KeyHint{"Enter", "activate"}, KeyHint{"t", "test"}, KeyHint{"d", "delete"})
		}
	case 2:
		if v.getActiveOrigin() != "" {
			hints = append(hints, KeyHint{"Enter", "pull"})
		}
	}
	return hints
}

func (v *RemoteView) renderConnectTab(vs ViewStyles, contentWidth int) string {
	var b strings.Builder

	b.WriteString(vs.Accent.Bold(true).Render("Connection Status"))
	b.WriteString("\n\n")

	// Status indicator
	b.WriteString(vs.Label.Render("Status"))
	if v.loading {
		b.WriteString(vs.Info.Render("[...] Checking"))
	} else if v.connectionOK {
		b.WriteString(vs.Success.Render("[OK] Connected"))
	} else {
		b.WriteString(vs.Error.Render("[X] Disconnected"))
	}
	b.WriteString("\n")

	// Active origin (env var or default profile)
	activeOrigin := v.getActiveOrigin()
	b.WriteString(vs.Label.Render("Active Server"))
	if activeOrigin == "" {
		b.WriteString(vs.Warning.Render("(none configured)"))
	} else {
		origin := activeOrigin
		if len(origin) > 45 {
			origin = origin[:42] + "..."
		}
		b.WriteString(vs.Info.Render(origin))
	}
	b.WriteString("\n")

	// Show source of active origin
	if v.currentOrigin != "" {
		b.WriteString(vs.Label.Render("Source"))
		b.WriteString(vs.Subtext.Render("RCC_REMOTE_ORIGIN env var"))
		b.WriteString("\n")
	} else if defaultProfile := v.profiles.GetDefault(); defaultProfile != nil {
		b.WriteString(vs.Label.Render("Source"))
		b.WriteString(vs.Subtext.Render("Saved profile: " + defaultProfile.Name))
		b.WriteString("\n")
	}

	// Last check
	if !v.lastCheck.IsZero() {
		b.WriteString(vs.Label.Render("Last Check"))
		b.WriteString(vs.Subtext.Render(v.lastCheck.Format("15:04:05")))
		b.WriteString("\n")
	}

	// Error
	if v.checkError != "" && !v.connectionOK {
		b.WriteString("\n")
		b.WriteString(vs.Error.Render("Error: " + v.checkError))
		b.WriteString("\n")
	}

	// Quick setup if not configured
	if activeOrigin == "" {
		b.WriteString("\n")
		b.WriteString(vs.Separator.Render(strings.Repeat("-", contentWidth)))
		b.WriteString("\n\n")
		b.WriteString(vs.Accent.Bold(true).Render("Getting Started"))
		b.WriteString("\n\n")
		b.WriteString(vs.Text.Render("1. Go to Servers tab and add a server"))
		b.WriteString("\n")
		b.WriteString(vs.Text.Render("2. Press Enter to set as default and test"))
		b.WriteString("\n")
		b.WriteString(vs.Text.Render("3. Go to Pull tab to pull catalogs"))
		b.WriteString("\n\n")
		b.WriteString(vs.Subtext.Render("Or set RCC_REMOTE_ORIGIN env var manually"))
	}

	return b.String()
}

func (v *RemoteView) renderServersTab(vs ViewStyles, contentWidth int) string {
	var b strings.Builder

	// Input mode overlay
	if v.inputMode {
		b.WriteString(vs.Accent.Bold(true).Render("Add Server"))
		b.WriteString("\n\n")

		fields := []struct {
			name   string
			label  string
			value  string
			active bool
		}{
			{"name", "Name", v.editingProfile.Name, v.inputField == "name"},
			{"url", "URL", v.editingProfile.URL, v.inputField == "url"},
			{"auth", "Auth Token", v.editingProfile.AuthToken, v.inputField == "auth"},
		}

		for _, f := range fields {
			b.WriteString(vs.Label.Render(f.label))
			if f.active {
				b.WriteString(vs.Selected.Render(v.inputBuffer + "_"))
			} else if f.value != "" {
				b.WriteString(vs.Text.Render(f.value))
			} else {
				b.WriteString(vs.Subtext.Render("(empty)"))
			}
			b.WriteString("\n")
		}

		b.WriteString("\n")
		b.WriteString(vs.Subtext.Render("Enter to continue, Esc to cancel"))
		return b.String()
	}

	b.WriteString(vs.Accent.Bold(true).Render("Saved Servers"))
	b.WriteString(vs.Subtext.Render(fmt.Sprintf(" (%d)", len(v.profiles.Profiles))))
	b.WriteString("\n\n")

	if len(v.profiles.Profiles) == 0 {
		b.WriteString(vs.Subtext.Render("No servers saved"))
		b.WriteString("\n\n")
		b.WriteString(vs.Subtext.Render("Press "))
		b.WriteString(vs.KeyHint.Render("a"))
		b.WriteString(vs.Subtext.Render(" to add a server"))
		return b.String()
	}

	for i, profile := range v.profiles.Profiles {
		isSelected := i == v.selected

		// Name with default indicator
		name := profile.Name
		if profile.IsDefault {
			name += " *"
		}

		if isSelected {
			b.WriteString(vs.Selected.Render("> " + name))
		} else {
			b.WriteString(vs.Normal.Render("  " + name))
		}
		b.WriteString("\n")

		// Show URL for selected
		if isSelected {
			b.WriteString("    ")
			url := profile.URL
			if len(url) > 45 {
				url = url[:42] + "..."
			}
			b.WriteString(vs.Info.Render(url))
			b.WriteString("\n")
			if profile.AuthToken != "" {
				b.WriteString("    ")
				b.WriteString(vs.Subtext.Render("Auth: "))
				b.WriteString(vs.Success.Render("configured"))
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

func (v *RemoteView) renderPullTab(vs ViewStyles, contentWidth int) string {
	var b strings.Builder

	b.WriteString(vs.Accent.Bold(true).Render("Pull Catalogs"))
	b.WriteString("\n\n")

	// Active server
	activeOrigin := v.getActiveOrigin()
	b.WriteString(vs.Label.Render("Server"))
	if activeOrigin == "" {
		b.WriteString(vs.Warning.Render("Not configured"))
		b.WriteString("\n\n")
		b.WriteString(vs.Subtext.Render("Add a server in Servers tab first"))
		return b.String()
	}

	origin := activeOrigin
	if len(origin) > 40 {
		origin = origin[:37] + "..."
	}
	b.WriteString(vs.Info.Render(origin))
	b.WriteString("\n\n")

	b.WriteString(vs.Separator.Render(strings.Repeat("-", contentWidth)))
	b.WriteString("\n\n")

	// Pull action
	if v.selected == 0 {
		b.WriteString(vs.Selected.Render("> Pull Catalogs from Server"))
	} else {
		b.WriteString(vs.Normal.Render("  Pull Catalogs from Server"))
	}
	b.WriteString("\n")
	b.WriteString("    ")
	b.WriteString(vs.Subtext.Render(fmt.Sprintf("rcc holotree pull -o %s", origin)))
	b.WriteString("\n\n")

	// Local catalogs
	b.WriteString(vs.Accent.Bold(true).Render("Local Catalogs"))
	b.WriteString(vs.Subtext.Render(fmt.Sprintf(" (%d)", len(v.localCatalogs))))
	b.WriteString("\n\n")

	if len(v.localCatalogs) == 0 {
		b.WriteString(vs.Subtext.Render("No local catalogs yet"))
		b.WriteString("\n")
		b.WriteString(vs.Subtext.Render("Pull from server or run a robot to create"))
	} else {
		maxShow := 8
		for i, cat := range v.localCatalogs {
			if i >= maxShow {
				b.WriteString(vs.Subtext.Render(fmt.Sprintf("  ... and %d more", len(v.localCatalogs)-maxShow)))
				break
			}
			displayName := cat
			if len(displayName) > 50 {
				displayName = displayName[:47] + "..."
			}
			b.WriteString(vs.Normal.Render("  " + displayName))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (v *RemoteView) renderHostTab(vs ViewStyles, contentWidth int) string {
	var b strings.Builder

	b.WriteString(vs.Accent.Bold(true).Render("Host Server"))
	b.WriteString("\n\n")

	// Status section
	b.WriteString(vs.Label.Render("Shared Holotree "))
	if v.sharedEnabled {
		b.WriteString(vs.Success.Render("[OK]"))
	} else {
		b.WriteString(vs.Error.Render("[Required]"))
		b.WriteString("\n")
		b.WriteString(vs.Warning.Render("  Run: sudo rcc holotree shared --enable"))
	}
	b.WriteString("\n")

	b.WriteString(vs.Label.Render("rccremote      "))
	if v.serverInstalled {
		b.WriteString(vs.Success.Render("[Installed]"))
	} else {
		b.WriteString(vs.Warning.Render("[Not installed]"))
	}
	b.WriteString("\n")

	b.WriteString(vs.Label.Render("Server         "))
	if v.serverRunning {
		b.WriteString(vs.Success.Render(fmt.Sprintf("[Running] PID %d", v.serverPID)))
	} else {
		b.WriteString(vs.Subtext.Render("[Stopped]"))
	}
	b.WriteString("\n\n")

	b.WriteString(vs.Separator.Render(strings.Repeat("-", contentWidth)))
	b.WriteString("\n\n")

	// Actions
	b.WriteString(vs.Accent.Bold(true).Render("Actions"))
	b.WriteString("\n\n")

	// Action 0: Download/Install
	action0Label := "Download rccremote"
	action0Desc := "Download from github.com/joshyorko/rcc/releases"
	if v.serverInstalled {
		action0Label = "rccremote installed"
		action0Desc = v.serverBinaryPath
	}
	if v.selected == 0 {
		b.WriteString(vs.Selected.Render("> " + action0Label))
	} else {
		b.WriteString(vs.Normal.Render("  " + action0Label))
	}
	b.WriteString("\n")
	b.WriteString(vs.Subtext.Render("    " + action0Desc))
	b.WriteString("\n\n")

	// Action 1: Start/Stop
	action1Label := "Start Server"
	action1Desc := fmt.Sprintf("Listen on %s:%d", v.serverHostname, v.serverPort)
	if v.serverRunning {
		action1Label = "Stop Server"
		action1Desc = fmt.Sprintf("Stop process %d", v.serverPID)
	} else if !v.serverInstalled {
		action1Desc = "(download rccremote first)"
	} else if !v.sharedEnabled {
		action1Desc = "(enable shared holotree first)"
	}
	if v.selected == 1 {
		b.WriteString(vs.Selected.Render("> " + action1Label))
	} else {
		b.WriteString(vs.Normal.Render("  " + action1Label))
	}
	b.WriteString("\n")
	b.WriteString(vs.Subtext.Render("    " + action1Desc))
	b.WriteString("\n")

	// Progress/status message
	if v.downloadProgress != "" {
		b.WriteString("\n")
		if strings.HasPrefix(v.downloadProgress, "Error") || strings.HasPrefix(v.downloadProgress, "Start failed") || strings.HasPrefix(v.downloadProgress, "Stop failed") {
			b.WriteString(vs.Error.Render(v.downloadProgress))
		} else {
			b.WriteString(vs.Info.Render(v.downloadProgress))
		}
		b.WriteString("\n")
	}

	// Client connection info
	if v.serverRunning {
		b.WriteString("\n")
		b.WriteString(vs.Separator.Render(strings.Repeat("-", contentWidth)))
		b.WriteString("\n\n")
		b.WriteString(vs.Accent.Bold(true).Render("Connect from clients"))
		b.WriteString("\n")
		b.WriteString(vs.Info.Render(fmt.Sprintf("  export RCC_REMOTE_ORIGIN=http://<this-ip>:%d", v.serverPort)))
		b.WriteString("\n")
		b.WriteString(vs.Info.Render("  rcc holotree pull"))
		b.WriteString("\n")
	}

	return b.String()
}

// Name implements View
func (v *RemoteView) Name() string {
	return "Remote"
}

// ShortHelp implements View
func (v *RemoteView) ShortHelp() string {
	return "tab:switch j/k:nav a:add p:pull R:refresh"
}
