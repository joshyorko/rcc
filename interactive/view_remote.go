package interactive

import (
	"fmt"
	"net/http"
	"strings"
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
	tab             int // 0 = Connect, 1 = Servers, 2 = Pull, 3 = Host Guide
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
}

// NewRemoteView creates a new remote view
func NewRemoteView(styles *Styles) *RemoteView {
	profiles, _ := LoadServerProfiles()
	return &RemoteView{
		styles:        styles,
		width:         120,
		height:        30,
		tab:           0,
		selected:      0,
		loading:       true,
		profiles:      profiles,
		currentOrigin: common.RccRemoteOrigin(),
		sharedEnabled: common.SharedHolotree,
	}
}

// Init implements View
func (v *RemoteView) Init() tea.Cmd {
	return tea.Batch(v.checkConnection, v.loadCatalogs)
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
			return v, tea.Batch(v.checkConnection, v.loadCatalogs)
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
		return 4
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

	b.WriteString(vs.Accent.Bold(true).Render("Host Your Own Server"))
	b.WriteString("\n\n")

	// Prerequisites
	b.WriteString(vs.Label.Render("Shared Holotree"))
	if v.sharedEnabled {
		b.WriteString(vs.Success.Render("[OK] Enabled"))
	} else {
		b.WriteString(vs.Error.Render("[X] Required"))
	}
	b.WriteString("\n\n")

	if !v.sharedEnabled {
		b.WriteString(vs.Warning.Render("Enable shared holotree first:"))
		b.WriteString("\n")
		b.WriteString(vs.Info.Render("sudo rcc holotree shared --enable"))
		b.WriteString("\n\n")
	}

	b.WriteString(vs.Separator.Render(strings.Repeat("-", contentWidth)))
	b.WriteString("\n\n")

	b.WriteString(vs.Accent.Bold(true).Render("Deployment Options"))
	b.WriteString("\n\n")

	options := []struct {
		name string
		desc string
		cmd  string
	}{
		{
			"Local Development",
			"Run rccremote directly (HTTP only)",
			"rccremote -hostname 0.0.0.0 -port 4653",
		},
		{
			"Docker + NGINX",
			"Production setup with TLS",
			"github.com/yorko-io/rccremote-docker",
		},
		{
			"Cloudflare Tunnel",
			"Expose via CF tunnel (no port forwarding)",
			"make quick-cf HOSTNAME=rcc.example.com",
		},
		{
			"Kubernetes",
			"K8s deployment with ingress",
			"make quick-k8s",
		},
	}

	for i, opt := range options {
		if i == v.selected {
			b.WriteString(vs.Selected.Render("> " + opt.name))
		} else {
			b.WriteString(vs.Normal.Render("  " + opt.name))
		}
		b.WriteString("\n")
		if i == v.selected {
			b.WriteString("    ")
			b.WriteString(vs.Subtext.Render(opt.desc))
			b.WriteString("\n    ")
			b.WriteString(vs.Info.Render(opt.cmd))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(vs.Subtext.Render("Note: Production deployments require TLS certificates"))

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
