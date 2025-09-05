package wizard

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pretty"
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
