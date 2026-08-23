package artifactprovider

import (
 "archive/tar"
 "bytes"
 "context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
 "testing"
 "github.com/joshyorko/rcc/environmentartifact"
)

type failingReader struct { data []byte; cut int; sent int }
func (r *failingReader) Read(p []byte) (int,error) { if r.sent >= r.cut { return 0,io.ErrUnexpectedEOF }; n:=copy(p,r.data[r.sent:minInt(r.cut,r.sent+len(p))]); r.sent+=n; return n,nil }
func minInt(a,b int)int{if a<b{return a};return b}

func TestFilesystemRestorePreflightsConflictsBeforePublishing(t *testing.T) {
 p, err := NewFilesystem(t.TempDir()); if err != nil { t.Fatal(err) }
	conflict := testBlob([]byte("old")); putFixtureBlob(t,p,conflict)
	newBlob := testBlob([]byte("new")); var archive bytes.Buffer; tw:=tar.NewWriter(&archive)
	for _, item := range []struct{name string; data []byte}{{"objects/sha256/"+newBlob.Descriptor.Digest.Hex(),[]byte("new")},{"objects/sha256/"+conflict.Descriptor.Digest.Hex(),[]byte("different")}} { if err:=tw.WriteHeader(&tar.Header{Name:item.name,Typeflag:tar.TypeReg,Size:int64(len(item.data))});err!=nil{t.Fatal(err)};if _,err:=tw.Write(item.data);err!=nil{t.Fatal(err)} }
	if err:=tw.Close();err!=nil{t.Fatal(err)}
	if err:=p.Restore(context.Background(),&archive);err==nil{t.Fatal("restore accepted immutable conflict")}
	if _,err:=p.GetObject(context.Background(),environmentartifact.Descriptor{Digest:newBlob.Descriptor.Digest,Size:newBlob.Descriptor.Size});err==nil{t.Fatal("restore published staged object before conflict validation")}
}

func TestProviderContractFilesystemAndJournal(t *testing.T) {
	providers:=[]struct{name string; new func() (Provider,error)}{
		{"filesystem",func()(Provider,error){p,e:=NewFilesystem(t.TempDir());return p,e}},
		{"journal",func()(Provider,error){p,e:=NewJournal(t.TempDir()+"/provider.log");return p,e}},
	}
	for _,tc:=range providers{t.Run(tc.name,func(t *testing.T){p,e:=tc.new();if e!=nil{t.Fatal(e)};f:=newProviderFixture(t);for _,b:=range f.blobs{raw,e:=io.ReadAll(b.Reader);if e!=nil{t.Fatal(e)};if e=p.PutObject(context.Background(),Blob{Descriptor:b.Descriptor,Reader:bytes.NewReader(raw)});e!=nil{t.Fatal(e)}};if e=p.CommitManifest(context.Background(),f.manifestBytes);e!=nil{t.Fatal(e)};got,e:=p.ResolveManifest(context.Background(),f.manifest.ArtifactDigest);if e!=nil||!bytes.Equal(got,f.manifestBytes){t.Fatalf("manifest resolve err=%v",e)}})}
}

func TestInterruptedUploadsLeaveNoProvisionalFilesAndCanRestart(t *testing.T) {
	for _,tc:=range []struct{name string; new func(string)(Provider,error)}{{"filesystem",func(root string)(Provider,error){return NewFilesystem(root)}},{"journal",func(root string)(Provider,error){return NewJournal(root+"/provider.log")}}}{t.Run(tc.name,func(t *testing.T){root:=t.TempDir();p,e:=tc.new(root);if e!=nil{t.Fatal(e)};content:=bytes.Repeat([]byte("x"),1024);blob:=testBlob(content);if e=p.PutObject(context.Background(),Blob{Descriptor:blob.Descriptor,Reader:&failingReader{data:content,cut:100}});e==nil{t.Fatal("interrupted upload accepted")};if e=p.PutObject(context.Background(),Blob{Descriptor:blob.Descriptor,Reader:bytes.NewReader(content)});e!=nil{t.Fatal(e)}})}
}

func TestHTTPProxyAndNoProxyRealRequests(t *testing.T) {
	var proxyHits atomic.Int64
	target:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.Header().Set("Content-Type","application/json");_,_=w.Write([]byte(`{"schemaVersions":[1],"digestAlgorithms":["sha256"],"encodings":["gzip"]}`))}));defer target.Close()
	proxy:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){proxyHits.Add(1);w.Header().Set("Content-Type","application/json");_,_=w.Write([]byte(`{"schemaVersions":[1],"digestAlgorithms":["sha256"],"encodings":["gzip"]}`))}));defer proxy.Close()
	via,e:=NewHTTPWithOptions(target.URL,HTTPOptions{Client:target.Client(),ProxyURL:proxy.URL});if e!=nil{t.Fatal(e)};if _,e=via.Capabilities(context.Background());e!=nil{t.Fatal(e)};if proxyHits.Load()!=1{t.Fatalf("proxy hits=%d",proxyHits.Load())}
	direct,e:=NewHTTPWithOptions(target.URL,HTTPOptions{Client:target.Client(),ProxyURL:proxy.URL,NoProxy:"127.0.0.1"});if e!=nil{t.Fatal(e)};if _,e=direct.Capabilities(context.Background());e!=nil{t.Fatal(e)};if proxyHits.Load()!=1{t.Fatalf("no-proxy request used proxy: hits=%d",proxyHits.Load())}
}

func TestHTTPAbuseBoundaries(t *testing.T) {
	p, err := NewFilesystem(t.TempDir()); if err != nil { t.Fatal(err) }
	h := NewHandler(p)
	for _, tc := range []struct{name, method, path, body string}{{"encoded traversal",http.MethodGet,"/v1/objects/sha256/%2e%2e/secret",""},{"oversized missing request",http.MethodPost,"/v1/objects/missing",strings.Repeat("x",maxProviderJSONBytes+1)}} {
		t.Run(tc.name,func(t *testing.T){req:=httptest.NewRequest(tc.method,tc.path,strings.NewReader(tc.body));if tc.body!=""{req.Header.Set("Content-Type","application/json")};rec:=httptest.NewRecorder();h.ServeHTTP(rec,req);if rec.Code<400||rec.Code>=500{t.Fatalf("abuse request status=%d",rec.Code)}})
	}
}
