package wizard

import (
	"bufio"
	"errors"
	"os"
	"strings"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pretty"
	"github.com/spf13/cobra"
)

var (
	// ErrConfirmationRequired is returned when confirmation is needed but not available
	ErrConfirmationRequired = errors.New("confirmation required: use --yes flag in non-interactive mode")
)

// Confirm displays a yes/no confirmation prompt and returns the user's choice.
// If force is true, automatically returns true without prompting.
// In non-interactive mode without force, returns ErrConfirmationRequired.
// Accepts y/Y for yes, n/N for no. Defaults to no if user presses Enter.
func Confirm(question string, force bool) (bool, error) {
	// If force flag is set, skip prompt and return true
	if force {
		return true, nil
	}

	// In non-interactive mode without force, require --yes flag
	if !pretty.Interactive && !force {
		return false, ErrConfirmationRequired
	}

	// Create validator for y/Y/n/N responses
	validator := memberValidation([]string{"y", "Y", "n", "N"}, "Please answer 'y' or 'n'.")

	// Display prompt and get response
	response, err := ask(question, "n", validator)
	if err != nil {
		return false, err
	}

	// Check if user confirmed
	confirmed := response == "y" || response == "Y"

	// Print cancellation message if user declined
	if !confirmed {
		common.Stdout("%sOperation cancelled.%s\n", pretty.Grey, pretty.Reset)
	}

	return confirmed, nil
}

// ConfirmDangerous displays a confirmation prompt that requires typing "yes" for dangerous operations.
// If force is true, automatically returns true without prompting.
// In non-interactive mode without force, returns ErrConfirmationRequired.
// Requires exact typing of "yes" (case-insensitive) to confirm.
func ConfirmDangerous(question string, force bool) (bool, error) {
	// If force flag is set, skip prompt and return true
	if force {
		return true, nil
	}

	// In non-interactive mode without force, require --yes flag
	if !pretty.Interactive && !force {
		return false, ErrConfirmationRequired
	}

	// Create validator for "yes" response (case-insensitive)
	validator := func(input string) bool {
		lower := strings.ToLower(input)
		if lower != "yes" {
			if lower != "" {
				common.Stdout("%sPlease type 'yes' to confirm or press Enter to cancel.%s\n\n", pretty.Red, pretty.Reset)
			}
			return false
		}
		return true
	}

	// Display prompt with special format for dangerous operations
	for {
		common.Stdout("%s? %s%s %s[type 'yes' to confirm]:%s ", pretty.Green, pretty.White, question, pretty.Grey, pretty.Reset)
		source := bufio.NewReader(os.Stdin)
		reply, err := source.ReadString(newline)
		common.Stdout("\n")
		if err != nil {
			return false, err
		}

		// Handle empty input (just pressing Enter)
		if reply == UNIX_NEWLINE || reply == WINDOWS_NEWLINE {
			common.Stdout("%sOperation cancelled.%s\n", pretty.Grey, pretty.Reset)
			return false, nil
		}

		reply = strings.TrimSpace(reply)

		// If empty after trimming, treat as cancellation
		if reply == "" {
			common.Stdout("%sOperation cancelled.%s\n", pretty.Grey, pretty.Reset)
			return false, nil
		}

		// Validate the response
		if !validator(reply) {
			continue
		}

		return true, nil
	}
}

// AddYesFlag adds a --yes/-y flag to the given command that can be used to skip confirmation prompts.
func AddYesFlag(cmd *cobra.Command, target *bool) {
	cmd.Flags().BoolVarP(target, "yes", "y", false, "Skip confirmation prompt")
}
