package artifactprovider

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

func NormalizeHTTPURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid artifact provider URL %q", raw)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("artifact provider URL must not contain a path")
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		ipv4Loopback := ip != nil && ip.To4() != nil && ip.To4()[0] == 127
		if host != "localhost" && !ipv4Loopback && host != "::1" {
			return "", fmt.Errorf("HTTP artifact provider must use localhost or loopback")
		}
	default:
		return "", fmt.Errorf("artifact provider URL must use HTTP or HTTPS")
	}
	return strings.TrimRight(raw, "/"), nil
}
