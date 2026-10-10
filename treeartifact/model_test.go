package treeartifact

import (
	"bytes"
	"context"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
)

func TestCanonicalTreeIdentity(t *testing.T) {
	d := descriptor(FileMediaType, []byte("content"))
	a := Entry{Name: "a", Type: TypeFile, Mode: 0644, Size: 7, Object: &d}
	b := a
	b.Name = "b"
	x, dx, err := EncodeTree(Tree{Version: Version, Mode: 0755, Entries: []Entry{b, a}})
	if err != nil {
		t.Fatal(err)
	}
	y, dy, err := EncodeTree(Tree{Version: Version, Mode: 0755, Entries: []Entry{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	if dx != dy || !bytes.Equal(x, y) {
		t.Fatal("input order changed canonical identity")
	}
	b.Mode = 0600
	_, dz, err := EncodeTree(Tree{Version: Version, Mode: 0755, Entries: []Entry{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	if dz == dx {
		t.Fatal("mode change did not change identity")
	}
	_, _, err = EncodeTree(Tree{Version: Version, Mode: 0755, Entries: []Entry{a, a}})
	if err == nil {
		t.Fatal("accepted duplicate names")
	}
}

func TestMalformedAndTraversalTreeRejection(t *testing.T) {
	e, _ := captureFixture(t)
	for _, body := range []string{
		`{"version":1,"mode":493,"entries":[],"unknown":true}`,
		`{"version":1,"version":1,"mode":493,"entries":[]}`,
		`{"version":1,"mode":493,"entries":null}`,
		`{"version":2,"mode":493,"entries":[]}`,
		`{"version":1,"mode":493,"entries":[]} `,
		`{"version":1,"mode":493,"entries":[{"name":"../escape","type":"symlink","mode":511,"size":1,"target":"x"}]}`,
	} {
		d := descriptor(TreeMediaType, []byte(body))
		if err := e.Store.PutObject(context.Background(), artifactprovider.Blob{Descriptor: d, Reader: bytes.NewBufferString(body)}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.LoadTree(context.Background(), d); err == nil {
			t.Fatalf("accepted malformed tree %s", body)
		}
	}
}
