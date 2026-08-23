package environmentlifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/htfs"
	"github.com/joshyorko/rcc/operations"
	"github.com/joshyorko/rcc/robot"
)

type CurrentRCCBuilder struct{}

var collectBuildCompatibility = compatibilityForBuiltEnvironment

func compatibilityForBuiltEnvironment(ctx context.Context, library htfs.Library, blueprint []byte, platform environmentartifact.Platform) (environmentartifact.CompatibilityRequirements, error) {
	materialization, err := library.Restore(blueprint, []byte(common.ControllerIdentity()), []byte(common.HolotreeSpace))
	if err != nil {
		return environmentartifact.CompatibilityRequirements{}, err
	}
	return compatibilityForMaterialization(ctx, materialization, platform)
}

func (CurrentRCCBuilder) Build(ctx context.Context, robotFile string) (BuildResult, error) {
	if err := ctx.Err(); err != nil {
		return BuildResult{}, err
	}
	sourceKind := filepath.Base(robotFile)
	if sourceKind != "package.yaml" && sourceKind != "robot.yaml" {
		sourceKind = "robot.yaml"
	}
	var config robot.Robot
	var blueprint []byte
	var err error
	if sourceKind == "package.yaml" {
		config, blueprint, err = htfs.ComposeFinalBlueprint([]string{robotFile}, "", false)
	} else {
		config, blueprint, err = htfs.ComposeFinalBlueprint(nil, robotFile, false)
	}
	if err != nil {
		return BuildResult{}, fmt.Errorf("compose current RCC blueprint: %w", err)
	}
	if config == nil && sourceKind != "package.yaml" {
		return BuildResult{}, fmt.Errorf("load robot environment source %q", robotFile)
	}
	condaFile := robotFile
	if config != nil {
		condaFile = config.CondaConfigFile()
	}
	if _, _, err := htfs.NewEnvironment(condaFile, "", false, false, operations.PullCatalog); err != nil {
		return BuildResult{}, fmt.Errorf("build current RCC environment: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return BuildResult{}, err
	}
	library, err := htfs.New()
	if err != nil {
		return BuildResult{}, fmt.Errorf("open current RCC Hololib: %w", err)
	}
	platform := environmentartifact.CurrentPlatform()
	platform.RCCPlatform = common.Platform()
	compatibility, err := collectBuildCompatibility(ctx, library, blueprint, platform)
	if err != nil {
		return BuildResult{}, fmt.Errorf("inventory environment compatibility: %w", err)
	}
	builder := environmentartifact.Builder{Kind: "rcc-holotree-v12", RCCVersion: common.Version, CompatibilityKey: "v12-gzip-sha256"}
	specification, err := semanticSpecificationBytes(sourceKind, blueprint, platform, builder, compatibility)
	if err != nil {
		return BuildResult{}, err
	}
	return BuildResult{
		LegacyBlueprint: blueprint, CatalogPath: library.CatalogPath(common.BlueprintHash(blueprint)),
		SpecificationBytes: specification, SourceKind: sourceKind, Platform: platform, Builder: builder,
		Compatibility: compatibility,
	}, nil
}

func semanticSpecificationBytes(sourceKind string, legacyBlueprint []byte, platform environmentartifact.Platform, builder environmentartifact.Builder, compatibility environmentartifact.CompatibilityRequirements) ([]byte, error) {
	content, err := json.Marshal(map[string]any{
		"mediaType": environmentartifact.SpecificationMediaType, "schemaVersion": environmentartifact.SchemaVersionV1,
		"sourceKind": sourceKind, "platform": platform, "builder": builder, "compatibility": compatibility, "normalizedBlueprint": string(legacyBlueprint),
	})
	if err != nil {
		return nil, fmt.Errorf("encode semantic specification: %w", err)
	}
	if err := environmentartifact.ValidateSpecificationBytes(content); err != nil {
		return nil, err
	}
	return content, nil
}
