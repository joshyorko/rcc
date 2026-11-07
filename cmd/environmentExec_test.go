package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
)

func TestEnvironmentExecRequiresSelectedExecutor(t *testing.T) {
	for _, mode := range []string{"json", "streams", "both"} {
		t.Run(mode, func(t *testing.T) {
			dependencies := environmentCommandDependencies{
				acquire: func(context.Context, environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
					t.Fatal("acquisition started without the selected executor")
					return environmentlifecycle.AcquireResult{}, nil
				},
				materializer: func() environmentlifecycle.Materializer {
					t.Fatal("materializer created without the selected executor")
					return nil
				},
			}
			if mode == "json" {
				dependencies.executeWithStreams = func(context.Context, environmentlifecycle.Materializer, environmentlifecycle.Materialization, []string, io.Reader, io.Writer, io.Writer) (environmentlifecycle.ExecutionHandle, environmentlifecycle.ChildResult, error) {
					t.Fatal("fell back to streams executor")
					return environmentlifecycle.ExecutionHandle{}, environmentlifecycle.ChildResult{}, nil
				}
			} else {
				dependencies.execute = func(context.Context, environmentlifecycle.Materializer, environmentlifecycle.Materialization, []string) (environmentlifecycle.ExecutionHandle, environmentlifecycle.ChildResult, error) {
					t.Fatal("fell back to JSON executor")
					return environmentlifecycle.ExecutionHandle{}, environmentlifecycle.ChildResult{}, nil
				}
			}
			arguments := []string{"exec", "--artifact", cliTestDigest}
			if mode != "streams" {
				arguments = append(arguments, "--json")
			}
			if mode != "json" {
				arguments = append(arguments, "--inherit-streams", "--receipt-file", filepath.Join(t.TempDir(), "receipt.json"))
			}
			command := newEnvironmentCommand(dependencies)
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			err := runCobraCommand(command, append(arguments, "--", "python", "-V"))
			if err == nil || !strings.Contains(err.Error(), "environment execution dependencies are unavailable") {
				t.Fatalf("missing selected executor: %v", err)
			}
		})
	}
}

func TestEnvironmentExecSelectsExecutorAndPreservesTrust(t *testing.T) {
	for _, mode := range []string{"json", "streams", "both"} {
		for _, outcome := range []struct {
			name   string
			code   int
			err    error
			status string
		}{
			{name: "success", status: "completed"},
			{name: "nonzero", code: 7, status: "failed"},
			{name: "cancelled", code: -1, err: context.Canceled, status: "cancelled"},
			{name: "deadline", code: -1, err: context.DeadlineExceeded, status: "cancelled"},
			{name: "execution-error", code: 7, err: errors.New("execution failed"), status: "failed"},
		} {
			t.Run(mode+"/"+outcome.name, func(t *testing.T) {
				digest, _ := environmentartifact.ParseDigest(cliTestDigest)
				policy := &artifacttrust.Policy{Mode: artifacttrust.StrictRemote, Revision: "sentinel-policy", AcceptedKeys: []string{"sentinel-key"}}
				request := &artifacttrust.VerifyRequest{Builder: "sentinel-builder", Platform: "sentinel-platform"}
				carrier := artifacttrust.NewFilesystemCarrier(t.TempDir())
				verification := artifacttrust.VerificationReceipt{Valid: true, Code: artifacttrust.CodeValid, DecisionID: "acquire-decision"}
				leasedVerification := verification
				leasedVerification.DecisionID = "lease-decision"
				materializer := &exitMaterializer{cwd: t.TempDir()}
				stdin := strings.NewReader("input")
				var stdout, stderr bytes.Buffer
				jsonCalls, streamCalls := 0, 0
				check := func(gotMaterializer environmentlifecycle.Materializer, got environmentlifecycle.Materialization, args []string) {
					t.Helper()
					if gotMaterializer != materializer || got.ArtifactDigest != digest || got.ID != "materialized" || got.Path != materializer.cwd || got.CacheHit != environmentlifecycle.CacheLocalMaterialization {
						t.Fatalf("materialization handoff: %+v", got)
					}
					if got.TrustPolicy != policy || got.TrustRequest != request || got.TrustCarrier != carrier || got.Verification != verification {
						t.Fatalf("runtime trust handoff: %+v", got)
					}
					if !reflect.DeepEqual(args, []string{"python", "two words", "--child-flag"}) {
						t.Fatalf("command arguments = %q", args)
					}
				}
				dependencies := environmentCommandDependencies{
					acquire: func(context.Context, environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
						return environmentlifecycle.AcquireResult{
							ArtifactDigest: digest, MaterializationID: "materialized", Path: materializer.cwd,
							CacheHit: environmentlifecycle.CacheLocalMaterialization, Verification: verification,
							TrustPolicy: policy, TrustRequest: request, TrustCarrier: carrier,
						}, nil
					},
					materializer: func() environmentlifecycle.Materializer { return materializer },
					execute: func(_ context.Context, m environmentlifecycle.Materializer, value environmentlifecycle.Materialization, args []string) (environmentlifecycle.ExecutionHandle, environmentlifecycle.ChildResult, error) {
						jsonCalls++
						check(m, value, args)
						return environmentlifecycle.ExecutionHandle{LeaseID: "lease", Verification: leasedVerification}, environmentlifecycle.ChildResult{ExitCode: outcome.code}, outcome.err
					},
					executeWithStreams: func(_ context.Context, m environmentlifecycle.Materializer, value environmentlifecycle.Materialization, args []string, in io.Reader, out, errout io.Writer) (environmentlifecycle.ExecutionHandle, environmentlifecycle.ChildResult, error) {
						streamCalls++
						check(m, value, args)
						if in != stdin || out != &stdout || errout != &stderr {
							t.Fatal("child streams were replaced")
						}
						_, _ = io.WriteString(out, "child-output")
						_, _ = io.WriteString(errout, "child-error")
						return environmentlifecycle.ExecutionHandle{LeaseID: "lease", Verification: leasedVerification}, environmentlifecycle.ChildResult{ExitCode: outcome.code}, outcome.err
					},
				}
				command := newEnvironmentCommand(dependencies)
				command.SetIn(stdin)
				command.SetOut(&stdout)
				command.SetErr(&stderr)
				receipt := filepath.Join(t.TempDir(), "receipt.json")
				arguments := []string{"exec", "--artifact", cliTestDigest}
				if mode != "streams" {
					arguments = append(arguments, "--json")
				}
				if mode != "json" {
					arguments = append(arguments, "--inherit-streams", "--receipt-file", receipt)
				}
				var executionErr error
				var exitCode int
				func() {
					defer func() {
						if value := recover(); value != nil {
							if exit, ok := value.(common.ExitCode); ok {
								exitCode = exit.Code
							} else {
								panic(value)
							}
						}
					}()
					executionErr = runCobraCommand(command, append(arguments, "--", "python", "two words", "--child-flag"))
				}()
				if !errors.Is(executionErr, outcome.err) || (outcome.err == nil && exitCode != outcome.code) || (outcome.err != nil && exitCode != 0) {
					t.Fatalf("error=%v exit=%d", executionErr, exitCode)
				}
				var content []byte
				if mode == "json" {
					if jsonCalls != 1 || streamCalls != 0 {
						t.Fatalf("executor calls: JSON=%d streams=%d", jsonCalls, streamCalls)
					}
					if outcome.err != nil {
						if stdout.Len() != 0 {
							t.Fatalf("JSON emitted on execution error: %q", stdout.String())
						}
						return
					}
					content = stdout.Bytes()
				} else {
					if jsonCalls != 0 || streamCalls != 1 {
						t.Fatalf("executor calls: JSON=%d streams=%d", jsonCalls, streamCalls)
					}
					if stdout.String() != "child-output" || stderr.String() != "child-error" {
						t.Fatal("receipt polluted child streams")
					}
					var err error
					content, err = os.ReadFile(receipt)
					if err != nil {
						t.Fatal(err)
					}
				}
				var result environmentExecResult
				if err := json.Unmarshal(content, &result); err != nil {
					t.Fatal(err)
				}
				if result.Status != outcome.status || result.ExitCode != outcome.code || result.LeaseID != "lease" || result.Verification == nil || *result.Verification != leasedVerification {
					t.Fatalf("execution receipt = %+v", result)
				}
			})
		}
	}
}

func TestEnvironmentExecReceiptFailurePrecedesExecutionFailure(t *testing.T) {
	for _, executionErr := range []error{nil, context.Canceled} {
		t.Run(fmt.Sprint(executionErr), func(t *testing.T) {
			digest, _ := environmentartifact.ParseDigest(cliTestDigest)
			receiptErr := errors.New("receipt replacement failed")
			previous := replaceReceipt
			replaceReceipt = func(string, string) error { return receiptErr }
			t.Cleanup(func() { replaceReceipt = previous })
			receipt := filepath.Join(t.TempDir(), "receipt.json")
			if err := os.WriteFile(receipt, []byte("previous receipt"), 0o600); err != nil {
				t.Fatal(err)
			}
			command := newEnvironmentCommand(environmentCommandDependencies{
				acquire: func(context.Context, environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
					return environmentlifecycle.AcquireResult{ArtifactDigest: digest}, nil
				},
				materializer: func() environmentlifecycle.Materializer { return &exitMaterializer{} },
				executeWithStreams: func(context.Context, environmentlifecycle.Materializer, environmentlifecycle.Materialization, []string, io.Reader, io.Writer, io.Writer) (environmentlifecycle.ExecutionHandle, environmentlifecycle.ChildResult, error) {
					return environmentlifecycle.ExecutionHandle{}, environmentlifecycle.ChildResult{ExitCode: 7}, executionErr
				},
			})
			err := runCobraCommand(command, []string{"exec", "--artifact", cliTestDigest, "--inherit-streams", "--receipt-file", receipt, "--", "python"})
			if !errors.Is(err, receiptErr) {
				t.Fatalf("receipt error did not win: %v", err)
			}
			if content, err := os.ReadFile(receipt); err != nil || string(content) != "previous receipt" {
				t.Fatalf("previous receipt changed: %q, %v", content, err)
			}
		})
	}
}
