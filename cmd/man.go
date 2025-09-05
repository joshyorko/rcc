package cmd

import (
	"github.com/robocorp/rcc/blobs"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pretty"
	"github.com/spf13/cobra"
)

type (
	cobraCommand func(*cobra.Command, []string)
)

var manCmd = &cobra.Command{
	Use:     "man",
	Aliases: []string{"manuals", "docs", "doc", "guides", "guide", "m"},
	Short:   "Group of commands related to `rcc documentation`.",
	Long:    "Build in documentation and manuals.",
}

func init() {
	rootCmd.AddCommand(manCmd)

	manCmd.AddCommand(&cobra.Command{
		Use:     "changelog",
		Short:   "Show the rcc changelog.",
		Long:    "Show the rcc changelog.",
		Aliases: []string{"changes"},
		Run:     makeShowDoc("changelog", "docs/changelog.md"),
	})

	manCmd.AddCommand(&cobra.Command{
		Use:   "features",
		Short: "Show some of rcc features.",
		Long:  "Show some of rcc features.",
		Run:   makeShowDoc("features", "docs/features.md"),
	})

	manCmd.AddCommand(&cobra.Command{
		Use:   "license",
		Short: "Show the rcc License.",
		Long:  "Show the rcc License.",
		Run:   makeShowDoc("LICENSE", "assets/man/LICENSE.txt"),
	})

	manCmd.AddCommand(&cobra.Command{
		Use:   "maintenance",
		Short: "Show holotree maintenance documentation.",
		Long:  "Show holotree maintenance documentation.",
		Run:   makeShowDoc("holotree maintenance documentation", "docs/maintenance.md"),
	})

	manCmd.AddCommand(&cobra.Command{
		Use:   "profiles",
		Short: "Show configuration profiles documentation.",
		Long:  "Show configuration profiles documentation.",
		Run:   makeShowDoc("profile documentation", "docs/profile_configuration.md"),
	})

	manCmd.AddCommand(&cobra.Command{
		Use:     "recipes",
		Short:   "Show rcc recipes, tips, and tricks.",
		Long:    "Show rcc recipes, tips, and tricks.",
		Aliases: []string{"recipe", "tips", "tricks"},
		Run:     makeShowDoc("recipes", "docs/recipes.md"),
	})

	manCmd.AddCommand(&cobra.Command{
		Use:   "troubleshooting",
		Short: "Show the rcc troubleshooting documentation.",
		Long:  "Show the rcc troubleshooting documentation.",
		Run:   makeShowDoc("troubleshooting", "docs/troubleshooting.md"),
	})

	manCmd.AddCommand(&cobra.Command{
		Use:   "usecases",
		Short: "Show some of rcc use cases.",
		Long:  "Show some of rcc use cases.",
		Run:   makeShowDoc("use-cases", "docs/usecases.md"),
	})

	tutorial := &cobra.Command{
		Use:     "tutorial",
		Short:   "Show the rcc tutorial.",
		Long:    "Show the rcc tutorial.",
		Aliases: []string{"tut"},
		Run:     makeShowDoc("tutorial", "assets/man/tutorial.txt"),
	}

	manCmd.AddCommand(&cobra.Command{
		Use:   "venv",
		Short: "Show virtual environment documentation.",
		Long:  "Show virtual environment documentation.",
		Run:   makeShowDoc("venv", "docs/venv.md"),
	})

	manCmd.AddCommand(&cobra.Command{
		Use:     "vocabulary",
		Short:   "Show vocabulary documentation",
		Long:    "Show vocabulary documentation",
		Aliases: []string{"glossary", "lexicon"},
		Run:     makeShowDoc("vocabulary documentation", "docs/vocabulary.md"),
	})

	if common.Product.IsLegacy() {
		manCmd.AddCommand(tutorial)
		rootCmd.AddCommand(tutorial)
	}
}

func makeShowDoc(label, asset string) cobraCommand {
	return func(cmd *cobra.Command, args []string) {
		content, err := blobs.Asset(asset)
		if err != nil {
			pretty.Exit(1, "Cannot show %s documentation, reason: %v", label, err)
		}
		pretty.Page(content)
	}
}
