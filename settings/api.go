package settings

import (
	"net/http"

	"github.com/robocorp/rcc/common"
)

type EndpointsApi func(string) string

type Api interface {
	Name() string
	Description() string
	TemplatesYamlURL() string
	Diagnostics(target *common.DiagnosticStatus)
	Endpoint(string) string
	Option(string) bool
	DefaultEndpoint() string
	IssuesURL() string
	TelemetryURL() string
	PypiURL() string
	PypiTrustedHost() string
	CondaURL() string
	DownloadsLink(resource string) string
	DocsLink(page string) string
	PypiLink(page string) string
	CondaLink(page string) string
	Hostnames() []string
	ConfiguredHttpTransport() *http.Transport
	NoProxy() string
	HttpsProxy() string
	HttpProxy() string
	HasPipRc() bool
	HasMicroMambaRc() bool
	HasCaBundle() bool
	VerifySsl() bool
	NoRevocation() bool
	LegacyRenegotiation() bool
	NoBuid() bool
}
