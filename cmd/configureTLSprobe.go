package cmd

import (
	"strings"

	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pretty"
	"github.com/spf13/cobra"
)

func fixHosts(hosts []string) []string {
	servers := make([]string, len(hosts))
	for at, host := range hosts {
		if strings.Contains(host, ":") {
			servers[at] = host
		} else {
			servers[at] = host + ":443"
		}
	}
	return servers
}

var tlsProbeCmd = &cobra.Command{
	Use:   "tlsprobe <host:port>+",
	Short: "Probe host:port combinations for supported TLS versions.",
	Long: `Probe host:port combinations for supported TLS versions.

This command will show following information on your TLS settings:
- current DNS resolution give host
- which TLS versions are available on specific host:port combo
- server name, address, port, and cipher suite that actually was negotiated
- certificate chains that was seen on that connection

Examples:
  rcc configuration tlsprobe www.bing.com www.google.com
  rcc configuration tlsprobe outlook.office365.com:993 outlook.office365.com:995
  rcc configuration tlsprobe api.us1.robocorp.com api.eu1.robocorp.com
`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		servers := fixHosts(args)
		err := operations.TLSProbe(servers)
		pretty.Guard(err == nil, 1, "Probe failure: %v", err)
		pretty.Ok()
	},
}

func init() {
	configureCmd.AddCommand(tlsProbeCmd)
}
