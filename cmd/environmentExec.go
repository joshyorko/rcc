package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/spf13/cobra"
)

type environmentExecResult struct {
	ArtifactDigest    environmentartifact.Digest                 `json:"artifactDigest"`
	MaterializationID string                                     `json:"materializationId"`
	Path              string                                     `json:"path"`
	CacheHit          environmentlifecycle.CacheProvenance       `json:"cacheHit"`
	ExitCode          int                                        `json:"exitCode"`
	LeaseID           string                                     `json:"leaseId"`
	Compatibility     *environmentlifecycle.CompatibilityReceipt `json:"compatibility,omitempty"`
	Verification      *artifacttrust.VerificationReceipt         `json:"verification,omitempty"`
	Status            string                                     `json:"status,omitempty"`
	Reason            string                                     `json:"reason,omitempty"`
}

var replaceReceipt = replaceReceiptFile

func newEnvironmentExecCommand(dependencies environmentCommandDependencies) *cobra.Command {
	var artifact, providerURL, trustCarrierPath, trustCarrierType string
	var strictRemote, permissiveLocal bool
	var jsonOutput, inheritStreams bool
	var receiptFile string
	command := &cobra.Command{
		Use:          "exec -- <command> [args...]",
		Short:        "Execute a command with a process-scoped environment lease.",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(command *cobra.Command, arguments []string) error {
			if command.ArgsLenAtDash() != 0 {
				return fmt.Errorf("env exec requires command arguments after --")
			}
			if !jsonOutput && !inheritStreams {
				return fmt.Errorf("--json is required")
			}
			if inheritStreams && receiptFile == "" {
				return fmt.Errorf("--receipt-file is required with --inherit-streams")
			}
			digest, err := environmentartifact.ParseDigest(artifact)
			if err != nil {
				return err
			}
			provider, err := optionalEnvironmentProvider(providerURL, dependencies.newProvider)
			if err != nil {
				return err
			}
			if dependencies.acquire == nil || dependencies.materializer == nil || (!inheritStreams && dependencies.execute == nil) {
				return fmt.Errorf("environment execution dependencies are unavailable")
			}
			policy, err := trustPolicyForCommand(strictRemote, permissiveLocal)
			if err != nil {
				return err
			}
			trustCarrier, err := optionalEnvironmentTrustCarrier(trustCarrierPath, trustCarrierType, providerURL)
			if err != nil {
				return err
			}
			acquired, err := dependencies.acquire(command.Context(), environmentlifecycle.AcquireRequest{
				ArtifactDigest: digest, Provider: provider, TrustPolicy: &policy, TrustCarrier: trustCarrier,
			})
			if err != nil {
				return err
			}
			materialization := environmentlifecycle.Materialization{
				ArtifactDigest: acquired.ArtifactDigest, ID: acquired.MaterializationID,
				Path: acquired.Path, CacheHit: acquired.CacheHit, Verification: acquired.Verification,
				TrustPolicy: acquired.TrustPolicy, TrustRequest: acquired.TrustRequest, TrustCarrier: acquired.TrustCarrier,
			}
			var handle environmentlifecycle.ExecutionHandle
			var child environmentlifecycle.ChildResult
			if inheritStreams {
				if dependencies.materializer == nil {
					return fmt.Errorf("environment execution dependencies are unavailable")
				}
				handle, child, err = environmentlifecycle.ExecuteWithStreams(command.Context(), dependencies.materializer(), materialization, arguments, command.InOrStdin(), command.OutOrStdout(), command.ErrOrStderr())
			} else {
				handle, child, err = dependencies.execute(command.Context(), dependencies.materializer(), materialization, arguments)
			}
			executionErr := err
			if executionErr != nil && !inheritStreams {
				return executionErr
			}
			var compatibility *environmentlifecycle.CompatibilityReceipt
			if acquired.Compatibility.SchemaVersion != 0 {
				compatibility = &acquired.Compatibility
			}
			output := environmentExecResult{
				ArtifactDigest: acquired.ArtifactDigest, MaterializationID: acquired.MaterializationID,
				Path: acquired.Path, CacheHit: acquired.CacheHit, ExitCode: child.ExitCode, LeaseID: handle.LeaseID, Compatibility: compatibility,
			}
			if executionErr != nil && isExecutionCancellation(executionErr) {
				output.Status = "cancelled"
				output.Reason = executionErr.Error()
			} else if executionErr != nil || child.ExitCode != 0 {
				output.Status = "failed"
				if executionErr != nil {
					output.Reason = executionErr.Error()
				} else {
					output.Reason = "child exited non-zero"
				}
			} else {
				output.Status = "completed"
			}
			verification := handle.Verification
			if verification.Code == "" {
				verification = acquired.Verification
			}
			if verification.Code != "" {
				output.Verification = &verification
			}
			if inheritStreams {
				if err := writeEnvironmentExecReceipt(receiptFile, output); err != nil {
					return err
				}
			} else if err := json.NewEncoder(command.OutOrStdout()).Encode(output); err != nil {
				return err
			}
			if child.ExitCode != 0 && executionErr == nil {
				panic(common.ExitCode{Code: child.ExitCode})
			}
			if executionErr != nil {
				return executionErr
			}
			return nil
		},
	}
	command.Flags().StringVar(&artifact, "artifact", "", "Canonical sha256 environment artifact digest.")
	command.Flags().StringVar(&providerURL, "provider", "", "Environment artifact provider URL; optional for local-ready artifacts.")
	command.Flags().StringVar(&trustCarrierPath, "trust-carrier", "", "Detached trust carrier path or URL; defaults to provider HTTP or local filesystem.")
	command.Flags().StringVar(&trustCarrierType, "trust-carrier-type", "auto", "Trust carrier type: auto, filesystem, archive, or http.")
	command.Flags().BoolVar(&jsonOutput, "json", false, "Write one JSON result object to stdout.")
	command.Flags().BoolVar(&inheritStreams, "inherit-streams", false, "Connect child stdin/stdout/stderr for the full process lifetime.")
	command.Flags().StringVar(&receiptFile, "receipt-file", "", "Required atomic JSON receipt path for --inherit-streams.")
	command.Flags().BoolVar(&strictRemote, "strict-remote", false, "Require detached signatures before execution.")
	command.Flags().BoolVar(&permissiveLocal, "permissive-local", false, "Explicitly allow unsigned local artifacts.")
	return command
}

func writeEnvironmentExecReceipt(path string, value environmentExecResult) error {
	if path == "" || filepath.Base(path) == "." {
		return fmt.Errorf("invalid --receipt-file")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("create receipt directory: %w", err)
	}
	if err := validateReceiptPath(path); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".rcc-receipt-*")
	if err != nil {
		return fmt.Errorf("create receipt temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	encodeErr := json.NewEncoder(tmp).Encode(value)
	if encodeErr == nil {
		encodeErr = tmp.Sync()
	}
	closeErr := tmp.Close()
	if encodeErr != nil {
		return fmt.Errorf("write execution receipt: %w", encodeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close execution receipt: %w", closeErr)
	}
	if err := replaceReceipt(tmpPath, path); err != nil {
		return fmt.Errorf("install execution receipt: %w", err)
	}
	return nil
}

func isExecutionCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func validateReceiptPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid --receipt-file: %w", err)
	}
	if info, err := os.Lstat(abs); err == nil {
		if info.IsDir() {
			return fmt.Errorf("--receipt-file is a directory")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("--receipt-file must not be a symlink")
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect --receipt-file: %w", err)
	}
	parent := filepath.Dir(abs)
	for {
		info, statErr := os.Lstat(parent)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("receipt parent must not contain symlinks")
			}
			if !info.IsDir() {
				return fmt.Errorf("receipt parent is not a directory")
			}
			if parent == filepath.Dir(parent) {
				break
			}
			parent = filepath.Dir(parent)
			continue
		}
		if !os.IsNotExist(statErr) {
			return fmt.Errorf("inspect receipt parent: %w", statErr)
		}
		parent = filepath.Dir(parent)
	}
	return nil
}
