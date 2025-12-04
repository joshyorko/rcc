package pretty

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/joshyorko/rcc/common"
	"golang.org/x/term"
)

// ProgressIndicator defines the interface for progress visualization
type ProgressIndicator interface {
	Start()
	Stop(success bool)
	Update(current int64, message string)
	IsRunning() bool
}

// getTerminalWidth returns the terminal width or 80 as fallback
func getTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		common.Trace("Failed to get terminal width, using fallback: %v", err)
		return 80
	}
	common.Trace("Terminal width detected: %d", width)
	return width
}

// setupSignalHandler sets up Ctrl+C handler to restore cursor visibility
func setupSignalHandler(cleanup func()) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		common.Trace("Interrupt signal received, cleaning up progress indicator")
		cleanup()
		os.Exit(1)
	}()
}

// Spinner implements ProgressIndicator with animated spinner
type Spinner struct {
	message  string
	frames   []string
	running  bool
	stopChan chan bool
	mu       sync.Mutex
}

// NewSpinner creates a new spinner with the given message
func NewSpinner(message string) ProgressIndicator {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

	// Use ASCII fallback if not interactive or icons disabled
	if !Interactive || !Iconic {
		frames = []string{"|", "/", "-", "\\"}
	}

	spinner := &Spinner{
		message:  message,
		frames:   frames,
		running:  false,
		stopChan: make(chan bool, 1),
	}

	common.Trace("Spinner created with message: %s", message)
	return spinner
}

// DelayedSpinner wraps a spinner with a delay before showing
type DelayedSpinner struct {
	spinner     *Spinner
	delay       time.Duration
	started     bool
	showChan    chan struct{}
	cancelChan  chan struct{}
	mu          sync.Mutex
}

// NewDelayedSpinner creates a spinner that only appears after 500ms delay
// This satisfies FR-001: Progress indicators appear within 500ms of operation start
// but only if the operation takes longer than 500ms
func NewDelayedSpinner(message string) *DelayedSpinner {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	if !Interactive || !Iconic {
		frames = []string{"|", "/", "-", "\\"}
	}

	spinner := &Spinner{
		message:  message,
		frames:   frames,
		running:  false,
		stopChan: make(chan bool, 1),
	}

	return &DelayedSpinner{
		spinner:    spinner,
		delay:      500 * time.Millisecond,
		started:    false,
		showChan:   make(chan struct{}),
		cancelChan: make(chan struct{}),
	}
}

// Start begins the delayed spinner - spinner appears after delay
func (d *DelayedSpinner) Start() {
	d.mu.Lock()
	if d.started {
		d.mu.Unlock()
		return
	}
	d.started = true
	d.mu.Unlock()

	if !Interactive {
		common.Trace("Delayed spinner skipped (non-interactive mode): %s", d.spinner.message)
		return
	}

	go func() {
		select {
		case <-time.After(d.delay):
			d.spinner.Start()
			close(d.showChan)
		case <-d.cancelChan:
			// Operation completed before delay - no spinner needed
			common.Trace("Delayed spinner cancelled (operation completed quickly): %s", d.spinner.message)
		}
	}()
}

// Stop stops the delayed spinner
func (d *DelayedSpinner) Stop(success bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.started {
		return
	}

	// Cancel the delay timer if still waiting
	select {
	case <-d.cancelChan:
		// Already cancelled
	default:
		close(d.cancelChan)
	}

	// If spinner was actually shown, stop it properly
	if d.spinner.IsRunning() {
		d.spinner.Stop(success)
	}
}

// Update changes the spinner message
func (d *DelayedSpinner) Update(current int64, message string) {
	d.spinner.Update(current, message)
}

// IsRunning returns whether the spinner is currently running
func (d *DelayedSpinner) IsRunning() bool {
	return d.spinner.IsRunning()
}

// Start begins the spinner animation
func (s *Spinner) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	// Skip animation if not interactive
	if !Interactive {
		common.Trace("Spinner skipped (non-interactive mode): %s", s.message)
		common.Stdout("%s\n", s.message)
		return
	}

	common.Trace("Starting spinner: %s", s.message)

	// Setup signal handler for cleanup
	setupSignalHandler(func() {
		s.cleanup()
	})

	// Hide cursor
	common.Stdout("%s", csif("?25l"))

	// Start animation loop
	go s.animate()
}

// animate runs the spinner animation loop
func (s *Spinner) animate() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	frameIndex := 0
	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.mu.Lock()
			frame := s.frames[frameIndex]
			message := s.message
			s.mu.Unlock()

			// Clear line and print spinner frame
			common.Stdout("\r%s%s %s", csif("0K"), frame, message)

			frameIndex = (frameIndex + 1) % len(s.frames)
		}
	}
}

// Stop stops the spinner and shows final status
func (s *Spinner) Stop(success bool) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	common.Trace("Stopping spinner with success=%v: %s", success, s.message)

	if !Interactive {
		return
	}

	// Stop animation
	s.stopChan <- true

	// Cleanup and show final status
	s.cleanup()

	status := "✓"
	color := Green
	if !success {
		status = "✗"
		color = Red
	}

	// Use ASCII fallback if not iconic
	if !Iconic {
		if success {
			status = "[OK]"
		} else {
			status = "[FAIL]"
		}
	}

	common.Stdout("\r%s%s%s %s%s\n", csif("0K"), color, status, s.message, Reset)
}

// Update changes the spinner message
func (s *Spinner) Update(current int64, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.message = message
	common.Trace("Spinner message updated: %s", message)
}

// IsRunning returns whether the spinner is currently running
func (s *Spinner) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// cleanup restores terminal state
func (s *Spinner) cleanup() {
	common.Stdout("\r%s", csif("0K"))
	common.Stdout("%s", csif("?25h"))
}

// ProgressBar implements ProgressIndicator with a progress bar
type ProgressBar struct {
	message  string
	total    int64
	current  int64
	running  bool
	started  time.Time
	mu       sync.Mutex
}

// NewProgressBar creates a new progress bar with the given message and total
func NewProgressBar(message string, total int64) ProgressIndicator {
	bar := &ProgressBar{
		message: message,
		total:   total,
		current: 0,
		running: false,
	}

	common.Trace("ProgressBar created with message: %s, total: %d", message, total)
	return bar
}

// Start begins the progress bar
func (p *ProgressBar) Start() {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.started = time.Now()
	p.mu.Unlock()

	// Skip if not interactive
	if !Interactive {
		common.Trace("ProgressBar skipped (non-interactive mode): %s", p.message)
		common.Stdout("%s\n", p.message)
		return
	}

	common.Trace("Starting progress bar: %s", p.message)

	// Setup signal handler for cleanup
	setupSignalHandler(func() {
		p.cleanup()
	})

	// Hide cursor
	common.Stdout("%s", csif("?25l"))

	// Draw initial progress
	p.draw()
}

// Stop stops the progress bar and shows final status
func (p *ProgressBar) Stop(success bool) {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	p.running = false
	p.mu.Unlock()

	common.Trace("Stopping progress bar with success=%v: %s", success, p.message)

	if !Interactive {
		return
	}

	// Cleanup and show final status
	p.cleanup()

	status := "✓"
	color := Green
	if !success {
		status = "✗"
		color = Red
	}

	// Use ASCII fallback if not iconic
	if !Iconic {
		if success {
			status = "[OK]"
		} else {
			status = "[FAIL]"
		}
	}

	common.Stdout("\r%s%s%s %s%s\n", csif("0K"), color, status, p.message, Reset)
}

// Update updates the progress bar with current progress
func (p *ProgressBar) Update(current int64, message string) {
	p.mu.Lock()
	p.current = current
	if message != "" {
		p.message = message
	}
	p.mu.Unlock()

	common.Trace("ProgressBar updated: current=%d, message=%s", current, message)

	if Interactive && p.IsRunning() {
		p.draw()
	}
}

// IsRunning returns whether the progress bar is currently running
func (p *ProgressBar) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// draw renders the progress bar
func (p *ProgressBar) draw() {
	p.mu.Lock()
	current := p.current
	total := p.total
	message := p.message
	elapsed := time.Since(p.started)
	p.mu.Unlock()

	// Calculate percentage
	percentage := 0
	if total > 0 {
		percentage = int((current * 100) / total)
		if percentage > 100 {
			percentage = 100
		}
	}

	// Calculate remaining time
	remaining := ""
	if current > 0 && total > 0 {
		rate := float64(current) / elapsed.Seconds()
		if rate > 0 {
			remainingSeconds := float64(total-current) / rate
			remainingDuration := time.Duration(remainingSeconds) * time.Second

			minutes := int(remainingDuration.Minutes())
			seconds := int(remainingDuration.Seconds()) % 60

			if minutes > 0 {
				remaining = fmt.Sprintf(" %dm%ds remaining", minutes, seconds)
			} else {
				remaining = fmt.Sprintf(" %ds remaining", seconds)
			}
		}
	}

	// Calculate bar width
	termWidth := getTerminalWidth()
	// Format: "[=====>    ] 45% 2m30s remaining message"
	// Reserve space for brackets, percentage, remaining time, and message
	reservedSpace := len(message) + len(remaining) + 20 // brackets + percentage + spaces
	barWidth := termWidth - reservedSpace
	if barWidth < 10 {
		barWidth = 10
	}
	if barWidth > 50 {
		barWidth = 50
	}

	// Build progress bar
	filled := (percentage * barWidth) / 100
	if filled > barWidth {
		filled = barWidth
	}

	bar := ""
	for i := 0; i < barWidth; i++ {
		if i < filled-1 {
			bar += "="
		} else if i == filled-1 {
			bar += ">"
		} else {
			bar += " "
		}
	}

	// Draw progress bar
	common.Stdout("\r%s[%s] %3d%%%s %s", csif("0K"), bar, percentage, remaining, message)
}

// cleanup restores terminal state
func (p *ProgressBar) cleanup() {
	common.Stdout("\r%s", csif("0K"))
	common.Stdout("%s", csif("?25h"))
}
