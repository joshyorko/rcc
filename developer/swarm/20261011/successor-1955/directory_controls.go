package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pathlib"
	_ "github.com/joshyorko/rcc/htfs"
)

func mode(p string) os.FileMode { s,e:=os.Lstat(p); if e!=nil {panic(e)};return s.Mode().Perm() }
func must(e error) {if e!=nil {panic(e)}}
func main() {
	root:=os.Args[1];must(os.Mkdir(root,0700));var results []map[string]any
	check:=func(name string,pass bool){results=append(results,map[string]any{"name":name,"pass":pass});if !pass {panic(name)}}
	check("import-home-stays-0700",mode(os.Getenv("ROBOCORP_HOME"))==0700)
	check("import-created-library-0750",mode(filepath.Join(os.Getenv("ROBOCORP_HOME"),"hololib","library"))==0750)
	for _,name:=range []string{"existing","nonhome","badmode","link-target","filecase","shared"} {must(os.Mkdir(filepath.Join(root,name),0700))}
	home:=filepath.Join(root,"existing");common.Product.ForceHome(home)
	_,err:=pathlib.MakeSharedDir(filepath.Join(home,"child"));check("existing-home-parent-preserved",err==nil&&mode(home)==0700&&mode(filepath.Join(home,"child"))==0750)
	_,err=pathlib.MakeSharedDir(home);check("explicit-leaf-existing-behavior",err==nil&&mode(home)==0750)
	missing:=filepath.Join(root,"nonhome","missing-home");common.Product.ForceHome(missing)
	_,err=pathlib.MakeSharedDir(filepath.Join(missing,"child"));check("missing-home-original-recursion",err==nil&&mode(missing)==0750&&mode(filepath.Join(root,"nonhome"))==0750)
	common.Product.ForceHome(filepath.Join(root,"unused"));p:=filepath.Join(root,"filecase");_,err=pathlib.MakeSharedDir(filepath.Join(p,"child"));check("nonhome-parent-original-mode",err==nil&&mode(p)==0750)
	bad:=filepath.Join(root,"badmode");must(os.Chmod(bad,0777));common.Product.ForceHome(bad);_,err=pathlib.MakeSharedDir(filepath.Join(bad,"child"));_,absent:=os.Lstat(filepath.Join(bad,"child"));check("unexpected-home-mode-rejected",err!=nil&&os.IsNotExist(absent)&&mode(bad)==0777)
	link:=filepath.Join(root,"home-link");must(os.Symlink(filepath.Join(root,"link-target"),link));common.Product.ForceHome(link);_,err=pathlib.MakeSharedDir(filepath.Join(link,"child"));check("home-symlink-rejected",err!=nil&&mode(filepath.Join(root,"link-target"))==0700)
	file:=filepath.Join(root,"home-file");must(os.WriteFile(file,[]byte("unchanged"),0600));common.Product.ForceHome(file);_,err=pathlib.MakeSharedDir(filepath.Join(file,"child"));b,e:=os.ReadFile(file);check("home-nondirectory-rejected",err!=nil&&e==nil&&string(b)=="unchanged")
	foreign:="/opt/codex";s,e:=os.Lstat(foreign);if e==nil {st,ok:=s.Sys().(*syscall.Stat_t);if ok&&int(st.Uid)!=os.Getuid() {common.Product.ForceHome(foreign);_,err=pathlib.MakeSharedDir(filepath.Join(foreign,"rcc-successor-must-not-create"));_,absent=os.Lstat(filepath.Join(foreign,"rcc-successor-must-not-create"));check("foreign-parent-rejected-before-mkdir",err!=nil&&os.IsNotExist(absent))}else{panic("foreign owner fixture unavailable")}}else{panic(e)}
	shared:=filepath.Join(root,"shared");common.SharedHolotree=true;pathlib.ForceShared();common.Product.ForceHome(shared);_,err=pathlib.MakeSharedDir(filepath.Join(shared,"child"));check("shared-original-0777",err==nil&&mode(shared)==0777&&mode(filepath.Join(shared,"child"))==0777)
	check("enclosing-boundary-stays-0700",mode(root)==0700)
	json.NewEncoder(os.Stdout).Encode(results);fmt.Fprintln(os.Stderr,"controls passed")
}
