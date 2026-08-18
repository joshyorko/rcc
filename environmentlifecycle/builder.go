package environmentlifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/htfs"
	"github.com/joshyorko/rcc/operations"
)

type CurrentRCCBuilder struct{}

func (CurrentRCCBuilder) Build(ctx context.Context, robotFile string) (BuildResult, error) {
	if err := ctx.Err(); err != nil {
		return BuildResult{}, err
	}
	config, blueprint, err := htfs.ComposeFinalBlueprint(nil, robotFile, false)
	if err != nil {
		return BuildResult{}, fmt.Errorf("compose current RCC blueprint: %w", err)
	}
	if config == nil {
		return BuildResult{}, fmt.Errorf("load robot environment source %q", robotFile)
	}
	if _, _, err := htfs.NewEnvironment(config.CondaConfigFile(), "", false, false, operations.PullCatalog); err != nil {
		return BuildResult{}, fmt.Errorf("build current RCC environment: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return BuildResult{}, err
	}
	library, err := htfs.New()
	if err != nil {
		return BuildResult{}, fmt.Errorf("open current RCC Hololib: %w", err)
	}
	platform := environmentartifact.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH, RCCPlatform: common.Platform()}
	builder := environmentartifact.Builder{Kind: "rcc-holotree-v12", RCCVersion: common.Version, CompatibilityKey: "v12-gzip-sha256"}
	specification, err := semanticSpecificationBytes("robot.yaml", blueprint, platform, builder)
	if err != nil {
		return BuildResult{}, err
	}
	return BuildResult{
		LegacyBlueprint: blueprint, CatalogPath: library.CatalogPath(common.BlueprintHash(blueprint)),
		SpecificationBytes: specification, SourceKind: "robot.yaml", Platform: platform, Builder: builder,
	}, nil
}

func semanticSpecificationBytes(sourceKind string, legacyBlueprint []byte, platform environmentartifact.Platform, builder environmentartifact.Builder) ([]byte, error) {
	content, err := json.Marshal(map[string]any{
		"mediaType": environmentartifact.SpecificationMediaType, "schemaVersion": environmentartifact.SchemaVersionV1,
		"sourceKind": sourceKind, "platform": platform, "builder": builder, "normalizedBlueprint": string(legacyBlueprint),
	})
	if err != nil {
		return nil, fmt.Errorf("encode semantic specification: %w", err)
	}
	if err := environmentartifact.ValidateSpecificationBytes(content); err != nil {
		return nil, err
	}
	return content, nil
}
