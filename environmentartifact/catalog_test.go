package environmentartifact

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/htfs"
)

func validCatalogForValidation(t *testing.T) (*htfs.Root, ObjectIndex, string) {
	t.Helper()
	producerIdentity := "h123456_123456789abcdeft"
	root, err := htfs.NewRoot(filepath.Join(t.TempDir(), producerIdentity))
	if err != nil {
		t.Fatal(err)
	}
	root.Tree.Mode = os.ModeDir | 0o750
	root.Tree.Files["python"] = &htfs.File{
		Name: "python", Mode: 0o750, Size: 128,
		Digest:  "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		Rewrite: []int64{8, 64},
	}
	index, _, err := NewObjectIndex([]ObjectEntry{{
		LegacyObjectID: root.Tree.Files["python"].Digest,
		StoredDigest:   testDigest(t, "f"), StoredSize: 80, LogicalSize: 128,
		Encoding: "gzip", LegacyLogicalDigestAlgorithm: "sha256",
	}})
	if err != nil {
		t.Fatal(err)
	}
	return root, index, producerIdentity
}

func TestValidateV12CatalogAcceptsSupportedSurface(t *testing.T) {
	root, index, identity := validCatalogForValidation(t)
	root.Tree.Files["relative-link"] = &htfs.File{Name: "relative-link", Mode: os.ModeSymlink, Symlink: "python"}
	if err := ValidateV12Catalog(root, index, identity); err != nil {
		t.Fatal(err)
	}
}

func TestValidateV12CatalogAcceptsPOSIXGitPerlPackageNames(t *testing.T) {
	for _, platform := range []string{"linux_amd64", "darwin_arm64"} {
		t.Run(platform, func(t *testing.T) {
			root, index, identity := validCatalogForValidation(t)
			root.Platform = platform
			for _, name := range []string{"B::Terse.3", "B::Op_private.3", "B::Showlex.3"} {
				root.Tree.Files[name] = &htfs.File{
					Name: name, Mode: 0o640, Size: root.Tree.Files["python"].Size,
					Digest: root.Tree.Files["python"].Digest,
				}
			}
			if err := ValidateV12Catalog(root, index, identity); err != nil {
				t.Fatalf("POSIX package filenames rejected unchanged: %v", err)
			}
		})
	}
}

func TestValidateV12CatalogUsesTargetPlatformPathSemantics(t *testing.T) {
	cases := []struct {
		name, platform, component string
		wantValid                 bool
	}{
		{name: "linux colon", platform: "linux_amd64", component: "B::Terse.3", wantValid: true},
		{name: "darwin colon", platform: "darwin_amd64", component: "B::Terse.3", wantValid: true},
		{name: "linux backslash", platform: "linux_amd64", component: `dir\python`, wantValid: true},
		{name: "windows drive", platform: "windows_amd64", component: "B::Terse.3", wantValid: false},
		{name: "windows backslash", platform: "windows_amd64", component: `dir\python`, wantValid: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root, index, identity := validCatalogForValidation(t)
			root.Platform = test.platform
			file := root.Tree.Files["python"]
			root.Tree.Files = map[string]*htfs.File{
				test.component: {
					Name: test.component, Mode: file.Mode, Size: file.Size,
					Digest: file.Digest, Rewrite: file.Rewrite,
				},
			}
			err := ValidateV12Catalog(root, index, identity)
			if test.wantValid && err != nil {
				t.Fatalf("target-valid component rejected: %v", err)
			}
			if !test.wantValid && err == nil {
				t.Fatal("target-invalid component accepted")
			}
		})
	}
}

func TestValidateV12CatalogRejectsUniversalAndWindowsTargetEscapes(t *testing.T) {
	cases := []struct {
		name, platform, component string
	}{
		{name: "empty POSIX", platform: "linux_amd64", component: ""},
		{name: "dot POSIX", platform: "linux_amd64", component: "."},
		{name: "dotdot POSIX", platform: "linux_amd64", component: ".."},
		{name: "absolute POSIX", platform: "linux_amd64", component: "/escape"},
		{name: "slash POSIX", platform: "linux_amd64", component: "dir/python"},
		{name: "drive relative Windows", platform: "windows_amd64", component: `C:escape`},
		{name: "drive absolute Windows", platform: "windows_amd64", component: `C:\escape`},
		{name: "rooted Windows", platform: "windows_amd64", component: `\escape`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root, index, identity := validCatalogForValidation(t)
			root.Platform = test.platform
			file := root.Tree.Files["python"]
			root.Tree.Files = map[string]*htfs.File{
				test.component: {
					Name: test.component, Mode: file.Mode, Size: file.Size,
					Digest: file.Digest, Rewrite: file.Rewrite,
				},
			}
			if err := ValidateV12Catalog(root, index, identity); err == nil {
				t.Fatal("unsafe target component accepted")
			}
		})
	}
}

func TestValidateV12CatalogUsesTargetPlatformForProducerRootAndSymlinks(t *testing.T) {
	cases := []struct {
		name, platform, rootPath, target string
		wantValid                        bool
	}{
		{name: "POSIX package link", platform: "linux_amd64", rootPath: "/producer/h123456_123456789abcdeft", target: "B::Terse.3", wantValid: true},
		{name: "POSIX backslash link", platform: "darwin_arm64", rootPath: "/producer/h123456_123456789abcdeft", target: `dir\python`, wantValid: true},
		{name: "Windows relative link", platform: "windows_amd64", rootPath: `C:\producer\h123456_123456789abcdeft`, target: "python", wantValid: true},
		{name: "Windows drive link", platform: "windows_amd64", rootPath: `C:\producer\h123456_123456789abcdeft`, target: `C:escape`, wantValid: false},
		{name: "Windows rooted link", platform: "windows_amd64", rootPath: `C:\producer\h123456_123456789abcdeft`, target: `\escape`, wantValid: false},
		{name: "POSIX escaping link", platform: "linux_amd64", rootPath: "/producer/h123456_123456789abcdeft", target: "../escape", wantValid: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root, index, identity := validCatalogForValidation(t)
			root.Platform = test.platform
			root.Path = test.rootPath
			root.Tree.Files["link"] = &htfs.File{Name: "link", Mode: os.ModeSymlink, Symlink: test.target}
			err := ValidateV12Catalog(root, index, identity)
			if test.wantValid && err != nil {
				t.Fatalf("target-valid root or symlink rejected: %v", err)
			}
			if !test.wantValid && err == nil {
				t.Fatal("target-invalid root or symlink accepted")
			}
		})
	}
}

func TestValidateV12CatalogRejectsUnsafeTreeMetadata(t *testing.T) {
	cases := map[string]func(*htfs.Root){
		"symlink root": func(root *htfs.Root) { root.Tree.Symlink = "elsewhere" },
		"absolute name": func(root *htfs.Root) {
			root.Tree.Files = map[string]*htfs.File{"/escape": {Name: "/escape", Mode: 0o640, Digest: root.Tree.Files["python"].Digest, Size: 128}}
		},
		"dot name":  func(root *htfs.Root) { root.Tree.Files["python"].Name = ".." },
		"separator": func(root *htfs.Root) { root.Tree.Files["python"].Name = `dir/python` },
		"file directory collision": func(root *htfs.Root) {
			root.Tree.Dirs["python"] = &htfs.Dir{Name: "python", Mode: os.ModeDir | 0o750, Dirs: map[string]*htfs.Dir{}, Files: map[string]*htfs.File{}}
		},
		"unsupported mode": func(root *htfs.Root) { root.Tree.Files["python"].Mode = os.ModeDevice | 0o640 },
		"escaping symlink": func(root *htfs.Root) {
			root.Tree.Files["link"] = &htfs.File{Name: "link", Mode: os.ModeSymlink, Symlink: "../../escape"}
		},
		"absolute symlink": func(root *htfs.Root) {
			root.Tree.Files["link"] = &htfs.File{Name: "link", Mode: os.ModeSymlink, Symlink: "/escape"}
		},
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			root, index, identity := validCatalogForValidation(t)
			mutate(root)
			if err := ValidateV12Catalog(root, index, identity); err == nil {
				t.Fatal("unsafe catalog metadata accepted")
			}
		})
	}
}

func TestValidateV12CatalogRejectsUnsafeRewriteOffsets(t *testing.T) {
	cases := map[string][]int64{
		"negative":      {-1},
		"out of bounds": {120},
	}
	for name, offsets := range cases {
		t.Run(name, func(t *testing.T) {
			root, index, identity := validCatalogForValidation(t)
			root.Tree.Files["python"].Rewrite = offsets
			if err := ValidateV12Catalog(root, index, identity); err == nil {
				t.Fatal("unsafe rewrite offsets accepted")
			}
		})
	}
}

func TestValidateV12CatalogAllowsBoundedUnorderedAndOverlappingRewriteOffsets(t *testing.T) {
	root, index, identity := validCatalogForValidation(t)
	root.Tree.Files["python"].Rewrite = []int64{64, 8, 16, 8}
	if err := ValidateV12Catalog(root, index, identity); err != nil {
		t.Fatal(err)
	}
}

func TestValidateV12CatalogRejectsSpecialPermissionBits(t *testing.T) {
	for name, mode := range map[string]os.FileMode{
		"setuid": os.ModeSetuid | 0o750,
		"setgid": os.ModeSetgid | 0o750,
		"sticky": os.ModeSticky | 0o750,
	} {
		t.Run(name, func(t *testing.T) {
			root, index, identity := validCatalogForValidation(t)
			root.Tree.Files["python"].Mode = mode
			if err := ValidateV12Catalog(root, index, identity); err == nil {
				t.Fatalf("special file mode %v accepted", mode)
			}
		})
	}
}

func TestValidateV12CatalogRejectsMissingOrUnindexedObjects(t *testing.T) {
	root, index, identity := validCatalogForValidation(t)
	index.Entries = nil
	index.Count = 0
	index.TotalStoredBytes = 0
	index.TotalLogicalBytes = 0
	if err := ValidateV12Catalog(root, index, identity); err == nil {
		t.Fatal("unindexed catalog object accepted")
	}

	root, index, identity = validCatalogForValidation(t)
	index.Entries = append(index.Entries, ObjectEntry{
		LegacyObjectID: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		StoredDigest:   testDigest(t, "c"), StoredSize: 1, LogicalSize: 1,
		Encoding: "gzip", LegacyLogicalDigestAlgorithm: "sha256",
	})
	index.Count++
	index.TotalStoredBytes++
	index.TotalLogicalBytes++
	if err := ValidateV12Catalog(root, index, identity); err == nil {
		t.Fatal("object index entry not referenced by catalog accepted")
	}
}
