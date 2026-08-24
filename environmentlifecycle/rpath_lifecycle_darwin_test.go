//go:build darwin

package environmentlifecycle

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/htfs"
)

func TestDarwinRPathArtifactRelocatesAndExecutesColdAndWarm(t *testing.T) {
	for _, tool := range []string{"clang", "install_name_tool", "otool"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is unavailable", tool)
		}
	}
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	producerHome := t.TempDir()
	common.Product.ForceHome(producerHome)
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
	})
	if err := os.MkdirAll(common.HololibCatalogLocation(), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(common.HololibLibraryLocation(), 0o750); err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(t.TempDir(), "h123456_123456789abcdeft")
	binDir := filepath.Join(source, "bin")
	libDir := filepath.Join(source, "lib")
	if err := os.MkdirAll(binDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(libDir, 0o750); err != nil {
		t.Fatal(err)
	}
	writeDarwinSource(t, filepath.Join(source, "dep.c"), "int dep(void) { return 42; }\n")
	writeDarwinSource(t, filepath.Join(source, "consumer.c"), "extern int dep(void); int consumer(void) { return dep(); }\n")
	writeDarwinSource(t, filepath.Join(source, "probe.c"), "extern int consumer(void); int main(void) { return consumer() == 42 ? 0 : 1; }\n")
	runDarwinTool(t, source, "clang", "-dynamiclib", "dep.c", "-install_name", "@rpath/libdep.dylib", "-o", "lib/libdep.dylib")
	runDarwinTool(t, source, "clang", "-dynamiclib", "consumer.c", "-Llib", "-ldep", "-install_name", "@rpath/libconsumer.dylib", "-Wl,-rpath,@loader_path", "-o", "lib/libconsumer.dylib")
	runDarwinTool(t, source, "install_name_tool", "-change", "@rpath/libdep.dylib", "@loader_path/libdep.dylib", "lib/libconsumer.dylib")
	runDarwinTool(t, source, "clang", "probe.c", "-Llib", "-lconsumer", "-Wl,-rpath,@executable_path/../lib", "-o", "bin/probe")
	if output, err := exec.Command(filepath.Join(binDir, "probe")).CombinedOutput(); err != nil {
		t.Fatalf("source rpath probe: %v: %s", err, output)
	}
	hostPython, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	pythonWrapper := []byte(fmt.Sprintf("#!/bin/sh\nexec %q \"$@\"\n", hostPython))
	if err := os.WriteFile(filepath.Join(binDir, "python"), pythonWrapper, 0o750); err != nil {
		t.Fatal(err)
	}

	legacyBlueprint := []byte("channels:\n- conda-forge\ndependencies:\n- python=3.11\n")
	root, err := htfs.NewRoot(source)
	if err != nil {
		t.Fatal(err)
	}
	root.Blueprint = common.BlueprintHash(legacyBlueprint)
	root.Tree.Mode = 0o750 | os.ModeDir
	root.Tree.Dirs["bin"] = &htfs.Dir{Name: "bin", Mode: 0o750 | os.ModeDir, Dirs: map[string]*htfs.Dir{}, Files: map[string]*htfs.File{}}
	root.Tree.Dirs["lib"] = &htfs.Dir{Name: "lib", Mode: 0o750 | os.ModeDir, Dirs: map[string]*htfs.Dir{}, Files: map[string]*htfs.File{}}
	for _, relative := range []string{"bin/python", "bin/probe", "lib/libconsumer.dylib", "lib/libdep.dylib"} {
		addDarwinCatalogFile(t, root, source, relative)
	}
	catalogPath := filepath.Join(common.HololibCatalogLocation(), htfs.CatalogName(root.Blueprint))
	if err := root.SaveAs(catalogPath); err != nil {
		t.Fatal(err)
	}
	platform := environmentartifact.CurrentPlatform()
	platform.RCCPlatform = common.Platform()
	compatibility, err := compatibilityForMaterialization(context.Background(), source, platform)
	if err != nil {
		t.Fatal(err)
	}
	builder := environmentartifact.Builder{Kind: "rcc-holotree-v12", RCCVersion: common.Version, CompatibilityKey: "v12-gzip-sha256"}
	specification, err := semanticSpecificationBytes("robot.yaml", legacyBlueprint, platform, builder, compatibility)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	published, err := Publish(context.Background(), PublishRequest{
		RobotFile: "robot.yaml",
		Provider:  provider,
		Builder: &recordingBuilder{result: BuildResult{
			LegacyBlueprint: legacyBlueprint, CatalogPath: catalogPath, SpecificationBytes: specification,
			SourceKind: "robot.yaml", Platform: platform, Builder: builder, Compatibility: compatibility,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	consumerHome := t.TempDir()
	common.Product.ForceHome(consumerHome)
	cold, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: published.ArtifactDigest, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	if cold.Path == source || !strings.HasPrefix(cold.Path, consumerHome) {
		t.Fatalf("cold materialization did not relocate: source=%s cold=%s", source, cold.Path)
	}
	materialization := Materialization{ArtifactDigest: cold.ArtifactDigest, ID: cold.MaterializationID, Path: cold.Path, CacheHit: cold.CacheHit, Verification: cold.Verification}
	handle, child, err := Execute(context.Background(), NewLocalMaterializer(), materialization, []string{filepath.Join(cold.Path, "bin", "probe")})
	if err != nil || child.ExitCode != 0 || handle.LeaseID == "" {
		t.Fatalf("cold relocated rpath execution: handle=%+v child=%+v err=%v", handle, child, err)
	}
	warm, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: published.ArtifactDigest, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	if warm.MaterializationID != cold.MaterializationID || warm.Path != cold.Path || warm.CacheHit != CacheLocalMaterialization {
		t.Fatalf("warm rpath materialization = %+v; cold = %+v", warm, cold)
	}
	assertDarwinLoadCommand(t, filepath.Join(cold.Path, "bin", "probe"), "@rpath/libconsumer.dylib", "@executable_path/../lib")
	assertDarwinLoadCommand(t, filepath.Join(cold.Path, "lib", "libconsumer.dylib"), "@loader_path/libdep.dylib", "@loader_path")
}

func writeDarwinSource(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runDarwinTool(t *testing.T, directory, name string, arguments ...string) {
	t.Helper()
	command := exec.Command(name, arguments...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v: %s", name, arguments, err, output)
	}
}

func addDarwinCatalogFile(t *testing.T, root *htfs.Root, source, relative string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(source, relative))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	legacyID := fmt.Sprintf("%x", sum)
	objectPath := htfs.ExactDefaultLocation(legacyID)
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o750); err != nil {
		t.Fatal(err)
	}
	var stored bytes.Buffer
	writer, err := gzip.NewWriterLevel(&stored, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objectPath, stored.Bytes(), 0o640); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(source, relative))
	if err != nil {
		t.Fatal(err)
	}
	directory, name := filepath.Split(filepath.ToSlash(relative))
	directory = strings.TrimSuffix(directory, "/")
	root.Tree.Dirs[directory].Files[name] = &htfs.File{Name: name, Size: int64(len(content)), Mode: info.Mode(), Digest: legacyID, Rewrite: []int64{}}
}

func assertDarwinLoadCommand(t *testing.T, binary string, expected ...string) {
	t.Helper()
	imports, err := exec.Command("otool", "-L", binary).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	loads, err := exec.Command("otool", "-l", binary).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	combined := string(imports) + string(loads)
	for _, value := range expected {
		if !strings.Contains(combined, value) {
			t.Fatalf("%s is missing %q from load commands:\n%s", binary, value, combined)
		}
	}
}
