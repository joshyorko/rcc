package artifactprovider

import (
 "bufio"
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/base64"
 "encoding/json"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "sync"
 "github.com/joshyorko/rcc/environmentartifact"
)

// Journal stores bytes in separate files and keeps only bounded metadata in memory.
type Journal struct { path, objectDir string; mu sync.RWMutex; objects map[string]objectRef; manifests map[string][]byte }
type objectRef struct { path string; size int64; mediaType string }
type journalRecord struct { Kind, Digest, MediaType, Content string; Size int64 }

func NewJournal(path string) (*Journal,error) {
 j:=&Journal{path:path,objectDir:path+".objects",objects:map[string]objectRef{},manifests:map[string][]byte{}}
 if err:=os.MkdirAll(j.objectDir,0700);err!=nil{return nil,err}; f,e:=os.Open(path);if os.IsNotExist(e){return j,nil};if e!=nil{return nil,e};defer f.Close()
 s:=bufio.NewScanner(f);s.Buffer(make([]byte,4096),int(maxManifestBytes+4096));for s.Scan(){var r journalRecord;if json.Unmarshal(s.Bytes(),&r)!=nil{break};if r.Kind=="manifest"{b,e:=base64.StdEncoding.DecodeString(r.Content);if e!=nil{return nil,e};j.manifests[r.Digest]=b;continue};if r.Kind!="object"{continue};p:=filepath.Join(j.objectDir,r.Digest);if r.Content!=""{b,e:=base64.StdEncoding.DecodeString(r.Content);if e!=nil{return nil,e};if e=os.WriteFile(p,b,0600);e!=nil{return nil,e};r.Size=int64(len(b))};if r.Size>=0&&r.Size<=maxProviderObjectBytes{if st,e:=os.Stat(p);e==nil&&st.Size()==r.Size{j.objects[r.Digest]=objectRef{p,r.Size,r.MediaType}}}}
 if e:=s.Err();e!=nil{return nil,e};return j,nil
}
func (j *Journal) Capabilities(context.Context)(Capabilities,error){return Capabilities{SchemaVersions:[]int{1},DigestAlgorithms:[]string{"sha256"},Encodings:[]string{"gzip"}},nil}
func (j *Journal) Health(ctx context.Context)(Health,error){if e:=ctx.Err();e!=nil{return Health{},e};j.mu.RLock();defer j.mu.RUnlock();var n int64;for _,r:=range j.objects{n+=r.size};return Health{Ready:true,Storage:"ok",Capability:"ok",Auth:"not-applicable",Quota:"ok",GC:"idle",Objects:int64(len(j.objects)),Manifests:int64(len(j.manifests)),Bytes:n},nil}
func (j *Journal) MissingObjects(ctx context.Context,ds []environmentartifact.Descriptor)([]environmentartifact.Digest,error){j.mu.RLock();defer j.mu.RUnlock();out:=[]environmentartifact.Digest{};for _,d:=range ds{if e:=ctx.Err();e!=nil{return nil,e};r,ok:=j.objects[d.Digest.Hex()];if !ok||r.size!=d.Size{out=append(out,d.Digest)}};return out,nil}
func (j *Journal) PutObject(ctx context.Context,b Blob)error{if b.Reader==nil||b.Descriptor.Size<0||b.Descriptor.Size>maxProviderObjectBytes{return fmt.Errorf("invalid object upload")};t,e:=os.CreateTemp(j.objectDir,".upload-");if e!=nil{return e};name:=t.Name();defer os.Remove(name);h:=sha256.New();n,e:=io.Copy(io.MultiWriter(t,h),io.LimitReader(&contextReader{ctx:ctx,reader:b.Reader},b.Descriptor.Size+1));if e!=nil{t.Close();return e};if e=t.Close();e!=nil{return e};actual,e:=environmentartifact.ParseDigest("sha256:"+hex.EncodeToString(h.Sum(nil)));if e!=nil||n!=b.Descriptor.Size||actual!=b.Descriptor.Digest{return fmt.Errorf("object size or digest mismatch")};k:=b.Descriptor.Digest.Hex();j.mu.Lock();defer j.mu.Unlock();if r,ok:=j.objects[k];ok{if r.size!=n{return fmt.Errorf("conflicting immutable object")};return nil};final:=filepath.Join(j.objectDir,k);if e=os.Rename(name,final);e!=nil{return e};if e=j.append(journalRecord{Kind:"object",Digest:k,MediaType:b.Descriptor.MediaType,Size:n});e!=nil{return e};j.objects[k]=objectRef{final,n,b.Descriptor.MediaType};return nil}
func (j *Journal) GetObject(ctx context.Context,d environmentartifact.Descriptor)(io.ReadCloser,error){if e:=ctx.Err();e!=nil{return nil,e};j.mu.RLock();r,ok:=j.objects[d.Digest.Hex()];j.mu.RUnlock();if !ok||r.size!=d.Size{return nil,os.ErrNotExist};return os.Open(r.path)}
func (j *Journal) GetObjectByDigest(ctx context.Context,d environmentartifact.Digest)(io.ReadCloser,int64,error){if e:=ctx.Err();e!=nil{return nil,0,e};j.mu.RLock();r,ok:=j.objects[d.Hex()];j.mu.RUnlock();if !ok{return nil,0,os.ErrNotExist};f,e:=os.Open(r.path);return f,r.size,e}
func (j *Journal) CommitManifest(ctx context.Context,c []byte)error{if e:=ctx.Err();e!=nil{return e};m,e:=environmentartifact.DecodeManifest(c);if e!=nil{return e};j.mu.Lock();defer j.mu.Unlock();k:=m.ArtifactDigest.Hex();if p,ok:=j.manifests[k];ok{if !bytes.Equal(p,c){return fmt.Errorf("conflicting immutable manifest")};return nil};if e=j.verifyManifestClosure(m);e!=nil{return e};if e=j.append(journalRecord{Kind:"manifest",Digest:k,Content:base64.StdEncoding.EncodeToString(c)});e!=nil{return e};j.manifests[k]=append([]byte(nil),c...);return nil}
func (j *Journal) verifyManifestClosure(m environmentartifact.Manifest)error{if len(m.Catalogs)==0{return fmt.Errorf("manifest catalog is empty")};refs:=[]environmentartifact.Descriptor{m.Specification.Descriptor,m.LegacyBlueprint.Descriptor,m.ObjectIndex,m.Catalogs[0].Descriptor};for _,x:=range refs{r,ok:=j.objects[x.Digest.Hex()];if !ok||r.size!=x.Size{return fmt.Errorf("manifest dependency %s is not complete",x.Digest)}};f,e:=j.GetObject(context.Background(),m.ObjectIndex);if e!=nil{return e};defer f.Close();b,e:=io.ReadAll(io.LimitReader(f,maxProviderObjectBytes+1));if e!=nil{return e};idx,e:=environmentartifact.DecodeObjectIndex(b);if e!=nil{return e};for _,x:=range idx.Entries{r,ok:=j.objects[x.StoredDigest.Hex()];if !ok||r.size!=x.StoredSize{return fmt.Errorf("manifest dependency %s is not complete",x.StoredDigest)}};return nil}
func (j *Journal) ResolveManifest(ctx context.Context,d environmentartifact.Digest)([]byte,error){if e:=ctx.Err();e!=nil{return nil,e};j.mu.RLock();b,ok:=j.manifests[d.Hex()];j.mu.RUnlock();if !ok{return nil,os.ErrNotExist};return append([]byte(nil),b...),nil}
func(j *Journal) append(r journalRecord)error{f,e:=os.OpenFile(j.path,os.O_WRONLY|os.O_CREATE|os.O_APPEND,0600);if e!=nil{return e};defer f.Close();b,e:=json.Marshal(r);if e!=nil{return e};if _,e=f.Write(append(b,'\n'));e!=nil{return e};return f.Sync()}
var _ Provider=(*Journal)(nil)
