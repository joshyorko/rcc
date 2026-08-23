package artifactprovider

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshyorko/rcc/environmentartifact"
)

func (it *Filesystem) ListObjects(ctx context.Context) ([]ObjectInfo, error) { return listProviderFiles(ctx, filepath.Join(it.root,"objects","sha256")) }
func (it *Filesystem) ListManifests(ctx context.Context) ([]ManifestInfo, error) { objects,e:=listProviderFiles(ctx, filepath.Join(it.root,"manifests","sha256"));out:=make([]ManifestInfo,len(objects));for i,v:=range objects{out[i]=ManifestInfo{Digest:v.Digest,Size:v.Size,ModifiedAt:v.ModifiedAt}};return out,e }
func listProviderFiles(ctx context.Context, root string) ([]ObjectInfo,error) { out:=[]ObjectInfo{}; err:=filepath.Walk(root,func(path string, info os.FileInfo, err error) error {if err!=nil{return err};if e:=ctx.Err();e!=nil{return e};if info.IsDir()||info.Mode()&os.ModeSymlink!=0{return nil}; if len(info.Name())!=64{return fmt.Errorf("invalid provider entry %q",info.Name())}; d,e:=environmentartifact.ParseDigest("sha256:"+info.Name());if e!=nil{return e};out=append(out,ObjectInfo{Digest:d,Size:info.Size(),ModifiedAt:info.ModTime()});return nil}); sort.Slice(out,func(i,j int)bool{return out[i].Digest.Hex()<out[j].Digest.Hex()});return out,err }
func (it *Filesystem) GarbageCollect(ctx context.Context, retention Retention) (GCReport,error) { manifests,err:=it.ListManifests(ctx);if err!=nil{return GCReport{},err}; report:=GCReport{ManifestsScanned:len(manifests)}; keep:=map[string]bool{}; now:=time.Now(); for _,m:=range manifests {if retention.MaxAge>0&&now.Sub(m.ModifiedAt)>retention.MaxAge{continue}; content,e:=it.ResolveManifest(ctx,m.Digest);if e!=nil{return report,e}; manifest,e:=environmentartifact.DecodeManifest(content);if e!=nil{return report,e}; keep[manifest.Specification.Descriptor.Digest.Hex()]=true;keep[manifest.LegacyBlueprint.Digest.Hex()]=true;for _,c:=range manifest.Catalogs{keep[c.Digest.Hex()]=true};keep[manifest.ObjectIndex.Digest.Hex()]=true;idx, e:=it.GetObject(ctx,manifest.ObjectIndex);if e!=nil{return report,e};idxBytes,_:=io.ReadAll(idx);_ = idx.Close(); parsed,e:=environmentartifact.DecodeObjectIndex(idxBytes);if e!=nil{return report,e};for _,entry:=range parsed.Entries{keep[entry.StoredDigest.Hex()]=true} }
	objects,e:=it.ListObjects(ctx);if e!=nil{return report,e};report.ObjectsScanned=len(objects);for _,o:=range objects{if keep[o.Digest.Hex()]{continue};if retention.MaxAge>0&&now.Sub(o.ModifiedAt)<=retention.MaxAge{continue};if e:=os.Remove(it.objectPath(o.Digest));e!=nil{continue};report.ObjectsRemoved++;report.BytesReclaimed+=o.Size};return report,nil }
func (it *Filesystem) Repair(ctx context.Context) (Health,error) { if _,e:=it.ListObjects(ctx);e!=nil{return Health{Ready:false,Corrupt:true,Storage:"corrupt"},e};if _,e:=it.ListManifests(ctx);e!=nil{return Health{Ready:false,Corrupt:true,Storage:"corrupt"},e};return it.Health(ctx) }
func (it *Filesystem) Backup(ctx context.Context,w io.Writer) error {if w==nil{return fmt.Errorf("nil backup writer")};tw:=tar.NewWriter(w);defer tw.Close();return filepath.Walk(it.root,func(path string,info os.FileInfo,e error)error{if e!=nil{return e};if e=ctx.Err();e!=nil{return e};if info.IsDir(){return nil};name:=info.Name();if strings.HasPrefix(name,".upload-")||strings.HasPrefix(name,".manifest-"){return nil};rel,_:=filepath.Rel(it.root,path);b,e:=os.ReadFile(path);if e!=nil{return e};if e=tw.WriteHeader(&tar.Header{Name:rel,Mode:0600,Size:int64(len(b)),ModTime:info.ModTime()});e!=nil{return e};_,e=tw.Write(b);return e})}
func (it *Filesystem) Restore(ctx context.Context, r io.Reader) error {if r==nil{return fmt.Errorf("nil restore reader")};tr:=tar.NewReader(r);for{if e:=ctx.Err();e!=nil{return e};h,e:=tr.Next();if e==io.EOF{break};if e!=nil{return e};if h.Typeflag!=tar.TypeReg||h.Name==""||filepath.IsAbs(h.Name)||strings.Contains(h.Name,".."+string(filepath.Separator)){return fmt.Errorf("unsafe backup path %q",h.Name)};target:=filepath.Join(it.root,filepath.Clean(h.Name));if !strings.HasPrefix(target,filepath.Clean(it.root)+string(filepath.Separator)){return fmt.Errorf("backup path escapes provider root")};b,e:=io.ReadAll(io.LimitReader(tr,maxProviderObjectBytes+1));if e!=nil{return e};if int64(len(b))!=h.Size{return fmt.Errorf("backup member size mismatch")};if existing,e:=os.ReadFile(target);e==nil{if !bytes.Equal(existing,b){return fmt.Errorf("backup conflicts with immutable content %q",h.Name)};continue};if e:=os.MkdirAll(filepath.Dir(target),0750);e!=nil{return e};if e:=os.WriteFile(target,b,0600);e!=nil{return e}};return nil}
