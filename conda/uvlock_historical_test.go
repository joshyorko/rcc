package conda

import "testing"

func TestUVReleaseDigestHistorical(t *testing.T) {
	assets := map[string]string{
		"uv-x86_64-unknown-linux-gnu.tar.gz":  "0dcad9831d3f10f3bc4dcd7678948dfc74c0b3ab3f07aa684eb9e5135b971a58",
		"uv-aarch64-unknown-linux-gnu.tar.gz": "f355989fb5ecf47c9f9087a0b21e2ee7d7c802bc3d0cf6edae07560d4297751f",
		"uv-x86_64-apple-darwin.tar.gz":       "d8609b53f280d5e784a7586bf7a3fd90c557656af109cee8572b24a0c1443191",
		"uv-aarch64-apple-darwin.tar.gz":      "90e10cc7f26cbaf3eaa867cf99344ffd550e942fd4b660e88f2f91c23022dc5a",
		"uv-x86_64-pc-windows-msvc.zip":       "32de1730597db0a7c5f34e2257ab491b660374b22c016c3d9a59ae279d837697",
	}
	for asset, want := range assets {
		got, err := uvReleaseDigest("0.6.1", asset, "")
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v", asset, got, err)
		}
		if _, err := uvReleaseDigest("0.6.2", asset, ""); err == nil {
			t.Errorf("accepted unpinned version for %s", asset)
		}
		if _, err := uvReleaseDigest("0.6.1", asset, "sha256:invalid"); err == nil {
			t.Errorf("accepted malformed metadata for %s", asset)
		}
	}
	if _, err := uvReleaseDigest("0.6.1", "unknown", ""); err == nil {
		t.Fatal("accepted unpinned asset")
	}
	want := assets["uv-x86_64-unknown-linux-gnu.tar.gz"]
	if got, err := uvReleaseDigest("0.6.2", "unknown", "sha256:"+want); err != nil || got != want {
		t.Fatalf("valid metadata: got %q, %v", got, err)
	}
}
