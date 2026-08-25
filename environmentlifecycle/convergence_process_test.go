package environmentlifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

const convergenceChildEnv = "RCC_CONVERGENCE_CHILD"

type convergenceReceipt struct {
	Operation          string                     `json:"operation"`
	ArtifactDigest     environmentartifact.Digest `json:"artifactDigest"`
	MaterializationID  string                     `json:"materializationId,omitempty"`
	Path               string                     `json:"path,omitempty"`
	CacheHit           CacheProvenance            `json:"cacheHit,omitempty"`
	State              string                     `json:"state,omitempty"`
	Ready              bool                       `json:"ready,omitempty"`
	Verified           bool                       `json:"verified,omitempty"`
	VerifyFailed       bool                       `json:"verifyFailed,omitempty"`
	ProvisionalRemoved int                        `json:"provisionalRemoved,omitempty"`
	Reclaimed          int                        `json:"reclaimed,omitempty"`
}

func TestIndependentProcessConvergence(t *testing.T) {
	digest, remoteRoot := publishConvergenceArtifact(t)
	home := t.TempDir()
	barrier := filepath.Join(home, "start")
	commands := []*exec.Cmd{
		convergenceChildCommand(home, digest, remoteRoot, "acquire", map[string]string{
			"RCC_CONVERGENCE_READY":  filepath.Join(home, "acquire-1-ready"),
			"RCC_CONVERGENCE_STATUS": filepath.Join(home, "acquire-1-status"),
		}),
		convergenceChildCommand(home, digest, remoteRoot, "acquire", map[string]string{
			"RCC_CONVERGENCE_READY":  filepath.Join(home, "acquire-2-ready"),
			"RCC_CONVERGENCE_STATUS": filepath.Join(home, "acquire-2-status"),
		}),
	}
	startConvergenceChildren(t, commands)
	waitForConvergenceFile(t, filepath.Join(home, "acquire-1-ready"))
	waitForConvergenceFile(t, filepath.Join(home, "acquire-2-ready"))
	writeConvergenceSignal(t, barrier)
	for _, command := range commands {
		waitForConvergenceChild(t, command)
	}

	first := readConvergenceReceipt(t, filepath.Join(home, "acquire-1-status"))
	second := readConvergenceReceipt(t, filepath.Join(home, "acquire-2-status"))
	if first.ArtifactDigest != digest || second.ArtifactDigest != digest {
		t.Fatalf("acquire identities = %s and %s, want %s", first.ArtifactDigest, second.ArtifactDigest, digest)
	}
	if first.MaterializationID == "" || first.MaterializationID != second.MaterializationID || first.Path != second.Path {
		t.Fatalf("acquires did not converge: first=%+v second=%+v", first, second)
	}
	if first.CacheHit == "" || second.CacheHit == "" {
		t.Fatalf("acquires did not return cache provenance: first=%+v second=%+v", first, second)
	}
	assertConvergedLocalState(t, home, remoteRoot, digest)
}

func TestIndependentProcessUnrelatedArtifacts(t *testing.T) {
	firstDigest, secondDigest, _ := publishTwoConvergenceArtifacts(t)
	home := t.TempDir()
	release := filepath.Join(home, "release")
	first := convergenceChildCommand(home, firstDigest, "", "lock", map[string]string{
		"RCC_CONVERGENCE_ROLE":    "holder",
		"RCC_CONVERGENCE_HELD":    filepath.Join(home, "holder-held"),
		"RCC_CONVERGENCE_RELEASE": release,
		"RCC_CONVERGENCE_READY":   filepath.Join(home, "holder-ready"),
		"RCC_CONVERGENCE_STATUS":  filepath.Join(home, "holder-status"),
	})
	second := convergenceChildCommand(home, secondDigest, "", "lock", map[string]string{
		"RCC_CONVERGENCE_ROLE":     "contender",
		"RCC_CONVERGENCE_STARTED":  filepath.Join(home, "contender-started"),
		"RCC_CONVERGENCE_ACQUIRED": filepath.Join(home, "contender-acquired"),
		"RCC_CONVERGENCE_READY":    filepath.Join(home, "contender-ready"),
		"RCC_CONVERGENCE_STATUS":   filepath.Join(home, "contender-status"),
	})
	startConvergenceChildren(t, []*exec.Cmd{first})
	waitForConvergenceFile(t, filepath.Join(home, "holder-ready"))
	writeConvergenceSignal(t, filepath.Join(home, "start"))
	waitForConvergenceFile(t, filepath.Join(home, "holder-held"))
	startConvergenceChildren(t, []*exec.Cmd{second})
	waitForConvergenceFile(t, filepath.Join(home, "contender-started"))
	if !convergenceFileWithin(filepath.Join(home, "contender-acquired"), 2*time.Second) {
		t.Fatal("unrelated artifact lock remained blocked by another artifact")
	}
	writeConvergenceSignal(t, release)
	waitForConvergenceChild(t, first)
	waitForConvergenceChild(t, second)
	if got := readConvergenceReceipt(t, filepath.Join(home, "holder-status")); got.ArtifactDigest != firstDigest {
		t.Fatalf("holder digest = %s, want %s", got.ArtifactDigest, firstDigest)
	}
	if got := readConvergenceReceipt(t, filepath.Join(home, "contender-status")); got.ArtifactDigest != secondDigest {
		t.Fatalf("contender digest = %s, want %s", got.ArtifactDigest, secondDigest)
	}
}

func TestIndependentProcessRepairAcquire(t *testing.T) {
	digest, remoteRoot := publishConvergenceArtifact(t)
	home := t.TempDir()
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(home)
	common.SharedHolotree = false
	t.Cleanup(func() { common.Product.ForceHome(previousHome); common.SharedHolotree = previousShared })
	writeMaterializationRecordForConvergence(t, digest, stateVerifiedContent)
	writeMaterializationRecordForConvergence(t, digest, stateMaterializing)
	barrier := filepath.Join(home, "start")
	commands := []*exec.Cmd{
		convergenceChildCommand(home, digest, remoteRoot, "repair", map[string]string{
			"RCC_CONVERGENCE_READY":  filepath.Join(home, "repair-ready"),
			"RCC_CONVERGENCE_STATUS": filepath.Join(home, "repair-status"),
		}),
		convergenceChildCommand(home, digest, remoteRoot, "acquire", map[string]string{
			"RCC_CONVERGENCE_READY":  filepath.Join(home, "acquire-ready"),
			"RCC_CONVERGENCE_STATUS": filepath.Join(home, "acquire-status"),
		}),
	}
	startConvergenceChildren(t, commands)
	waitForConvergenceFile(t, filepath.Join(home, "repair-ready"))
	waitForConvergenceFile(t, filepath.Join(home, "acquire-ready"))
	writeConvergenceSignal(t, barrier)
	for _, command := range commands {
		waitForConvergenceChild(t, command)
	}
	if got := readConvergenceReceipt(t, filepath.Join(home, "repair-status")); got.ArtifactDigest != digest {
		t.Fatalf("repair digest = %s, want %s", got.ArtifactDigest, digest)
	}
	if got := readConvergenceReceipt(t, filepath.Join(home, "repair-status")); !got.Ready || !got.Verified {
		t.Fatalf("repair did not report verified ready state: %+v", got)
	}
	if got := readConvergenceReceipt(t, filepath.Join(home, "acquire-status")); got.ArtifactDigest != digest {
		t.Fatalf("acquire digest = %s, want %s", got.ArtifactDigest, digest)
	}
	assertConvergedLocalState(t, home, remoteRoot, digest)
	if _, err := os.Stat(filepath.Join(recordRoot(), digest.Hex(), string(stateVerifiedContent)+".json")); !os.IsNotExist(err) {
		t.Fatalf("verified-content provisional record remained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(recordRoot(), digest.Hex(), string(stateMaterializing)+".json")); !os.IsNotExist(err) {
		t.Fatalf("materializing provisional record remained: %v", err)
	}
}

func TestIndependentProcessStaleTemporary(t *testing.T) {
	digest, _ := publishConvergenceArtifact(t)
	home := t.TempDir()
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(home)
	common.SharedHolotree = false
	t.Cleanup(func() { common.Product.ForceHome(previousHome); common.SharedHolotree = previousShared })
	writeMaterializationRecordForConvergence(t, digest, stateVerifiedContent)
	writeMaterializationRecordForConvergence(t, digest, stateMaterializing)
	temporary := filepath.Join(home, "artifacts", "v1", "content", "objects", "sha256", "aa", "bb", ".upload-stale")
	if err := os.MkdirAll(filepath.Dir(temporary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temporary, []byte("partial provisional content"), 0o600); err != nil {
		t.Fatal(err)
	}

	ready := filepath.Join(home, "reconcile-ready")
	status := filepath.Join(home, "reconcile-status")
	command := convergenceChildCommand(home, digest, "", "reconcile", map[string]string{
		"RCC_CONVERGENCE_READY":  ready,
		"RCC_CONVERGENCE_STATUS": status,
	})
	startConvergenceChildren(t, []*exec.Cmd{command})
	waitForConvergenceFile(t, ready)
	writeConvergenceSignal(t, filepath.Join(home, "start"))
	waitForConvergenceChild(t, command)
	receipt := readConvergenceReceipt(t, status)
	if receipt.ProvisionalRemoved != 2 || receipt.Ready || receipt.State != "absent" || !receipt.VerifyFailed {
		t.Fatalf("stale provisional receipt = %+v", receipt)
	}
	if _, err := os.Stat(temporary); err != nil {
		t.Fatalf("stale temporary content disappeared unexpectedly: %v", err)
	}
	if _, err := readReadyRecord(digest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale provisional content became ready: %v", err)
	}
}

func TestIndependentProcessGCFinalization(t *testing.T) {
	digest, remoteRoot := publishConvergenceArtifact(t)
	home := t.TempDir()
	contentRelease := filepath.Join(home, "content-release")
	acquire := convergenceChildCommand(home, digest, remoteRoot, "acquire-finalize", map[string]string{
		"RCC_CONVERGENCE_READY":           filepath.Join(home, "acquire-ready"),
		"RCC_CONVERGENCE_STATUS":          filepath.Join(home, "acquire-status"),
		"RCC_CONVERGENCE_CONTENT_HELD":    filepath.Join(home, "content-held"),
		"RCC_CONVERGENCE_CONTENT_RELEASE": contentRelease,
	})
	gc := convergenceChildCommand(home, digest, "", "gc", map[string]string{
		"RCC_CONVERGENCE_READY":  filepath.Join(home, "gc-ready"),
		"RCC_CONVERGENCE_STATUS": filepath.Join(home, "gc-status"),
		"RCC_CONVERGENCE_DONE":   filepath.Join(home, "gc-done"),
	})
	startConvergenceChildren(t, []*exec.Cmd{acquire})
	waitForConvergenceFile(t, filepath.Join(home, "acquire-ready"))
	writeConvergenceSignal(t, filepath.Join(home, "start"))
	waitForConvergenceFile(t, filepath.Join(home, "content-held"))
	startConvergenceChildren(t, []*exec.Cmd{gc})
	waitForConvergenceFile(t, filepath.Join(home, "gc-ready"))
	writeConvergenceSignal(t, contentRelease)
	waitForConvergenceFile(t, filepath.Join(home, "gc-done"))
	assertConvergedLocalState(t, home, remoteRoot, digest)
	waitForConvergenceChild(t, acquire)
	waitForConvergenceChild(t, gc)
	if got := readConvergenceReceipt(t, filepath.Join(home, "gc-status")); got.Reclaimed != 0 {
		t.Fatalf("GC reclaimed newly finalized materialization: %+v", got)
	}
	assertConvergedLocalState(t, home, remoteRoot, digest)
}

func TestConvergenceProcessChild(t *testing.T) {
	if os.Getenv(convergenceChildEnv) != "1" {
		return
	}
	previousVerifier := verifyMaterializedCompatibility
	verifyMaterializedCompatibility = func(context.Context, string, environmentartifact.CompatibilityRequirements) error { return nil }
	defer func() { verifyMaterializedCompatibility = previousVerifier }()
	common.Product.ForceHome(os.Getenv("RCC_CONVERGENCE_HOME"))
	common.SharedHolotree = false
	digest, err := environmentartifact.ParseDigest("sha256:" + os.Getenv("RCC_CONVERGENCE_DIGEST"))
	if err != nil {
		t.Fatal(err)
	}
	if ready := os.Getenv("RCC_CONVERGENCE_READY"); ready != "" {
		writeConvergenceSignal(t, ready)
	}
	if start := os.Getenv("RCC_CONVERGENCE_START"); start != "" {
		waitForConvergenceFile(t, start)
	}
	if barrier := os.Getenv("RCC_CONVERGENCE_BARRIER"); barrier != "" {
		waitForConvergenceFile(t, barrier)
	}

	receipt := convergenceReceipt{Operation: os.Getenv("RCC_CONVERGENCE_OPERATION"), ArtifactDigest: digest}
	switch receipt.Operation {
	case "acquire":
		remote := convergenceProvider(t)
		result, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: digest, Provider: remote})
		if err != nil {
			t.Fatal(err)
		}
		receipt.MaterializationID, receipt.Path, receipt.CacheHit = result.MaterializationID, result.Path, result.CacheHit
	case "acquire-finalize":
		remote := convergenceProvider(t)
		contentTransactionProbe = func() {
			writeConvergenceSignal(t, os.Getenv("RCC_CONVERGENCE_CONTENT_HELD"))
			waitForConvergenceFile(t, os.Getenv("RCC_CONVERGENCE_CONTENT_RELEASE"))
		}
		result, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: digest, Provider: remote})
		contentTransactionProbe = nil
		if err != nil {
			t.Fatal(err)
		}
		receipt.MaterializationID, receipt.Path, receipt.CacheHit = result.MaterializationID, result.Path, result.CacheHit
	case "repair":
		remote := convergenceProvider(t)
		report, err := RepairFromProvider(context.Background(), digest, remote)
		if err != nil {
			t.Fatal(err)
		}
		receipt.Ready, receipt.Verified = report.Verification.State == string(stateReady), report.Verification.Verified
	case "lock":
		if os.Getenv("RCC_CONVERGENCE_ROLE") != "holder" {
			writeConvergenceSignal(t, os.Getenv("RCC_CONVERGENCE_STARTED"))
		}
		release, err := acquireCrossArtifactLock(digest)
		if err != nil {
			t.Fatal(err)
		}
		if os.Getenv("RCC_CONVERGENCE_ROLE") == "holder" {
			writeConvergenceSignal(t, os.Getenv("RCC_CONVERGENCE_HELD"))
			waitForConvergenceFile(t, os.Getenv("RCC_CONVERGENCE_RELEASE"))
		} else {
			writeConvergenceSignal(t, os.Getenv("RCC_CONVERGENCE_ACQUIRED"))
		}
		if err := release(); err != nil {
			t.Fatal(err)
		}
	case "reconcile":
		report, err := Reconcile(context.Background(), digest)
		if err != nil {
			t.Fatal(err)
		}
		inspection, err := Inspect(context.Background(), digest)
		if err != nil {
			t.Fatal(err)
		}
		_, verifyErr := Verify(context.Background(), digest)
		receipt.ProvisionalRemoved, receipt.Ready, receipt.State, receipt.VerifyFailed = report.ProvisionalRemoved, inspection.Ready, inspection.State, verifyErr != nil
	case "gc":
		report, err := Collect(context.Background(), GCPolicy{Pressure: true, Retention: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		receipt.Reclaimed = report.Reclaimed
		writeConvergenceSignal(t, os.Getenv("RCC_CONVERGENCE_DONE"))
	default:
		t.Fatalf("unknown convergence operation %q", receipt.Operation)
	}
	if status := os.Getenv("RCC_CONVERGENCE_STATUS"); status != "" {
		content, err := json.Marshal(receipt)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(status, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func publishConvergenceArtifact(t *testing.T) (environmentartifact.Digest, string) {
	t.Helper()
	fixture := newPublishFixture(t)
	remoteRoot := t.TempDir()
	remote, err := artifactprovider.NewFilesystem(remoteRoot)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Publish(context.Background(), PublishRequest{RobotFile: "robot.yaml", Provider: remote, Builder: &recordingBuilder{result: fixture.build}})
	if err != nil {
		t.Fatal(err)
	}
	return result.ArtifactDigest, remoteRoot
}

func publishTwoConvergenceArtifacts(t *testing.T) (environmentartifact.Digest, environmentartifact.Digest, string) {
	t.Helper()
	fixture := newPublishFixture(t)
	remoteRoot := t.TempDir()
	remote, err := artifactprovider.NewFilesystem(remoteRoot)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Publish(context.Background(), PublishRequest{RobotFile: "robot.yaml", Provider: remote, Builder: &recordingBuilder{result: fixture.build}})
	if err != nil {
		t.Fatal(err)
	}
	fixture.build.SpecificationBytes = []byte(`{"dependencies":["python=3.11"],"source":"other.yaml"}`)
	second, err := Publish(context.Background(), PublishRequest{RobotFile: "other.yaml", Provider: remote, Builder: &recordingBuilder{result: fixture.build}})
	if err != nil {
		t.Fatal(err)
	}
	if first.ArtifactDigest == second.ArtifactDigest {
		t.Fatal("test fixture did not produce unrelated artifact identities")
	}
	return first.ArtifactDigest, second.ArtifactDigest, remoteRoot
}

func convergenceChildCommand(home string, digest environmentartifact.Digest, remoteRoot, operation string, extra map[string]string) *exec.Cmd {
	env := []string{
		convergenceChildEnv + "=1",
		"RCC_CONVERGENCE_HOME=" + home,
		"RCC_CONVERGENCE_DIGEST=" + digest.Hex(),
		"RCC_CONVERGENCE_REMOTE=" + remoteRoot,
		"RCC_CONVERGENCE_OPERATION=" + operation,
		"RCC_CONVERGENCE_BARRIER=" + filepath.Join(home, "start"),
	}
	for key, value := range extra {
		env = append(env, key+"="+value)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestConvergenceProcessChild$", "--", "")
	command.Env = append(os.Environ(), env...)
	return command
}

func convergenceProvider(t *testing.T) artifactprovider.Provider {
	t.Helper()
	provider, err := artifactprovider.NewFilesystem(os.Getenv("RCC_CONVERGENCE_REMOTE"))
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func startConvergenceChildren(t *testing.T, commands []*exec.Cmd) {
	t.Helper()
	for _, command := range commands {
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if command.Process != nil && command.ProcessState == nil {
				_ = command.Process.Kill()
			}
		})
	}
}

func waitForConvergenceChild(t *testing.T, command *exec.Cmd) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			exitCode := -1
			if command.ProcessState != nil {
				exitCode = command.ProcessState.ExitCode()
			}
			t.Fatalf("child exited with status %d: %v", exitCode, err)
		}
		if command.ProcessState == nil || command.ProcessState.ExitCode() != 0 {
			t.Fatalf("child did not have exact zero exit status: %#v", command.ProcessState)
		}
	case <-time.After(15 * time.Second):
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		t.Fatal("timed out waiting for convergence child")
	}
}

func waitForConvergenceFile(t *testing.T, path string) {
	t.Helper()
	if !convergenceFileWithin(path, 15*time.Second) {
		t.Fatalf("timed out waiting for %s", path)
	}
}

func convergenceFileWithin(path string, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	ticker := time.NewTicker(2 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		select {
		case <-deadline.C:
			return false
		case <-ticker.C:
		}
	}
}

func writeConvergenceSignal(t *testing.T, path string) {
	t.Helper()
	if path == "" {
		t.Fatal("empty convergence signal path")
	}
	if err := os.WriteFile(path, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readConvergenceReceipt(t *testing.T, path string) convergenceReceipt {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt convergenceReceipt
	if err := json.Unmarshal(content, &receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}

func writeMaterializationRecordForConvergence(t *testing.T, digest environmentartifact.Digest, state materializationState) {
	t.Helper()
	if err := writeMaterializationRecord(materializationRecord{
		ArtifactDigest: digest, LegacyBlueprintKey: "stale-provisional", MaterializationID: "stale-provisional",
		Path: filepath.Join(common.HolotreeLocation(), "stale-provisional"), State: state,
		CreatedAt: time.Unix(1, 0).UTC(), VerifiedAt: time.Unix(1, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
}

func assertConvergedLocalState(t *testing.T, home, remoteRoot string, digest environmentartifact.Digest) {
	t.Helper()
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(home)
	common.SharedHolotree = false
	t.Cleanup(func() { common.Product.ForceHome(previousHome); common.SharedHolotree = previousShared })
	remote, err := artifactprovider.NewFilesystem(remoteRoot)
	if err != nil {
		t.Fatal(err)
	}
	wantManifest, err := remote.ResolveManifest(context.Background(), digest)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := environmentartifact.DecodeManifest(wantManifest)
	if err != nil {
		t.Fatal(err)
	}
	local, err := artifactprovider.NewFilesystem(localContentRoot())
	if err != nil {
		t.Fatal(err)
	}
	gotManifest, err := local.ResolveManifest(context.Background(), digest)
	if err != nil || !bytes.Equal(gotManifest, wantManifest) {
		t.Fatalf("local manifest mismatch: err=%v equal=%v", err, bytes.Equal(gotManifest, wantManifest))
	}
	indexContent, err := readProviderObject(context.Background(), local, manifest.ObjectIndex)
	if err != nil {
		t.Fatal(err)
	}
	index, err := environmentartifact.DecodeObjectIndex(indexContent)
	if err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range []environmentartifact.Descriptor{manifest.Specification.Descriptor, manifest.LegacyBlueprint.Descriptor, manifest.Catalogs[0].Descriptor, manifest.ObjectIndex} {
		if _, err := readProviderObject(context.Background(), local, descriptor); err != nil {
			t.Fatalf("local object %s is not verified: %v", descriptor.Digest, err)
		}
	}
	for _, entry := range index.Entries {
		descriptor := environmentartifact.Descriptor{MediaType: "application/vnd.rcc.hololib.object.v12+gzip", Digest: entry.StoredDigest, Size: entry.StoredSize}
		if _, err := readProviderObject(context.Background(), local, descriptor); err != nil {
			t.Fatalf("local legacy object %s is not verified: %v", descriptor.Digest, err)
		}
	}
	record, err := readReadyRecord(digest)
	if err != nil || record.ArtifactDigest != digest || record.State != stateReady {
		t.Fatalf("ready record = %+v, err=%v", record, err)
	}
	verification, err := Verify(context.Background(), digest)
	if err != nil || !verification.Verified {
		t.Fatalf("final verification = %+v, err=%v", verification, err)
	}
	if _, err := materializedPython(record.Path); err != nil {
		t.Fatalf("final materialization is not executable: %v", err)
	}
}
