package operations

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"

	"github.com/robocorp/rcc/cloud"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
	"github.com/robocorp/rcc/set"
	"github.com/robocorp/rcc/settings"
	"gopkg.in/yaml.v2"
)

type (
	WebConfig struct {
		URL         string `yaml:"url"`
		Codes       []int  `yaml:"codes"`
		Fingerprint string `yaml:"content-sha256,omitempty"`
	}

	NetConfig struct {
		DNS  []string     `yaml:"dns-lookup"`
		TLS  []string     `yaml:"tls-verify"`
		Head []*WebConfig `yaml:"head-request"`
		Get  []*WebConfig `yaml:"get-request"`
	}

	Configuration struct {
		Network *NetConfig `yaml:"network"`
	}

	webtool func(string) (int, string, error)
)

func (it *NetConfig) Hostnames() []string {
	result := make([]string, 0, len(it.DNS)+len(it.TLS))
	result = append(result, it.DNS...)
	result = append(result, it.TLS...)
	for _, entry := range it.Head {
		parsed, err := url.Parse(entry.URL)
		if err == nil {
			result = append(result, parsed.Hostname())
		}
	}
	for _, entry := range it.Get {
		parsed, err := url.Parse(entry.URL)
		if err == nil {
			result = append(result, parsed.Hostname())
		}
	}
	return set.Set(result)
}

func parseNetworkDiagnosticConfig(body []byte) (*Configuration, error) {
	config := &Configuration{}
	err := yaml.Unmarshal(body, config)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func networkDiagnostics(config *Configuration, target *common.DiagnosticStatus) []*common.DiagnosticCheck {
	supportUrl := settings.Global.DocsLink("troubleshooting/firewall-and-proxies")
	if config == nil || config.Network == nil {
		return target.Checks
	}
	diagnosticsStopwatch := common.Stopwatch("Full network diagnostics time was about")
	hostnames := config.Network.Hostnames()
	dnsStopwatch := common.Stopwatch("DNS lookup time for %d hostnames was about", len(hostnames))
	for _, host := range hostnames {
		target.Checks = append(target.Checks, dnsLookupCheck(host))
	}
	target.Details["dns-lookup-time"] = dnsStopwatch.Text()
	tlsRoots := make(map[string]bool)
	tlsStopwatch := common.Stopwatch("TLS verification time for %d hostnames was about", len(hostnames))
	for _, host := range hostnames {
		target.Checks = append(target.Checks, tlsCheckHost(host, tlsRoots)...)
	}
	target.Details["tls-lookup-time"] = tlsStopwatch.Text()
	if len(hostnames) > 1 && len(tlsRoots) == 1 {
		for name, _ := range tlsRoots {
			target.Details["tls-proxy-firewall"] = name
		}
	} else {
		target.Details["tls-proxy-firewall"] = "undetectable"
	}
	headStopwatch := common.Stopwatch("HEAD request time for %d requests was about", len(config.Network.Head))
	for _, entry := range config.Network.Head {
		target.Checks = append(target.Checks, webDiagnostics("HEAD", common.CategoryNetworkHEAD, headRequest, entry, supportUrl)...)
	}
	target.Details["head-time"] = headStopwatch.Text()
	getStopwatch := common.Stopwatch("GET request time for %d requests was about", len(config.Network.Get))
	for _, entry := range config.Network.Get {
		target.Checks = append(target.Checks, webDiagnostics("GET", common.CategoryNetworkCanary, getRequest, entry, supportUrl)...)
	}
	target.Details["get-time"] = getStopwatch.Text()
	target.Details["diagnostics-time"] = diagnosticsStopwatch.Text()
	return target.Checks
}

func webDiagnostics(label string, category uint64, tool webtool, item *WebConfig, supportUrl string) []*common.DiagnosticCheck {
	result := make([]*common.DiagnosticCheck, 0, 2)
	code, fingerprint, err := tool(item.URL)
	valid := set.Set(item.Codes)
	member := set.Member(valid, code)
	if member {
		result = append(result, &common.DiagnosticCheck{
			Type:     "network",
			Category: category,
			Status:   statusOk,
			Message:  fmt.Sprintf("%s %q successful with status %d.", label, item.URL, code),
			Link:     supportUrl,
		})
	} else {
		result = append(result, &common.DiagnosticCheck{
			Type:     "network",
			Category: category,
			Status:   statusFail,
			Message:  fmt.Sprintf("%s %q failed with status %d.", label, item.URL, code),
			Link:     supportUrl,
		})
	}
	if err != nil {
		result = append(result, &common.DiagnosticCheck{
			Type:     "network",
			Category: category,
			Status:   statusWarning,
			Message:  fmt.Sprintf("%s %q resulted error: %v.", label, item.URL, err),
			Link:     supportUrl,
		})
	}
	if len(item.Fingerprint) > 0 && !strings.HasPrefix(fingerprint, item.Fingerprint) {
		result = append(result, &common.DiagnosticCheck{
			Type:     "network",
			Category: category,
			Status:   statusWarning,
			Message:  fmt.Sprintf("%s %q fingerprint mismatch: expected %q, but got %q instead.", label, item.URL, item.Fingerprint, fingerprint),
			Link:     supportUrl,
		})
	}
	return result
}

func digest(body []byte) string {
	digester := sha256.New()
	digester.Write(body)
	return fmt.Sprintf("%02x", digester.Sum([]byte{}))
}

func headRequest(link string) (code int, fingerprint string, err error) {
	defer fail.Around(&err)

	client, err := cloud.NewClient(link)
	fail.On(err != nil, "Client for %q failed, reason: %v", link, err)
	if common.TraceFlag() {
		client = client.WithTracing()
	}
	request := client.NewRequest("")
	response := client.Head(request)
	fail.On(response.Err != nil, "HEAD request to %q failed, reason: %v", link, response.Err)

	return response.Status, digest(response.Body), nil
}

func getRequest(link string) (code int, fingerprint string, err error) {
	defer fail.Around(&err)

	client, err := cloud.NewClient(link)
	fail.On(err != nil, "Client for %q failed, reason: %v", link, err)
	if common.TraceFlag() {
		client = client.WithTracing()
	}
	request := client.NewRequest("")
	response := client.Get(request)
	fail.On(response.Err != nil, "HEAD request to %q failed, reason: %v", link, response.Err)

	return response.Status, digest(response.Body), nil
}
