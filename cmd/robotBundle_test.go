package cmd

import (
	"archive/zip"
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
