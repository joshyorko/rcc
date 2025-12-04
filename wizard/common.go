package wizard

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pretty"
)

const (
	UNIX_NEWLINE    = "\n"
	WINDOWS_NEWLINE = "\r\n"
)

var (
	namePattern = regexp.MustCompile("^[\\w-]*$")
)

type Validator func(string) bool

type WizardFn func([]string) error

func memberValidation(members []string, erratic string) Validator {
	return func(input string) bool {
		for _, member := range members {
			if input == member {
				return true
			}
		}
		common.Stdout("%s%s%s\n\n", pretty.Red, erratic, pretty.Reset)
		return false
	}
}

func regexpValidation(validator *regexp.Regexp, erratic string) Validator {
	return func(input string) bool {
		if !validator.MatchString(input) {
			common.Stdout("%s%s%s\n\n", pretty.Red, erratic, pretty.Reset)
			return false
		}
		return true
	}
}

func warning(condition bool, message string) {
	if condition {
		common.Stdout("%s%s%s\n\n", pretty.Yellow, message, pretty.Reset)
	}
}

func firstOf(arguments []string, missing string) string {
	if len(arguments) > 0 {
		return arguments[0]
	}
	return missing
}

func note(form string, details ...interface{}) {
	message := fmt.Sprintf(form, details...)
	common.Stdout("%s! %s%s%s\n", pretty.Red, pretty.White, message, pretty.Reset)
}

func ask(question, defaults string, validator Validator) (string, error) {
	for {
		common.Stdout("%s? %s%s %s[%s]:%s ", pretty.Green, pretty.White, question, pretty.Grey, defaults, pretty.Reset)
		source := bufio.NewReader(os.Stdin)
		reply, err := source.ReadString(newline)
		common.Stdout("\n")
		if err != nil {
			return "", err
		}
		if reply == UNIX_NEWLINE || reply == WINDOWS_NEWLINE {
			reply = defaults
		}
		reply = strings.TrimSpace(reply)
		if !validator(reply) {
			continue
		}
		return reply, nil
	}
}

// ValidateProjectName returns a validator for project names that ensures the input
// matches the namePattern (alphanumeric characters, underscores, and hyphens only).
func ValidateProjectName() Validator {
	return regexpValidation(
		namePattern,
		"Invalid project name. Only alphanumeric characters, underscores, and hyphens are allowed.",
	)
}

// ValidateSelection returns a validator for list selection that accepts either
// the option value or a 1-based index number. On invalid input, it shows all
// available options with their corresponding numbers.
func ValidateSelection(options []string, displayNames []string) Validator {
	return func(input string) bool {
		// Check if input matches any option value
		for _, option := range options {
			if input == option {
				return true
			}
		}

		// Check if input is a valid 1-based index
		var index int
		if _, err := fmt.Sscanf(input, "%d", &index); err == nil {
			if index >= 1 && index <= len(options) {
				return true
			}
		}

		// Build error message with all options
		var optionsList strings.Builder
		for i, option := range options {
			if i > 0 {
				optionsList.WriteString(", ")
			}
			optionsList.WriteString(fmt.Sprintf("%d) %s", i+1, option))
		}

		common.Stdout("%sInvalid selection. Choose from: %s%s\n\n", pretty.Red, optionsList.String(), pretty.Reset)
		return false
	}
}

// ShowOptions displays a numbered list of options before prompting the user.
// Uses displayNames if provided, otherwise falls back to options.
func ShowOptions(options []string, displayNames []string) {
	for i, option := range options {
		display := option
		if displayNames != nil && i < len(displayNames) {
			display = displayNames[i]
		}
		common.Stdout("  %d) %s\n", i+1, display)
	}
}
