package blobs_test

import (
	"testing"

	"github.com/robocorp/rcc/blobs"
	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/operations"
)

func TestCanSeeBaseZipAsset(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	must_be.Panic(func() { blobs.MustAsset("assets/missing.zip") })
	wont_be.Panic(func() { blobs.MustAsset("assets/standard.zip") })
	wont_be.Panic(func() { blobs.MustAsset("assets/python.zip") })

	_, err := blobs.Asset("assets/missing.zip")
	wont_be.Nil(err)

	asset, err := blobs.Asset("assets/standard.zip")
	must_be.Nil(err)
	wont_be.Nil(asset)
}

func TestCanOtherAssets(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	must_be.Panic(func() { blobs.MustAsset("assets/missing.yaml") })
	must_be.Panic(func() { blobs.MustAsset("assets/settings.yaml") })

	wont_be.Panic(func() { blobs.MustAsset("assets/robocorp_settings.yaml") })
	wont_be.Panic(func() { blobs.MustAsset("assets/sema4ai_settings.yaml") })

	wont_be.Panic(func() { blobs.MustAsset("assets/micromamba_version.txt") })
	wont_be.Panic(func() { blobs.MustAsset("assets/externally_managed.txt") })

	wont_be.Panic(func() { blobs.MustAsset("assets/templates.yaml") })
	wont_be.Panic(func() { blobs.MustAsset("assets/speedtest.yaml") })

	wont_be.Panic(func() { blobs.MustAsset("assets/man/LICENSE.txt") })
	wont_be.Panic(func() { blobs.MustAsset("assets/man/tutorial.txt") })

	wont_be.Panic(func() { blobs.MustAsset("docs/BUILD.md") })
	wont_be.Panic(func() { blobs.MustAsset("docs/README.md") })
	wont_be.Panic(func() { blobs.MustAsset("docs/changelog.md") })
	wont_be.Panic(func() { blobs.MustAsset("docs/environment-caching.md") })
	wont_be.Panic(func() { blobs.MustAsset("docs/features.md") })
	wont_be.Panic(func() { blobs.MustAsset("docs/profile_configuration.md") })
	wont_be.Panic(func() { blobs.MustAsset("docs/recipes.md") })
	wont_be.Panic(func() { blobs.MustAsset("docs/usecases.md") })
	wont_be.Panic(func() { blobs.MustMicromamba() })
}

func TestCanGetTemplateNamesThruOperations(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	assets := operations.ListTemplates(true)
	wont_be.Nil(assets)
	must_be.True(len(assets) == 3)
	must_be.Equal([]string{"extended", "python", "standard"}, assets)
}
