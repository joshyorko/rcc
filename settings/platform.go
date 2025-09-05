package settings

import (
	"regexp"
	"strings"

	"github.com/robocorp/rcc/shell"
)

const (
	liningPattern  = `\r?\n`
	spacingPattern = `\s+`
)

var (
	spacingForm = regexp.MustCompile(spacingPattern)
	liningForm  = regexp.MustCompile(liningPattern)
)

func operatingSystem() string {
	output, _, err := shell.New(nil, ".", osInfoCommand...).NoStderr().CaptureOutput()
	if err != nil {
		output = err.Error()
	}
	return output
}

func pickLines(text string) []string {
	result := []string{}
	for _, part := range liningForm.Split(text, -1) {
		flat := strings.TrimSpace(strings.Join(spacingForm.Split(part, -1), " "))
		if len(flat) > 0 {
			result = append(result, flat)
		}
	}
	return result
}

func OperatingSystem() string {
	return strings.Join(pickLines(operatingSystem()), "; ")
}
