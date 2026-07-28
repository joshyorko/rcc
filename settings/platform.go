package settings

import (
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/joshyorko/rcc/shell"
)

const (
	liningPattern  = `\r?\n`
	spacingPattern = `\s+`
)

var (
	spacingForm = regexp.MustCompile(spacingPattern)
	liningForm  = regexp.MustCompile(liningPattern)
)

func osReleaseValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return value
	}
	if value[0] == '"' && value[len(value)-1] == '"' {
		unquoted, err := strconv.Unquote(value)
		if err == nil {
			return unquoted
		}
	}
	if value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1]
	}
	return value
}

func formatOSRelease(content string) string {
	values := make(map[string]string)
	for _, line := range liningForm.Split(content, -1) {
		line = strings.TrimSpace(line)
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = osReleaseValue(value)
		}
	}

	description := values["PRETTY_NAME"]
	if len(description) == 0 {
		description = strings.TrimSpace(strings.Join([]string{values["NAME"], values["VERSION"]}, " "))
	}
	if len(description) == 0 {
		return ""
	}

	details := make([]string, 0, 6)
	for _, entry := range []struct {
		label string
		key   string
	}{
		{"id", "ID"},
		{"id-like", "ID_LIKE"},
		{"version", "VERSION_ID"},
		{"variant", "VARIANT_ID"},
	} {
		if value := values[entry.key]; len(value) > 0 {
			details = append(details, fmt.Sprintf("%s=%s", entry.label, value))
		}
	}
	if image := values["IMAGE_ID"]; len(image) > 0 {
		if version := values["IMAGE_VERSION"]; len(version) > 0 {
			image += ":" + version
		}
		details = append(details, "image="+image)
	}
	if value := values["OSTREE_VERSION"]; len(value) > 0 {
		details = append(details, "ostree="+value)
	}
	if len(details) == 0 {
		return description
	}
	return fmt.Sprintf("%s [%s]", description, strings.Join(details, "; "))
}

func operatingSystemFromFiles(filenames []string) (string, bool) {
	for _, filename := range filenames {
		content, err := os.ReadFile(filename)
		if err != nil {
			continue
		}
		details := formatOSRelease(string(content))
		if len(details) > 0 {
			return details, true
		}
	}
	return "", false
}

func operatingSystem() string {
	if runtime.GOOS == "linux" {
		if details, ok := operatingSystemFromFiles([]string{"/etc/os-release", "/usr/lib/os-release"}); ok {
			return details
		}
	}

	output, _, err := shell.New(nil, ".", osInfoCommand...).NoStderr().CaptureOutput()
	if err == nil && len(strings.TrimSpace(output)) > 0 {
		return output
	}
	if runtime.GOOS == "linux" {
		fallback, _, fallbackErr := shell.New(nil, ".", "uname", "-a").NoStderr().CaptureOutput()
		if fallbackErr == nil && len(strings.TrimSpace(fallback)) > 0 {
			return fallback
		}
		if fallbackErr != nil {
			return fallbackErr.Error()
		}
	}
	if err != nil {
		return err.Error()
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
