package artifactprovider

import (
 "archive/tar"
 "bytes"
 "context"
 "testing"
 "github.com/joshyorko/rcc/environmentartifact"
)

func TestFilesystemRestorePreflightsConflictsBeforePublishing(t *testing.T) {
 p, err := NewFilesystem(t.TempDir()); if err != nil { t.Fatal(err) }
	conflict := testBlob([]byte("old")); putFixtureBlob(t,p,conflict)
	newBlob := testBlob([]byte("new")); var archive bytes.Buffer; tw:=tar.NewWriter(&archive)
	for _, item := range []struct{name string; data []byte}{{"objects/sha256/"+newBlob.Descriptor.Digest.Hex(),[]byte("new")},{"objects/sha256/"+conflict.Descriptor.Digest.Hex(),[]byte("different")}} { if err:=tw.WriteHeader(&tar.Header{Name:item.name,Typeflag:tar.TypeReg,Size:int64(len(item.data))});err!=nil{t.Fatal(err)};if _,err:=tw.Write(item.data);err!=nil{t.Fatal(err)} }
	if err:=tw.Close();err!=nil{t.Fatal(err)}
	if err:=p.Restore(context.Background(),&archive);err==nil{t.Fatal("restore accepted immutable conflict")}
	if _,err:=p.GetObject(context.Background(),environmentartifact.Descriptor{Digest:newBlob.Descriptor.Digest,Size:newBlob.Descriptor.Size});err==nil{t.Fatal("restore published staged object before conflict validation")}
}
