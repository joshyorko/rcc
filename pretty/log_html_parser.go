package pretty

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ParseLogHTML reads and parses a Robot Framework log.html or output.xml file
// Returns lines suitable for terminal display
func ParseLogHTML(artifactsDir string) ([]string, error) {
	// Robot Framework log.html is JavaScript-heavy, so we parse output.xml instead
	// which contains the actual structured test results
	return parseOutputXML(artifactsDir)
}

// parseOutputXML parses the simpler output.xml file
func parseOutputXML(artifactsDir string) ([]string, error) {
	xmlPath := filepath.Join(artifactsDir, "output.xml")

	file, err := os.Open(xmlPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)

	// Regex patterns for extracting useful info from output.xml
	msgPattern := regexp.MustCompile(`<msg[^>]*>([^<]+)</msg>`)
	statusPattern := regexp.MustCompile(`<status[^>]*status="([^"]+)"`)
	kwPattern := regexp.MustCompile(`<kw[^>]*name="([^"]+)"`)
	testPattern := regexp.MustCompile(`<test[^>]*name="([^"]+)"`)

	for scanner.Scan() {
		line := scanner.Text()

		// Extract test names
		if matches := testPattern.FindStringSubmatch(line); len(matches) > 1 {
			lines = append(lines, "")
			lines = append(lines, "=== TEST: "+matches[1]+" ===")
		}

		// Extract keyword names (indented)
		if matches := kwPattern.FindStringSubmatch(line); len(matches) > 1 {
			// Skip internal keywords
			name := matches[1]
			if !strings.HasPrefix(name, "BuiltIn.") {
				lines = append(lines, "  > "+name)
			}
		}

		// Extract messages
		if matches := msgPattern.FindStringSubmatch(line); len(matches) > 1 {
			msg := unescapeXML(matches[1])
			msg = strings.TrimSpace(msg)
			if msg != "" && len(msg) < 200 {
				lines = append(lines, "    "+msg)
			}
		}

		// Extract status info
		if strings.Contains(line, "</test>") || strings.Contains(line, "</suite>") {
			if matches := statusPattern.FindStringSubmatch(line); len(matches) > 1 {
				status := matches[1]
				if status == "PASS" {
					lines = append(lines, "  [PASS]")
				} else if status == "FAIL" {
					lines = append(lines, "  [FAIL]")
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

// unescapeXML handles common XML/HTML escape sequences
func unescapeXML(s string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&apos;", "'",
		"&#10;", "\n",
		"&#13;", "\r",
		"&#9;", "\t",
	)
	return replacer.Replace(s)
}
