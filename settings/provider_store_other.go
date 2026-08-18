//go:build windows || plan9 || wasip1

package settings

func syncSettingsParent(string) error {
	return nil
}
