package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/settings"

	"github.com/spf13/cobra"
)

var (
	configFile      string
	profileName     string
	clearProfile    bool
	immediateSwitch bool
)

func profileMap() map[string]string {
	pattern := common.ExpandPath(filepath.Join(common.Product.Home(), "profile_*.yaml"))
	found, err := filepath.Glob(pattern)
	pretty.Guard(err == nil, 1, "Error while searching profiles: %v", err)
	profiles := make(map[string]string)
	for _, name := range found {
		profile := settings.Profile{}
		err = profile.LoadFrom(name)
		if err == nil {
			profiles[profile.Name] = profile.Description
		}
	}
	return profiles
}

func jsonListProfiles() {
	profiles := make(map[string]interface{})
	profiles["profiles"] = profileMap()
	profiles["current"] = settings.Global.Name()
	content, err := operations.NiceJsonOutput(profiles)
	pretty.Guard(err == nil, 1, "Error serializing profiles: %v", err)
	common.Stdout("%s\n", content)
}

func listProfiles() {
	profiles := profileMap()
	pretty.Guard(len(profiles) > 0, 2, "No profiles found, you must first import some.")
	common.Stdout("Available profiles:\n")
	for name, description := range profiles {
		common.Stdout("- %s: %s\n", name, description)
	}
	common.Stdout("\n")
}

func profileFullPath(name string) string {
	filename := fmt.Sprintf("profile_%s.yaml", strings.ToLower(name))
	return common.ExpandPath(filepath.Join(common.Product.Home(), filename))
}

func loadNamedProfile(name string) *settings.Profile {
	fullpath := profileFullPath(name)
	profile := &settings.Profile{}
	err := profile.LoadFrom(fullpath)
	pretty.Guard(err == nil, 3, "Error while loading/parsing profile, reason: %v", err)
	return profile
}

func switchProfileTo(name string) {
	profile := loadNamedProfile(name)
	err := profile.Activate()
	pretty.Guard(err == nil, 4, "Error while activating profile, reason: %v", err)
}

func cleanupProfile() {
	profile := settings.Profile{}
	err := profile.Remove()
	pretty.Guard(err == nil, 5, "Error while clearing profile, reason: %v", err)
}

var configureSwitchCmd = &cobra.Command{
	Use:   "switch",
	Short: fmt.Sprintf("Switch active configuration profile for %s tooling.", common.Product.Name()),
	Long:  fmt.Sprintf("Switch active configuration profile for %s tooling.", common.Product.Name()),
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Configuration switch lasted").Report()
		}
		if clearProfile {
			cleanupProfile()
			pretty.Ok()
		} else if len(profileName) == 0 {
			if jsonFlag {
				jsonListProfiles()
			} else {
				listProfiles()
				common.Stdout("Currently active profile is: %s\n", settings.Global.Name())
				pretty.Ok()
			}
		} else {
			switchProfileTo(profileName)
			pretty.Ok()
		}
	},
}

var configureRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: fmt.Sprintf("Remove named a configuration profile for %s tooling.", common.Product.Name()),
	Long:  fmt.Sprintf("Remove named a configuration profile for %s tooling.", common.Product.Name()),
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Configuration remove lasted").Report()
		}
		profiles := profileMap()
		description, ok := profiles[profileName]
		pretty.Guard(ok, 2, "No match for profile with name %q.", profileName)
		fullpath := profileFullPath(profileName)

		common.Log("Trying to remove profile: %s %q [%s].", profileName, description, fullpath)
		err := os.Remove(fullpath)
		pretty.Guard(err == nil, 5, "Error while removing profile file %q, reason: %v", fullpath, err)

		pretty.Ok()
	},
}

var configureExportCmd = &cobra.Command{
	Use:   "export",
	Short: fmt.Sprintf("Export a configuration profile for %s tooling.", common.Product.Name()),
	Long:  fmt.Sprintf("Export a configuration profile for %s tooling.", common.Product.Name()),
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Configuration export lasted").Report()
		}
		profile := loadNamedProfile(profileName)
		err := profile.SaveAs(configFile)
		pretty.Guard(err == nil, 1, "Error while exporting profile, reason: %v", err)
		pretty.Ok()
	},
}

var configureImportCmd = &cobra.Command{
	Use:   "import",
	Short: fmt.Sprintf("Import a configuration profile for %s tooling.", common.Product.Name()),
	Long:  fmt.Sprintf("Import a configuration profile for %s tooling.", common.Product.Name()),
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Configuration import lasted").Report()
		}
		profile := &settings.Profile{}
		err := profile.LoadFrom(configFile)
		pretty.Guard(err == nil, 1, "Error while loading profile: %v", err)
		err = profile.Import()
		pretty.Guard(err == nil, 2, "Error while importing profile: %v", err)
		if immediateSwitch {
			switchProfileTo(profile.Name)
		}
		pretty.Ok()
	},
}

func init() {
	configureCmd.AddCommand(configureSwitchCmd)
	configureSwitchCmd.Flags().StringVarP(&profileName, "profile", "p", "", "The name of configuration profile to activate.")
	configureSwitchCmd.Flags().BoolVarP(&clearProfile, "noprofile", "n", false, "Remove active profile, and reset to defaults.")
	configureSwitchCmd.Flags().BoolVarP(&jsonFlag, "json", "j", false, "Show profile list as JSON stream.")

	configureCmd.AddCommand(configureRemoveCmd)
	configureRemoveCmd.Flags().StringVarP(&profileName, "profile", "p", "", "The name of configuration profile to remove.")
	configureRemoveCmd.MarkFlagRequired("profile")

	configureCmd.AddCommand(configureExportCmd)
	configureExportCmd.Flags().StringVarP(&configFile, "filename", "f", "exported_profile.yaml", "The filename where configuration profile is exported.")
	configureExportCmd.Flags().StringVarP(&profileName, "profile", "p", "", "The name of configuration profile to export.")
	configureExportCmd.MarkFlagRequired("profile")

	configureCmd.AddCommand(configureImportCmd)
	configureImportCmd.Flags().BoolVarP(&immediateSwitch, "switch", "s", false, "Immediately switch to use new profile.")
	configureImportCmd.Flags().StringVarP(&configFile, "filename", "f", "exported_profile.yaml", "The filename to import as configuration profile.")
	configureImportCmd.MarkFlagRequired("filename")
}
