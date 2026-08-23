package environmentlifecycle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

func TestSubprocessCrashRestartMatrixCoversEveryLifecycleHook(t *testing.T) {
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
	digest := result.ArtifactDigest
	for _, point := range CrashPoints() {
		t.Run(string(point), func(t *testing.T) {
			home := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=TestLifecycleCrashChild", "--")
			cmd.Env = append(os.Environ(),
				"RCC_LIFECYCLE_CHILD=crash", "RCC_LIFECYCLE_HOME="+home,
				"RCC_LIFECYCLE_DIGEST="+digest.Hex(), "RCC_LIFECYCLE_REMOTE="+remoteRoot,
				"RCC_LIFECYCLE_POINT="+string(point), "RCC_LIFECYCLE_PROOF="+filepath.Join(home, "hook-proof"))
			if err := cmd.Run(); err == nil {
				t.Fatalf("crash point %s did not terminate the subprocess", point)
			}
			proof, err := os.ReadFile(filepath.Join(home, "hook-proof"))
			if err != nil || string(proof) != string(point) {
				t.Fatalf("hook proof = %q, err=%v", proof, err)
			}
			common.Product.ForceHome(home)
			common.SharedHolotree = false
			if _, err := Reconcile(context.Background(), digest); err != nil {
				t.Fatalf("restart reconciliation after %s: %v", point, err)
			}
		})
	}
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

func TestLifecycleCrashChild(t *testing.T) {
	if os.Getenv("RCC_LIFECYCLE_CHILD") != "crash" {
		return
	}
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

func TestLifecycleRaceChild(t *testing.T) {
	if os.Getenv("RCC_LIFECYCLE_CHILD") != "race" {
		return
	}
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
