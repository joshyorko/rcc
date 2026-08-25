package environmentlifecycle

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

func TestSubprocessCrashRestartMatrixCoversEveryLifecycleHook(t *testing.T) {
	remote, remoteRoot, digest := publishCrashArtifact(t)
	expectations := map[CrashPoint]crashStateExpectation{
		CrashBeforeVerified:      {provisional: 0, ready: false, repairable: true},
		CrashAfterVerified:       {provisional: 1, ready: false, repairable: true},
		CrashBeforeMaterializing: {provisional: 1, ready: false, repairable: true},
		CrashAfterMaterializing:  {provisional: 2, ready: false, repairable: true},
		CrashBeforeReady:         {provisional: 2, ready: false, repairable: true},
		CrashAfterReady:          {provisional: 2, ready: true},
		CrashBeforeLease:         {provisional: 2, ready: true},
		CrashAfterLease:          {provisional: 2, ready: true, staleLease: true},
		CrashBeforeRelease:       {provisional: 2, ready: true, staleLease: true},
		CrashAfterRelease:        {provisional: 2, ready: true},
		CrashBeforeGC:            {provisional: 2, ready: true},
		CrashAfterGC:             {provisional: 0, ready: false, repairable: true},
	}
	for _, point := range CrashPoints() {
		expectation := expectations[point]
		t.Run(string(point), func(t *testing.T) {
			home := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestLifecycleCrashChild", "--")
			cmd.Env = append(os.Environ(),
				"RCC_LIFECYCLE_CHILD=crash", "RCC_LIFECYCLE_HOME="+home,
				"RCC_LIFECYCLE_DIGEST="+digest.Hex(), "RCC_LIFECYCLE_REMOTE="+remoteRoot,
				"RCC_LIFECYCLE_POINT="+string(point), "RCC_LIFECYCLE_PROOF="+filepath.Join(home, "hook-proof"))
			assertSubprocessRunExit(t, cmd, ctx, 42)
			proof, err := os.ReadFile(filepath.Join(home, "hook-proof"))
			if err != nil || string(proof) != string(point) {
				t.Fatalf("hook proof = %q, err=%v", proof, err)
			}
			common.Product.ForceHome(home)
			common.SharedHolotree = false
			assertCrashState(t, digest, remote, expectation)
		})
	}
}

type crashStateExpectation struct {
	provisional int
	ready       bool
	repairable  bool
	staleLease  bool
}

func publishCrashArtifact(t *testing.T) (*artifactprovider.Filesystem, string, environmentartifact.Digest) {
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
	return remote, remoteRoot, result.ArtifactDigest
}

func assertSubprocessRunExit(t *testing.T, cmd *exec.Cmd, ctx context.Context, want int) {
	t.Helper()
	err := cmd.Run()
	assertSubprocessError(t, err, ctx, want)
}

func assertSubprocessExit(t *testing.T, cmd *exec.Cmd, ctx context.Context, want int) {
	t.Helper()
	err := cmd.Wait()
	assertSubprocessError(t, err, ctx, want)
}

func assertSubprocessError(t *testing.T, err error, ctx context.Context, want int) {
	t.Helper()
	if ctx.Err() != nil {
		t.Fatalf("subprocess timed out: %v", ctx.Err())
	}
	if err == nil {
		t.Fatalf("subprocess exited cleanly, want exit code %d", want)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != want {
		t.Fatalf("subprocess exit = %v, want exit code %d", err, want)
	}
}

func assertCrashState(t *testing.T, digest environmentartifact.Digest, remote artifactprovider.Provider, want crashStateExpectation) {
	t.Helper()
	reconciled, err := Reconcile(context.Background(), digest)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Provisional != want.provisional || reconciled.ProvisionalRemoved != want.provisional {
		t.Fatalf("provisional reconciliation = %+v, want count %d", reconciled, want.provisional)
	}
	wantStale := 0
	if want.staleLease {
		wantStale = 1
	}
	if reconciled.Stale != wantStale || reconciled.Active != 0 || reconciled.Ambiguous != 0 {
		t.Fatalf("lease reconciliation = %+v, want stale=%d and no active/ambiguous leases", reconciled, wantStale)
	}
	if len(reconciled.Repaired) != wantStale {
		t.Fatalf("reconciled lease repairs = %v, want %d", reconciled.Repaired, wantStale)
	}
	if want.ready {
		assertVerifiedReady(t, digest)
		return
	}
	if !want.repairable {
		t.Fatalf("crash state is neither ready nor declared repairable")
	}
	assertRepairableState(t, digest)
	assertProviderRepair(t, digest, remote)
}

func TestIndependentProcessLifecycleRacesUseBarriers(t *testing.T) {
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
	scenarios := [][2]string{{"acquire", "acquire"}, {"acquire", "repair"}, {"repair", "gc"}, {"gc", "acquire"}}
	for _, scenario := range scenarios {
		t.Run(scenario[0]+"-"+scenario[1], func(t *testing.T) {
			home := t.TempDir()
			common.Product.ForceHome(home)
			common.SharedHolotree = false
			if scenario[0] != "acquire" || scenario[1] != "acquire" {
				if _, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: result.ArtifactDigest, Provider: remote}); err != nil {
					t.Fatal(err)
				}
			}
			barrier := filepath.Join(home, "barrier")
			commands := make([]*exec.Cmd, 2)
			for i, operation := range scenario {
				ready := filepath.Join(home, "ready-"+string(rune('0'+i)))
				cmd := exec.Command(os.Args[0], "-test.run=TestLifecycleRaceChild", "--")
				cmd.Env = append(os.Environ(), "RCC_LIFECYCLE_CHILD=race", "RCC_LIFECYCLE_HOME="+home, "RCC_LIFECYCLE_DIGEST="+result.ArtifactDigest.Hex(), "RCC_LIFECYCLE_REMOTE="+remoteRoot, "RCC_LIFECYCLE_OPERATION="+operation, "RCC_LIFECYCLE_BARRIER="+barrier, "RCC_LIFECYCLE_READY="+ready)
				commands[i] = cmd
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
			}
			for i := range commands {
				waitForFile(t, filepath.Join(home, "ready-"+string(rune('0'+i))))
			}
			if err := os.WriteFile(barrier, []byte("go"), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, cmd := range commands {
				if err := cmd.Wait(); err != nil {
					t.Fatalf("%s race child: %v", scenario, err)
				}
			}
		})
	}
}

func TestAcquireAndContentGCRaceSharesGlobalContentTransaction(t *testing.T) {
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
	home := t.TempDir()
	barrier := filepath.Join(home, "barrier")
	held := filepath.Join(home, "content-held")
	release := filepath.Join(home, "content-release")
	acquire := exec.Command(os.Args[0], "-test.run=TestLifecycleRaceChild", "--")
	acquire.Env = append(os.Environ(), "RCC_LIFECYCLE_CHILD=race", "RCC_LIFECYCLE_HOME="+home, "RCC_LIFECYCLE_DIGEST="+result.ArtifactDigest.Hex(), "RCC_LIFECYCLE_REMOTE="+remoteRoot, "RCC_LIFECYCLE_OPERATION=acquire", "RCC_LIFECYCLE_BARRIER="+barrier, "RCC_LIFECYCLE_READY="+filepath.Join(home, "acquire-ready"), "RCC_LIFECYCLE_CONTENT_HELD="+held, "RCC_LIFECYCLE_CONTENT_RELEASE="+release)
	if err := acquire.Start(); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, filepath.Join(home, "acquire-ready"))
	if err := os.WriteFile(barrier, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, held)
	done := filepath.Join(home, "gc-done")
	gc := exec.Command(os.Args[0], "-test.run=TestLifecycleRaceChild", "--")
	gc.Env = append(os.Environ(), "RCC_LIFECYCLE_CHILD=race", "RCC_LIFECYCLE_HOME="+home, "RCC_LIFECYCLE_DIGEST="+result.ArtifactDigest.Hex(), "RCC_LIFECYCLE_REMOTE="+remoteRoot, "RCC_LIFECYCLE_OPERATION=gc", "RCC_LIFECYCLE_BARRIER="+barrier, "RCC_LIFECYCLE_READY="+filepath.Join(home, "gc-ready"), "RCC_LIFECYCLE_DONE="+done, "RCC_LIFECYCLE_PRESSURE=1")
	if err := gc.Start(); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, filepath.Join(home, "gc-ready"))
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(done); !os.IsNotExist(err) {
		t.Fatalf("content GC completed while acquire held lock: %v", err)
	}
	if err := os.WriteFile(release, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := acquire.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := gc.Wait(); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, done)
}

func TestSubprocessENOSPCFailureMatrixLeavesExplicitlyRepairableState(t *testing.T) {
	cases := []struct {
		name         string
		mode         string
		providerGone bool
		repairable   bool
		errorText    string
	}{
		{name: "provider-disappears", mode: "provider-disappears", providerGone: true, errorText: "resolve manifest"},
		{name: "local-CAS-ENOSPC", mode: "local-CAS-ENOSPC", repairable: true, errorText: "local cache write object"},
		{name: "catalog-import", mode: "catalog-import", repairable: true, errorText: "register consumer legacy catalog"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			remote, remoteRoot, digest := publishCrashArtifact(t)
			home := t.TempDir()
			failureReady := filepath.Join(home, "failure-ready")
			failureBarrier := filepath.Join(home, "failure-barrier")
			failureProof := filepath.Join(home, "failure-proof")
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestLifecycleFailureChild", "--")
			cmd.Env = append(os.Environ(),
				"RCC_LIFECYCLE_CHILD=failure", "RCC_LIFECYCLE_HOME="+home,
				"RCC_LIFECYCLE_DIGEST="+digest.Hex(), "RCC_LIFECYCLE_REMOTE="+remoteRoot,
				"RCC_LIFECYCLE_FAILURE="+tc.mode, "RCC_LIFECYCLE_READY="+failureReady,
				"RCC_LIFECYCLE_BARRIER="+failureBarrier, "RCC_LIFECYCLE_PROOF="+failureProof)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			})
			if tc.providerGone {
				waitForFile(t, failureReady)
				if err := os.RemoveAll(remoteRoot); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(failureBarrier, []byte("go"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			assertSubprocessExit(t, cmd, ctx, 43)
			proof, err := os.ReadFile(failureProof)
			if err != nil || !strings.Contains(string(proof), tc.errorText) {
				t.Fatalf("failure proof = %q, err=%v, want %q", proof, err, tc.errorText)
			}
			common.Product.ForceHome(home)
			common.SharedHolotree = false
			reconciled, err := Reconcile(context.Background(), digest)
			if err != nil {
				t.Fatal(err)
			}
			if reconciled.Provisional != 0 || reconciled.ProvisionalRemoved != 0 {
				t.Fatalf("failure reconciliation = %+v, want no provisional state", reconciled)
			}
			assertRepairableState(t, digest)
			if tc.repairable {
				if tc.mode == "catalog-import" {
					if err := os.RemoveAll(common.HololibCatalogLocation()); err != nil {
						t.Fatal(err)
					}
					if err := os.MkdirAll(common.HololibCatalogLocation(), 0o700); err != nil {
						t.Fatal(err)
					}
				}
				assertProviderRepair(t, digest, remote)
				return
			}
			if _, err := RepairFromProvider(context.Background(), digest, remote); !errors.Is(err, ErrProviderUnavailable) {
				t.Fatalf("provider-disappearance repair error = %v, want ErrProviderUnavailable", err)
			}
		})
	}
}

func assertRepairableState(t *testing.T, digest environmentartifact.Digest) {
	t.Helper()
	inspection, err := Inspect(context.Background(), digest)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Ready || inspection.State != "absent" || inspection.Corrupt || inspection.Lease.Active != 0 || inspection.Lease.Stale != 0 || inspection.Lease.Ambiguous != 0 {
		t.Fatalf("incomplete lifecycle state = %+v", inspection)
	}
	if _, err := Verify(context.Background(), digest); !errors.Is(err, ErrMaterializationCorrupt) {
		t.Fatalf("incomplete lifecycle verification error = %v, want ErrMaterializationCorrupt", err)
	}
}

func assertProviderRepair(t *testing.T, digest environmentartifact.Digest, remote artifactprovider.Provider) {
	t.Helper()
	repaired, err := RepairFromProvider(context.Background(), digest, remote)
	if err != nil {
		t.Fatal(err)
	}
	if !repaired.Repaired || !repaired.Verification.Verified || repaired.Verification.State != string(stateReady) {
		t.Fatalf("provider repair report = %+v", repaired)
	}
	assertVerifiedReady(t, digest)
}

func assertVerifiedReady(t *testing.T, digest environmentartifact.Digest) {
	t.Helper()
	inspection, err := Inspect(context.Background(), digest)
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.Ready || inspection.State != string(stateReady) || inspection.Corrupt || inspection.Lease.Active != 0 || inspection.Lease.Stale != 0 || inspection.Lease.Ambiguous != 0 {
		t.Fatalf("verified-ready lifecycle state = %+v", inspection)
	}
	verification, err := Verify(context.Background(), digest)
	if err != nil || !verification.Verified || verification.State != string(stateReady) {
		t.Fatalf("ready verification = %+v, err=%v", verification, err)
	}
	warm, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: digest, Provider: failOnTouchProvider{t: t}})
	if err != nil {
		t.Fatal(err)
	}
	if warm.CacheHit != CacheLocalMaterialization || warm.ArtifactDigest != digest {
		t.Fatalf("warm ready acquisition = %+v", warm)
	}
}

func TestLifecycleCrashChild(t *testing.T) {
	if os.Getenv("RCC_LIFECYCLE_CHILD") != "crash" {
		return
	}
	previousVerifier := verifyMaterializedCompatibility
	verifyMaterializedCompatibility = func(context.Context, string, environmentartifact.CompatibilityRequirements) error { return nil }
	defer func() { verifyMaterializedCompatibility = previousVerifier }()
	common.Product.ForceHome(os.Getenv("RCC_LIFECYCLE_HOME"))
	common.SharedHolotree = false
	digest, err := environmentartifact.ParseDigest("sha256:" + os.Getenv("RCC_LIFECYCLE_DIGEST"))
	if err != nil {
		t.Fatal(err)
	}
	point := CrashPoint(os.Getenv("RCC_LIFECYCLE_POINT"))
	SetCrashHook(func(got CrashPoint) error {
		if got == point {
			if proof := os.Getenv("RCC_LIFECYCLE_PROOF"); proof != "" {
				_ = os.WriteFile(proof, []byte(got), 0o600)
			}
			return os.ErrProcessDone
		}
		return nil
	})
	defer SetCrashHook(nil)
	remote, err := artifactprovider.NewFilesystem(os.Getenv("RCC_LIFECYCLE_REMOTE"))
	if err != nil {
		t.Fatal(err)
	}
	switch point {
	case CrashBeforeVerified, CrashAfterVerified, CrashBeforeMaterializing, CrashAfterMaterializing, CrashBeforeReady, CrashAfterReady:
		_, err = NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: digest, Provider: remote})
	case CrashBeforeLease, CrashAfterLease, CrashBeforeRelease, CrashAfterRelease:
		materialization, acquireErr := NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: digest, Provider: remote})
		if acquireErr == nil {
			lease, leaseErr := NewLocalMaterializer().Lease(context.Background(), Materialization{ArtifactDigest: materialization.ArtifactDigest, ID: materialization.MaterializationID, Path: materialization.Path})
			err = leaseErr
			if err == nil && (point == CrashBeforeRelease || point == CrashAfterRelease) {
				err = NewLocalMaterializer().Release(context.Background(), lease)
			}
		}
	case CrashBeforeGC, CrashAfterGC:
		_, err = NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: digest, Provider: remote})
		if err == nil {
			_, err = Collect(context.Background(), GCPolicy{})
		}
	}
	if err == nil {
		t.Fatal("lifecycle operation unexpectedly succeeded")
	}
	os.Exit(42)
}

func TestLifecycleFailureChild(t *testing.T) {
	if os.Getenv("RCC_LIFECYCLE_CHILD") != "failure" {
		return
	}
	common.Product.ForceHome(os.Getenv("RCC_LIFECYCLE_HOME"))
	common.SharedHolotree = false
	digest, err := environmentartifact.ParseDigest("sha256:" + os.Getenv("RCC_LIFECYCLE_DIGEST"))
	if err != nil {
		t.Fatal(err)
	}
	remote, err := artifactprovider.NewFilesystem(os.Getenv("RCC_LIFECYCLE_REMOTE"))
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("RCC_LIFECYCLE_FAILURE") == "provider-disappears" {
		if err := os.WriteFile(os.Getenv("RCC_LIFECYCLE_READY"), []byte("ready"), 0o600); err != nil {
			t.Fatal(err)
		}
		waitForFile(t, os.Getenv("RCC_LIFECYCLE_BARRIER"))
	}
	var operationErr error
	switch os.Getenv("RCC_LIFECYCLE_FAILURE") {
	case "provider-disappears":
		_, operationErr = NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: digest, Provider: remote})
	case "local-CAS-ENOSPC":
		local, localErr := newLocalContentProvider()
		if localErr != nil {
			t.Fatal(localErr)
		}
		acquirer := NewAcquirer()
		acquirer.localProviderFactory = func() (artifactprovider.Provider, error) {
			return &localCacheENOSPCProvider{Provider: local, failPut: true}, nil
		}
		_, operationErr = acquirer.Acquire(context.Background(), AcquireRequest{ArtifactDigest: digest, Provider: remote})
		if !errors.Is(operationErr, syscall.ENOSPC) {
			t.Fatalf("local CAS failure = %v, want ENOSPC", operationErr)
		}
	case "catalog-import":
		manifestBytes, resolveErr := remote.ResolveManifest(context.Background(), digest)
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		manifest, decodeErr := environmentartifact.DecodeManifest(manifestBytes)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if len(manifest.Catalogs) == 0 || manifest.Catalogs[0].LegacyName == "" {
			t.Fatal("resolved manifest has no legacy catalog")
		}
		if err := os.MkdirAll(common.HololibCatalogLocation(), 0o700); err != nil {
			t.Fatal(err)
		}
		blocked := filepath.Join(common.HololibCatalogLocation(), manifest.Catalogs[0].LegacyName+".info")
		if err := os.Mkdir(blocked, 0o700); err != nil {
			t.Fatal(err)
		}
		_, operationErr = acquireVerifiedContent(context.Background(), digest, remote)
	default:
		t.Fatal("unknown lifecycle failure")
	}
	if operationErr == nil {
		t.Fatal("failure operation unexpectedly succeeded")
	}
	if proof := os.Getenv("RCC_LIFECYCLE_PROOF"); proof != "" {
		if err := os.WriteFile(proof, []byte(operationErr.Error()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	os.Exit(43)
}

func TestLifecycleRaceChild(t *testing.T) {
	if os.Getenv("RCC_LIFECYCLE_CHILD") != "race" {
		return
	}
	previousVerifier := verifyMaterializedCompatibility
	verifyMaterializedCompatibility = func(context.Context, string, environmentartifact.CompatibilityRequirements) error { return nil }
	defer func() { verifyMaterializedCompatibility = previousVerifier }()
	common.Product.ForceHome(os.Getenv("RCC_LIFECYCLE_HOME"))
	common.SharedHolotree = false
	digest, err := environmentartifact.ParseDigest("sha256:" + os.Getenv("RCC_LIFECYCLE_DIGEST"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("RCC_LIFECYCLE_READY"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	if held := os.Getenv("RCC_LIFECYCLE_CONTENT_HELD"); held != "" {
		contentTransactionProbe = func() {
			_ = os.WriteFile(held, []byte("held"), 0o600)
			for {
				if _, err := os.Stat(os.Getenv("RCC_LIFECYCLE_CONTENT_RELEASE")); err == nil {
					return
				}
				time.Sleep(time.Millisecond)
			}
		}
		defer func() { contentTransactionProbe = nil }()
	}
	for {
		if _, err := os.Stat(os.Getenv("RCC_LIFECYCLE_BARRIER")); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	remote, err := artifactprovider.NewFilesystem(os.Getenv("RCC_LIFECYCLE_REMOTE"))
	if err != nil {
		t.Fatal(err)
	}
	switch os.Getenv("RCC_LIFECYCLE_OPERATION") {
	case "acquire":
		_, err = NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: digest, Provider: remote})
	case "repair":
		_, err = RepairFromProvider(context.Background(), digest, remote)
	case "gc":
		_, err = Collect(context.Background(), GCPolicy{Pressure: os.Getenv("RCC_LIFECYCLE_PRESSURE") == "1"})
	default:
		t.Fatal("unknown lifecycle race operation")
	}
	if err != nil {
		t.Fatal(err)
	}
	if done := os.Getenv("RCC_LIFECYCLE_DONE"); done != "" {
		_ = os.WriteFile(done, []byte("done"), 0o600)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(time.Millisecond)
	}
}
