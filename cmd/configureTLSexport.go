package cmd

import (
	"crypto/x509"
	"os"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/settings"
	"github.com/spf13/cobra"
)

var (
	pemFile string
)

var tlsExportCmd = &cobra.Command{
	Use:   "tlsexport <configuration.yaml>+",
	Short: "Export TLS certificates from set of secure and unsecure URLs.",
	Long: `Export TLS certificates from set of secure and unsecure URLs.

CLI examples:
  rcc configuration tlsexport --pemfile export.pem robot_urls.yaml
  rcc configuration tlsexport --pemfile many.pem company_urls.yaml robot_urls.yaml more_urls.yaml


Configuration example in YAML format:
# trusted:
#     - https://api.eu1.robocorp.com/
#     - https://pypi.org/
#     - https://files.pythonhosted.org/
# untrusted:
#     - https://self-signed.badssl.com/

Note: exported PEM file is sorted, so it can be easily compared to other export.
      Use your fabourite diffing tool there.
`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, configfiles []string) {
		pretty.Guard(!settings.Global.HasCaBundle(), 1, "Cannot create certificate bundle, while profile provides %q!", common.CaBundleFile())
		err := operations.TLSExport(pemFile, configfiles)
		pretty.Guard(err == nil, 2, "Probe failure: %v", err)
		err = certificatePool(pemFile)
		pretty.Guard(err == nil, 3, "Could not import created CA bundle, reason: %v", err)
		pretty.Ok()
	},
}

func certificatePool(bundle string) (err error) {
	defer fail.Around(&err)

	pool, err := x509.SystemCertPool()
	fail.On(err != nil, "Could not get system certificate pool, reason: %v", err)
	blob, err := os.ReadFile(bundle)
	fail.On(err != nil, "Could not get read certificate bundle from %q, reason: %v", bundle, err)
	fail.On(!pool.AppendCertsFromPEM(blob), "Could not add certs from %q to created pool!", bundle)
	return nil
}

func init() {
	configureCmd.AddCommand(tlsExportCmd)
	tlsExportCmd.Flags().StringVarP(&pemFile, "pemfile", "p", "", "Name of exported PEM file to write.")
	tlsExportCmd.MarkFlagRequired("pemfile")
}
