package settings

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pathlib"
	"gopkg.in/yaml.v2"
)

const (
	httpsPrefix = `https://`
)

type StringMap map[string]string
type BoolMap map[string]bool

func (it StringMap) Lookup(key string) string {
	return it[key]
}

// layer 0 is defaults from assets
// layer 1 is settings.yaml from disk
// layer 2 is "temporary" update layer
type SettingsLayers [3]*Settings

func (it SettingsLayers) Effective() *Settings {
	result := &Settings{
		Autoupdates:  make(StringMap),
		Branding:     make(StringMap),
		Certificates: &Certificates{},
		Network:      &Network{},
		Endpoints:    make(StringMap),
		Options:      make(BoolMap),
		Hosts:        make([]string, 0, 100),
		Meta: &Meta{
			Name:        "generated",
			Description: "generated",
			Source:      "generated",
			Version:     "unknown",
		},
	}
	for _, layer := range it {
		if layer != nil {
			layer.onTopOf(result)
		}
	}
	return result
}

type Settings struct {
	Autoupdates  StringMap     `yaml:"autoupdates,omitempty" json:"autoupdates,omitempty"`
	Branding     StringMap     `yaml:"branding,omitempty" json:"branding,omitempty"`
	Certificates *Certificates `yaml:"certificates,omitempty" json:"certificates,omitempty"`
	Network      *Network      `yaml:"network,omitempty" json:"network,omitempty"`
	Endpoints    StringMap     `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
	Hosts        []string      `yaml:"diagnostics-hosts,omitempty" json:"diagnostics-hosts,omitempty"`
	Options      BoolMap       `yaml:"options,omitempty" json:"options,omitempty"`
	Meta         *Meta         `yaml:"meta,omitempty" json:"meta,omitempty"`
}

func Empty() *Settings {
	return &Settings{
		Meta: &Meta{
			Name:        "generated",
			Description: "generated",
			Source:      "generated",
			Version:     "unknown",
		},
	}
}

func FromBytes(raw []byte) (*Settings, error) {
	var settings Settings
	err := yaml.Unmarshal(raw, &settings)
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

func (it *Settings) onTopOf(target *Settings) {
	for key, value := range it.Autoupdates {
		if len(value) > 0 {
			target.Autoupdates[key] = value
		}
	}
	for key, value := range it.Branding {
		if len(value) > 0 {
			target.Branding[key] = value
		}
	}
	for key, value := range it.Endpoints {
		if len(value) > 0 {
			target.Endpoints[key] = value
		}
	}
	for key, value := range it.Options {
		target.Options[key] = value
	}
	for _, host := range it.Hosts {
		target.Hosts = append(target.Hosts, host)
	}
	if it.Certificates != nil {
		it.Certificates.onTopOf(target)
	}
	if it.Network != nil {
		it.Network.onTopOf(target)
	}
	if it.Meta != nil {
		it.Meta.onTopOf(target)
	}
}

func (it *Settings) AsYaml() ([]byte, error) {
	content, err := yaml.Marshal(it)
	if err != nil {
		return nil, err
	}
	return content, nil
}

func (it *Settings) Source(filename string) *Settings {
	if it.Meta != nil && len(filename) > 0 {
		it.Meta.Source = filename
	}
	return it
}

func (it *Settings) Hostnames() []string {
	collector := make(map[string]bool)
	if it.Endpoints != nil {
		for _, name := range it.Endpoints {
			hostFromUrl(name, collector)
		}
	}
	if it.Hosts != nil {
		for _, name := range it.Hosts {
			collector[name] = true
		}
	}
	result := make([]string, 0, len(collector))
	for key, _ := range collector {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func (it *Settings) AsJson() ([]byte, error) {
	content, err := json.MarshalIndent(it, "", "  ")
	if err != nil {
		return nil, err
	}
	return content, nil
}

func diagnoseUrl(link, label string, diagnose common.Diagnoser, correct bool) bool {
	if len(link) == 0 {
		diagnose.Fatal(0, "", "required %q URL is missing.", label)
		return false
	}
	if !strings.HasPrefix(link, httpsPrefix) {
		diagnose.Fatal(0, "", "%q URL %q is does not start with %q prefix.", label, link, httpsPrefix)
		return false
	}
	_, err := url.Parse(link)
	if err != nil {
		diagnose.Fatal(0, "", "%q URL %q cannot be parsed, reason %v.", label, link, err)
		return false
	}
	return correct
}

func diagnoseOptionalUrl(link, label string, diagnose common.Diagnoser, correct bool) bool {
	if len(strings.TrimSpace(link)) == 0 {
		return correct
	} else {
		return diagnoseUrl(link, label, diagnose, correct)
	}
}

func (it *Settings) CriticalEnvironmentDiagnostics(target *common.DiagnosticStatus) {
	diagnose := target.Diagnose("settings.yaml")
	correct := true
	if it.Endpoints == nil {
		diagnose.Fatal(0, "", "endpoints section is totally missing")
		correct = false
	} else {
		correct = diagnoseUrl(it.Endpoints["cloud-api"], "endpoints/cloud-api", diagnose, correct)
		correct = diagnoseUrl(it.Endpoints["downloads"], "endpoints/downloads", diagnose, correct)
	}
	if correct {
		diagnose.Ok(0, "Critical environment diagnostics are ok.")
	}
}

func (it *Settings) Diagnostics(target *common.DiagnosticStatus) {
	diagnose := target.Diagnose("Settings")
	correct := true
	if it.Certificates == nil {
		diagnose.Warning(0, "", "settings.yaml: certificates section is totally missing")
		correct = false
	}
	if it.Endpoints == nil {
		diagnose.Warning(0, "", "settings.yaml: endpoints section is totally missing")
		correct = false
	} else {
		correct = diagnoseUrl(it.Endpoints["cloud-api"], "endpoints/cloud-api", diagnose, correct)
		correct = diagnoseUrl(it.Endpoints["downloads"], "endpoints/downloads", diagnose, correct)

		correct = diagnoseOptionalUrl(it.Endpoints["cloud-ui"], "endpoints/cloud-ui", diagnose, correct)
		correct = diagnoseOptionalUrl(it.Endpoints["cloud-linking"], "endpoints/cloud-linking", diagnose, correct)
		correct = diagnoseOptionalUrl(it.Endpoints["issues"], "endpoints/issues", diagnose, correct)
		correct = diagnoseOptionalUrl(it.Endpoints["telemetry"], "endpoints/telemetry", diagnose, correct)
		correct = diagnoseOptionalUrl(it.Endpoints["docs"], "endpoints/docs", diagnose, correct)
		correct = diagnoseOptionalUrl(it.Endpoints["conda"], "endpoints/conda", diagnose, correct)
		correct = diagnoseOptionalUrl(it.Endpoints["pypi"], "endpoints/pypi", diagnose, correct)
		correct = diagnoseOptionalUrl(it.Endpoints["pypi-trusted"], "endpoints/pypi-trusted", diagnose, correct)
	}
	if it.Meta == nil {
		diagnose.Warning(0, "", "settings.yaml: meta section is totally missing")
		correct = false
	}
	if correct {
		diagnose.Ok(0, "In general, 'settings.yaml' is ok.")
	}
}

type Certificates struct {
	VerifySsl           bool   `yaml:"verify-ssl" json:"verify-ssl"`
	SslNoRevoke         bool   `yaml:"ssl-no-revoke" json:"ssl-no-revoke"`
	LegacyRenegotiation bool   `yaml:"legacy-renegotiation-allowed" json:"legacy-renegotiation-allowed"`
	CaBundle            string `yaml:"ca-bundle,omitempty" json:"ca-bundle,omitempty"`
}

func (it *Certificates) onTopOf(target *Settings) {
	if target.Certificates == nil {
		target.Certificates = &Certificates{}
	}
	target.Certificates.VerifySsl = it.VerifySsl
	target.Certificates.SslNoRevoke = it.SslNoRevoke
	target.Certificates.LegacyRenegotiation = it.LegacyRenegotiation
	if pathlib.IsFile(common.CaBundleFile()) {
		target.Certificates.CaBundle = common.CaBundleFile()
	}
}

func justHostAndPort(link string) string {
	if len(link) == 0 {
		return ""
	}
	parsed, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return parsed.Host
}

func hostFromUrl(link string, collector map[string]bool) {
	host := justHostAndPort(link)
	if len(host) > 0 {
		parts := strings.SplitN(host, ":", 2)
		collector[parts[0]] = true
	}
}

type Meta struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Source      string `yaml:"source" json:"source"`
	Version     string `yaml:"version" json:"version"`
}

func (it *Meta) onTopOf(target *Settings) {
	if target.Meta == nil {
		target.Meta = &Meta{}
	}
	if len(it.Name) > 0 {
		target.Meta.Name = it.Name
	}
	if len(it.Description) > 0 {
		target.Meta.Description = it.Description
	}
	if len(it.Source) > 0 {
		target.Meta.Source = it.Source
	}
	if len(it.Version) > 0 {
		target.Meta.Version = it.Version
	}
}

type Network struct {
	NoProxy    string `yaml:"no-proxy" json:"no-proxy"`
	HttpsProxy string `yaml:"https-proxy" json:"https-proxy"`
	HttpProxy  string `yaml:"http-proxy" json:"http-proxy"`
}

func (it *Network) onTopOf(target *Settings) {
	if target.Network == nil {
		target.Network = &Network{}
	}
	if len(it.NoProxy) > 0 {
		target.Network.NoProxy = it.NoProxy
	}
	if len(it.HttpsProxy) > 0 {
		target.Network.HttpsProxy = it.HttpsProxy
	}
	if len(it.HttpProxy) > 0 {
		target.Network.HttpProxy = it.HttpProxy
	}
}
