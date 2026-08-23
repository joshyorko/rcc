package artifacttrust

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type FilesystemCarrier struct{ Root string }
func NewFilesystemCarrier(root string) *FilesystemCarrier { return &FilesystemCarrier{Root:root} }
func (c *FilesystemCarrier) Read(name string)([]byte,error){ return os.ReadFile(filepath.Join(c.Root,filepath.FromSlash(name))) }
func (c *FilesystemCarrier) Write(name string,b []byte)error{ p:=filepath.Join(c.Root,filepath.FromSlash(name)); if !strings.HasPrefix(filepath.Clean(p),filepath.Clean(c.Root)+string(os.PathSeparator)){return fmt.Errorf("carrier path escapes root")}; if err:=os.MkdirAll(filepath.Dir(p),0700);err!=nil{return err}; return os.WriteFile(p,b,0600) }

type HTTPCarrier struct{ BaseURL string; Client *http.Client }
func (c *HTTPCarrier) Read(name string)([]byte,error){ req,err:=http.NewRequest(http.MethodGet,strings.TrimRight(c.BaseURL,"/")+"/"+name,nil);if err!=nil{return nil,err}; resp,err:=c.client().Do(req);if err!=nil{return nil,err};defer resp.Body.Close();if resp.StatusCode<200||resp.StatusCode>=300{return nil,fmt.Errorf("carrier HTTP status %s",resp.Status)};return io.ReadAll(resp.Body) }
func (c *HTTPCarrier) Write(name string,b []byte)error{ req,err:=http.NewRequest(http.MethodPut,strings.TrimRight(c.BaseURL,"/")+"/"+name,bytes.NewReader(b));if err!=nil{return err};resp,err:=c.client().Do(req);if err!=nil{return err};defer resp.Body.Close();if resp.StatusCode<200||resp.StatusCode>=300{return fmt.Errorf("carrier HTTP status %s",resp.Status)};return nil }
func(c *HTTPCarrier)client()*http.Client{if c.Client!=nil{return c.Client};return http.DefaultClient}

type ArchiveCarrier struct{ Files map[string][]byte }
func OpenArchiveCarrier(path string)(*ArchiveCarrier,error){ z,err:=zip.OpenReader(path);if err!=nil{return nil,err};defer z.Close(); out:=&ArchiveCarrier{Files:map[string][]byte{}};for _,f:=range z.File{if f.FileInfo().IsDir(){continue};r,e:=f.Open();if e!=nil{return nil,e};b,e:=io.ReadAll(r);r.Close();if e!=nil{return nil,e};out.Files[f.Name]=b};return out,nil }
func(c *ArchiveCarrier)Read(n string)([]byte,error){b,ok:=c.Files[n];if !ok{return nil,os.ErrNotExist};return b,nil}
func(c *ArchiveCarrier)Write(n string,b []byte)error{if c.Files==nil{c.Files=map[string][]byte{}};c.Files[n]=append([]byte(nil),b...);return nil}
func ExportArchive(c Carrier,path,artifact string,kinds []string)error{f,err:=os.Create(path);if err!=nil{return err};defer f.Close();z:=zip.NewWriter(f);for _,k:=range kinds{b,e:=GetAttachment(c,artifact,k);if e!=nil{z.Close();return e};w,e:=z.Create(AttachmentName(artifact,k));if e!=nil{z.Close();return e};if _,e= w.Write(b);e!=nil{z.Close();return e}};return z.Close()}

type ReceiptStore struct{ Root string }
func NewReceiptStore(root string)*ReceiptStore{return &ReceiptStore{Root:root}}
func(s *ReceiptStore)Put(r VerificationReceipt)error{b,e:=r.JSON();if e!=nil{return e};if err:=os.MkdirAll(s.Root,0700);err!=nil{return err};return os.WriteFile(filepath.Join(s.Root,strings.ReplaceAll(r.ArtifactDigest,":","_")+".json"),b,0600)}
