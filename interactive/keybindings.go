package interactive

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds all the key bindings for the interactive TUI
type KeyMap struct {
	// Global
	Quit key.Binding
	Help key.Binding

	// View switching
	ViewHome        key.Binding
	ViewCommands    key.Binding
	ViewRobots      key.Binding
	ViewEnvs        key.Binding
	ViewHistory     key.Binding
	ViewDiagnostics key.Binding
	ViewLogs        key.Binding
	ViewRemote      key.Binding

	// Navigation (vim-style)
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	Top      key.Binding
	Bottom   key.Binding
	PageUp   key.Binding
	PageDown key.Binding

	// Selection
	Select key.Binding
	Back   key.Binding

	// Actions
	Run     key.Binding
	Edit    key.Binding
	Delete  key.Binding
	New     key.Binding
	Search  key.Binding
	Refresh key.Binding
	Tab     key.Binding
}

// DefaultKeyMap returns the default key bindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		// Global
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),

		// View switching
		ViewHome: key.NewBinding(
			key.WithKeys("1"),
			key.WithHelp("1", "home"),
		),
		ViewCommands: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "commands"),
		),
		ViewRobots: key.NewBinding(
			key.WithKeys("3"),
			key.WithHelp("3", "robots"),
		),
		ViewEnvs: key.NewBinding(
			key.WithKeys("4"),
			key.WithHelp("4", "environments"),
		),
		ViewHistory: key.NewBinding(
			key.WithKeys("5"),
			key.WithHelp("5", "history"),
		),
		ViewDiagnostics: key.NewBinding(
			key.WithKeys("6"),
			key.WithHelp("6", "diagnostics"),
		),
		ViewLogs: key.NewBinding(
			key.WithKeys("7"),
			key.WithHelp("7", "logs"),
		),
		ViewRemote: key.NewBinding(
			key.WithKeys("8"),
			key.WithHelp("8", "remote"),
		),

		// Navigation (vim-style + arrows)
		Up: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("k/↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("j/↓", "down"),
		),
		Left: key.NewBinding(
			key.WithKeys("h", "left"),
			key.WithHelp("h/←", "collapse"),
		),
		Right: key.NewBinding(
			key.WithKeys("l", "right"),
			key.WithHelp("l/→", "expand"),
		),
		Top: key.NewBinding(
			key.WithKeys("g"),
			key.WithHelp("g", "top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G"),
			key.WithHelp("G", "bottom"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("ctrl+u", "pgup"),
			key.WithHelp("ctrl+u", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("ctrl+d", "pgdown"),
			key.WithHelp("ctrl+d", "page down"),
		),

		// Selection
		Select: key.NewBinding(
			key.WithKeys("enter", "l"),
			key.WithHelp("enter", "select"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc", "h"),
			key.WithHelp("esc", "back"),
		),

		// Actions
		Run: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "run"),
		),
		Edit: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "edit"),
		),
		Delete: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete"),
		),
		New: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "new"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("R"),
			key.WithHelp("R", "refresh"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch"),
		),
	}
}

// keys is the global key map instance
var keys = DefaultKeyMap()
