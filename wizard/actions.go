package wizard

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pretty"
)

var (
	// ErrNotInteractive is returned when an action requires interactive mode but it's not available
	ErrNotInteractive = errors.New("action selection requires interactive mode")
)

// Action represents a selectable option in the wizard UI
type Action struct {
	Key         string       // Unique identifier
	Name        string       // Display name
	Description string       // Help text
	Handler     func() error // Optional callback
}

// ChooseAction presents numbered options and returns the selected action.
// It displays a numbered list with names and descriptions, validates user input,
// and returns a pointer to the selected Action.
// Returns error if not in interactive mode or if input reading fails.
func ChooseAction(prompt string, actions []Action) (*Action, error) {
	// Check if we're in interactive mode
	if !pretty.Interactive {
		return nil, ErrNotInteractive
	}

	// Validate we have actions
	if len(actions) == 0 {
		return nil, errors.New("no actions provided")
	}

	// Display prompt
	common.Stdout("%s%s%s\n\n", pretty.White, prompt, pretty.Reset)

	// Display numbered list of actions
	for i, action := range actions {
		common.Stdout("  %s%d)%s %s%s%s\n", pretty.Green, i+1, pretty.Reset, pretty.White, action.Name, pretty.Reset)
		if action.Description != "" {
			common.Stdout("     %s%s%s\n", pretty.Grey, action.Description, pretty.Reset)
		}
		common.Stdout("\n")
	}

	// Create validator for numeric input with bounds checking
	validator := func(input string) bool {
		// Reject excessively long input (prevents parsing issues)
		if len(input) > 10 {
			common.Stdout("%sInput too long. Please enter a number between 1 and %d.%s\n\n", pretty.Red, len(actions), pretty.Reset)
			return false
		}
		num, err := strconv.Atoi(input)
		if err != nil {
			common.Stdout("%sPlease enter a number between 1 and %d.%s\n\n", pretty.Red, len(actions), pretty.Reset)
			return false
		}
		if num < 1 || num > len(actions) {
			common.Stdout("%sPlease enter a number between 1 and %d.%s\n\n", pretty.Red, len(actions), pretty.Reset)
			return false
		}
		return true
	}

	// Ask for selection
	prompt = fmt.Sprintf("Enter choice [1-%d]", len(actions))
	reply, err := ask(prompt, "1", validator)
	if err != nil {
		return nil, err
	}

	// Parse the selection (we know it's valid from validator)
	index, _ := strconv.Atoi(reply)
	selected := &actions[index-1]

	return selected, nil
}

// AskRecovery presents recovery options for an error.
// It displays the error in red, then shows recovery options using the ChooseAction pattern.
// Returns the selected recovery action.
func AskRecovery(err error, options []Action) (*Action, error) {
	// Check if we're in interactive mode
	if !pretty.Interactive {
		return nil, ErrNotInteractive
	}

	// Display error
	common.Stdout("%sError: %s%s\n\n", pretty.Red, err.Error(), pretty.Reset)

	// Display recovery prompt and options
	prompt := "How would you like to proceed?"
	return ChooseAction(prompt, options)
}

// ConfirmDangerousWithText requires typing specific confirmation text for critical operations.
// It's more strict than Confirm() - requires typing exact text rather than just y/n.
// If force is true, returns true without prompting.
// If not interactive and force is false, returns error.
// Uses case-insensitive comparison for the confirmation text.
func ConfirmDangerousWithText(prompt string, confirmText string, force bool) (bool, error) {
	// If force flag is set, skip prompt and return true
	if force {
		return true, nil
	}

	// In non-interactive mode without force, return error
	if !pretty.Interactive && !force {
		return false, ErrConfirmationRequired
	}

	// Display warning prompt
	common.Stdout("%s\u26A0 WARNING: %s%s\n\n", pretty.Yellow, prompt, pretty.Reset)

	// Create validator for exact text match (case-insensitive)
	lowerConfirm := strings.ToLower(confirmText)
	validator := func(input string) bool {
		lowerInput := strings.ToLower(input)
		if lowerInput != lowerConfirm {
			if lowerInput != "" {
				common.Stdout("%sInput does not match. Please type \"%s\" to confirm or press Enter to cancel.%s\n\n", pretty.Red, confirmText, pretty.Reset)
			}
			return false
		}
		return true
	}

	// Display prompt and get response
	question := fmt.Sprintf("To confirm, type \"%s\"", confirmText)
	response, err := ask(question, "", validator)
	if err != nil {
		return false, err
	}

	// Empty response means cancellation
	if response == "" {
		common.Stdout("%sOperation cancelled.%s\n", pretty.Grey, pretty.Reset)
		return false, nil
	}

	return true, nil
}
