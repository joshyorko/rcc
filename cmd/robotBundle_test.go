package cmd

import (
	"archive/zip"
	"encoding/json"
	"github.com/joshyorko/rcc/environmentartifact"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeBundleFixture(t *testing.T, root string) (string, string, string) {
	t.Helper()
	robotYaml := filepath.Join(root, "robot.yaml")
	hololib := filepath.Join(t.TempDir(), "hololib.zip")
	conda := filepath.Join(root, "conda.yaml")
	for path, body := range map[string]string{
		robotYaml:                           "tasks:\n  test:\n    command:\n      - python\n      - task.py\ncondaConfigFile: conda.yaml\nignoreFiles:\n  - .robotignore\n",
		filepath.Join(root, ".robotignore"): "ignored.txt\nlinked.py\n",
		filepath.Join(root, "task.py"):      "print('included')\n",
		filepath.Join(root, "ignored.txt"):  "secret\n",
		conda:                               "channels: []\n",
		hololib:                             "hololib",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return robotYaml, hololib, conda
}

func TestCreateBundleWithArtifactDeclaresSourcePlusArtifactMode(t *testing.T) {
	root := t.TempDir()
	robotYaml, hololib, conda := writeBundleFixture(t, root)
	artifact := filepath.Join(t.TempDir(), "environment.rcca")
	if err := os.WriteFile(artifact, []byte("canonical artifact bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "bundle.py")
	if err := createBundleWithArtifact(robotYaml, hololib, output, conda, artifact); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var mode map[string]string
	for _, entry := range zr.File {
		if entry.Name != "environment/bundle.json" {
			continue
		}
		reader, openErr := entry.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		decodeErr := json.NewDecoder(reader).Decode(&mode)
		_ = reader.Close()
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
	}
	if mode["sourceMode"] != "source+artifact" {
		t.Fatalf("bundle source mode = %q", mode["sourceMode"])
	}
	if !bundleEntries(t, output)["environment/artifact.rcca"] {
		t.Fatal("source+artifact bundle omitted artifact payload")
	}
}

func TestCreateBundleWithPlatformIndexEmbedsValidatedIndex(t *testing.T) {
	root := t.TempDir()
	robotYaml, hololib, conda := writeBundleFixture(t, root)
	artifact := filepath.Join(t.TempDir(), "environment.rcca")
	if err := os.WriteFile(artifact, []byte("artifact"), 0o640); err != nil {
		t.Fatal(err)
	}
	_, indexBytes, err := environmentartifact.NewPlatformIndex(environmentartifact.DigestBytes([]byte("spec")), []environmentartifact.PlatformArtifact{{Platform: environmentartifact.CurrentPlatform(), Artifact: environmentartifact.DigestBytes([]byte("artifact"))}})
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(t.TempDir(), "platform-index.json")
	if err := os.WriteFile(indexPath, indexBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "bundle.py")
	if err := createBundleWithArtifactAndIndex(robotYaml, hololib, output, conda, artifact, indexPath); err != nil {
		t.Fatal(err)
	}
	entries := bundleEntries(t, output)
	if !entries["environment/platform-index.json"] {
		t.Fatal("bundle omitted platform index")
	}
}

func bundleEntries(t *testing.T, filename string) map[string]bool {
	t.Helper()
	zr, err := zip.OpenReader(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	result := make(map[string]bool, len(zr.File))
	for _, entry := range zr.File {
		result[entry.Name] = true
	}
	return result
}

func TestCreateBundleUsesRobotIgnoreFiles(t *testing.T) {
	root := t.TempDir()
	robotYaml, hololib, conda := writeBundleFixture(t, root)
	output := filepath.Join(t.TempDir(), "bundle.py")

	if err := createBundle(robotYaml, hololib, output, conda); err != nil {
		t.Fatal(err)
	}
	entries := bundleEntries(t, output)
	if !entries["robot/task.py"] || !entries["robot/robot.yaml"] {
		t.Fatalf("expected project files in bundle, got %v", entries)
	}
	if entries["robot/ignored.txt"] {
		t.Fatal("robot.yaml ignoreFiles entry was included")
	}
}

func TestCreateBundleRejectsProjectSymlinkAndPreservesOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires additional privileges on Windows")
	}
	root := t.TempDir()
	robotYaml, hololib, conda := writeBundleFixture(t, root)
	if err := os.Symlink(filepath.Join(root, "task.py"), filepath.Join(root, "linked.py")); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "bundle.py")
	if err := os.WriteFile(output, []byte("previous bundle"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := createBundle(robotYaml, hololib, output, conda)
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("expected symbolic link rejection, got %v", err)
	}
	content, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "previous bundle" {
		t.Fatalf("failed creation replaced output with %q", content)
	}
}

func TestCreateBundleAtomicallyReplacesExistingOutput(t *testing.T) {
	root := t.TempDir()
	robotYaml, hololib, conda := writeBundleFixture(t, root)
	output := filepath.Join(t.TempDir(), "bundle.py")
	if err := os.WriteFile(output, []byte("previous bundle"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := createBundle(robotYaml, hololib, output, conda); err != nil {
		t.Fatal(err)
	}
	entries := bundleEntries(t, output)
	if !entries["hololib/hololib.zip"] || !entries["envs/default/conda.yaml"] {
		t.Fatalf("replacement bundle missing payload entries: %v", entries)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("replacement bundle is not executable: %v", info.Mode())
	}
}
