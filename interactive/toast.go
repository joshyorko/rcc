package interactive

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ToastType defines the type of toast notification
type ToastType int

const (
	ToastInfo ToastType = iota
	ToastSuccess
	ToastWarning
	ToastError
)

// Toast represents a notification structure
type Toast struct {
	ID        int64
	Type      ToastType
	Message   string
	StartTime time.Time
	Duration  time.Duration
}

// ToastMsg is sent to trigger a new toast
type ToastMsg struct {
	Type     ToastType
	Message  string
	Duration time.Duration
}

// ToastTimeoutMsg is sent when a toast expires
type ToastTimeoutMsg struct {
	ID int64
}

// ShowToast creates a command to show a toast
func ShowToast(msg string, t ToastType) tea.Cmd {
	return func() tea.Msg {
		return ToastMsg{
			Type:     t,
			Message:  msg,
			Duration: 3 * time.Second,
		}
	}
}

// ShowErrorToast is a helper for error messages
func ShowErrorToast(msg string) tea.Cmd {
	return ShowToast(msg, ToastError)
}

// ShowSuccessToast is a helper for success messages
func ShowSuccessToast(msg string) tea.Cmd {
	return ShowToast(msg, ToastSuccess)
}

// ShowInfoToast is a helper for info messages
func ShowInfoToast(msg string) tea.Cmd {
	return ShowToast(msg, ToastInfo)
}

// ShowWarningToast is a helper for warning messages
func ShowWarningToast(msg string) tea.Cmd {
	return ShowToast(msg, ToastWarning)
}
