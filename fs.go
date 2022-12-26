package main

// Naming conventions:
// - apath: an absolute path against the OS root
// - upath: an absolute path, relative to the UFS mount point
// FIXME
// - remove the asserts

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"aqwari.net/net/styx"
)

type UFS struct {
	id             string
	layers         []string
	addr           string
	port           int
	mnt            string
	debug, verbose bool
}

type UFile struct {
	apath string
	depth int
}

func sleep2(s string, ms time.Duration) {
	fmt.Printf("--- sleep %s %dms\n", s, ms)
	time.Sleep(ms * time.Millisecond)
}

func (ufs UFS) serve() {
	fmt.Printf("UFS.serve: id=%s addr=%v layers=%v\n",
		ufs.id, ufs.addr, ufs.layers)
	srv := fmt.Sprintf("/srv/%s", ufs.id)
	go func() {
		sleep2("ufs.serve before running srv", 500)
		os.Remove(srv)
		fullAddr := fmt.Sprintf("tcp!ninsis!%d", ufs.port)
		log.Printf("overlay: running srv -cn %v %s %v\n",
			fullAddr, ufs.id, ufs.mnt)
		cmd := exec.Command("srv", "-cn", fullAddr, ufs.id, ufs.mnt)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Start()
		if err != nil {
			log.Fatalf("overlay: srv error: %v\n", err)
		}
	}()

	styxServer := styx.Server{
		Addr:    ufs.addr,
		Handler: ufs,
	}
	if ufs.verbose {
		styxServer.ErrorLog = log.New(os.Stderr, "", 0)
	}
	if ufs.debug {
		styxServer.TraceLog = log.New(os.Stderr, "", 0)
	}
	err := styxServer.ListenAndServe()
	fmt.Printf("ListenAndServe exited with: %v\n", err)
	//os.Remove(srv)
}

func (ufs UFS) Serve9P(s *styx.Session) {
	for s.Next() {
		req := s.Request()
		if ufs.verbose {
			log.Printf("→→%q %T %s", s.User, req, req.Path())
		}
		switch req.(type) {
		case styx.Tstat:
			ufs.handleTstat(s, req.(styx.Tstat))
		case styx.Twalk:
			ufs.handleTwalk(s, req.(styx.Twalk))
		case styx.Topen:
			ufs.handleTopen(s, req.(styx.Topen))
		case styx.Tcreate:
			ufs.handleTcreate(s, req.(styx.Tcreate))
		case styx.Tremove:
			ufs.handleTremove(s, req.(styx.Tremove))
		case styx.Tutimes:
			ufs.handleTutimes(s, req.(styx.Tutimes))
		case styx.Tchmod:
			ufs.handleTchmod(s, req.(styx.Tchmod))
		case styx.Tchown:
			ufs.handleTchown(s, req.(styx.Tchown))
		case styx.Ttruncate:
			ufs.handleTtruncate(s, req.(styx.Ttruncate))
		case styx.Trename:
			ufs.handleTrename(s, req.(styx.Trename))
		case styx.Tsync:
			ufs.handleTsync(s, req.(styx.Tsync))
		default:
			fmt.Printf("# unknown request type %v, %v\n", req.Path(), req)
		}
	}
	if false {
		fmt.Printf("# ufs.Serve9P: unmounting %v\n", ufs.mnt)
		out, err := exec.Command("unmount", ufs.mnt).Output()
		if err != nil {
			log.Fatalf("unmount error: %v\n%s", err, string(out))
		}
		fmt.Printf("# ufs.Serve9P: unmounted %v\n", ufs.mnt)
	}
}

func assertAbsolute(path string) {
	if !strings.HasPrefix(path, "/") {
		panic(fmt.Sprintf("overlayfs bug: non-absolute path: '%s'", path))
	}
	if strings.HasPrefix(path, "//") {
		panic(fmt.Sprintf("overlayfs bug: double slash: '%s'", path))
	}
}

func (ufs UFS) apathAtDepth(upath string, depth int) string {
	assertAbsolute(upath)
	if depth < 0 {
		depth = 0
	}
	if depth > len(ufs.layers) {
		panic(fmt.Sprintf("invalid layer depth: %d", depth))
	}
	return ufs.layers[depth] + "/" + upath
}

// FIXME Optimise for common cases:
// - path exists on one of the layers
// - or its parent path exists
func (ufs UFS) resolve(upath string) UFile {
	//fmt.Printf("resolve(%s)\n", path)
	assertAbsolute(upath)

	getDepthNoWhiteout := func(upath2 string) int {
		//fmt.Printf("getDepthNoWhiteout(%s)\n", upath2)
		for depth := range ufs.layers {
			apath := ufs.apathAtDepth(upath2, depth)
			_, err := os.Stat(apath)
			if err == nil {
				if depth > 0 && ufs.upathHasWhiteout(upath2, depth-1) {
					return -1
				}
				return depth
			}
		}
		return -1
	}

	depth := getDepthNoWhiteout(upath)
	return UFile{ufs.apathAtDepth(upath, depth), depth}
}

// Returns smallest depth containing the path, or -1
func (ufs UFS) getDepth(upath string, maxDepth int) int {
	//fmt.Printf("getDepth(%s, %depth)\n", p, maxDepth)
	for depth := 0; depth <= maxDepth; depth++ {
		apath := ufs.apathAtDepth(upath, depth)
		_, err := os.Stat(apath)
		if err == nil {
			return depth
		}
	}
	return -1
}

// Returns true iff path or one of its parents has a whiteout
func (ufs UFS) upathHasWhiteout(upath string, maxDepth int) bool {
	//fmt.Printf("hasWhiteout(%s, %d)\n", upath, maxDepth)
	assertAbsolute(upath)
	names := strings.Split(upath, "/")[1:]
	for depth := 0; depth <= maxDepth; depth++ {
		for i, name := range names {
			names[i] = ".wh." + name
			upath2 := "/" + strings.Join(names[:i+1], "/")
			if ufs.getDepth(upath2, maxDepth) >= 0 {
				return true
			}
			names[i] = name
		}
	}
	return false
}

func (ufs UFS) upathInfo(upath string) (fs.FileInfo, error) {
	uf := ufs.resolve(upath)
	//fmt.Printf("# upathInfo(%s): realPath=%v\n", upath, uf.realPath)
	info, err := os.Stat(uf.apath)
	if err != nil {
		return nil, err
	}
	//fmt.Printf("# pathInfo: info=%v\n", info)
	return info, nil
}

func (ufs UFS) handleTstat(s *styx.Session, req styx.Tstat) {
	upath := req.Path()
	//fmt.Printf("# handleTstats: upath=%v\n", upath)
	info, err := ufs.upathInfo(upath)
	if err != nil {
		req.Rerror("%v", err)
		return
	}
	req.Rstat(info, nil)
}

func (ufs UFS) handleTwalk(s *styx.Session, req styx.Twalk) {
	upath := req.Path()
	//fmt.Printf("# handleTwalk: upath=%v\n",upath)
	info, err := ufs.upathInfo(upath)
	if err != nil {
		req.Rwalk(nil, errors.New("directory entry not found"))
		return
	}
	req.Rwalk(info, nil)
}

type UDirectory struct {
	infos *[]os.FileInfo
}

func (udir UDirectory) Readdir(n int) ([]os.FileInfo, error) {
	//fmt.Printf("# Readdir n=%v\n", n)
	infos := *udir.infos
	if n <= 0 {
		n = len(infos)
	}
	if len(infos) == 0 {
		return nil, errors.New("all entries read") //FIXME bug
	}
	if n > len(infos) {
		n = len(infos)
	}
	result := infos[:n]
	*udir.infos = (*udir.infos)[n:]
	return result, nil
}

func (ufs UFS) directory(upath string) styx.Directory {
	//FIXME optimise allocs
	infosByName := map[string]os.FileInfo{}
	for depth := len(ufs.layers) - 1; depth >= 0; depth-- {
		entries, err := os.ReadDir(ufs.apathAtDepth(upath, depth))
		if err != nil {
			continue //FIXME
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				fmt.Printf("# FIXME directory: %v\n", err)
				continue
			}
			name := info.Name()
			//fmt.Printf("-- directory: depth=%d name=%s\n", depth, name)
			var entryUpath string
			if upath == "/" {
				entryUpath = fmt.Sprintf("/%s", name)
			} else {
				entryUpath = fmt.Sprintf("%s/%s", upath, name)
			}
			if ufs.upathHasWhiteout(entryUpath, depth) || strings.HasPrefix(name, ".wh.") {
				delete(infosByName, name)
			} else {
				infosByName[name] = info
			}
		}
	}
	infos := make([]os.FileInfo, 0)
	for _, info := range infosByName {
		infos = append(infos, info)
	}
	return UDirectory{&infos}
}

func (ufs UFS) handleTopen(s *styx.Session, req styx.Topen) {
	//fmt.Printf("# handleTopen: req=%v\n", req)
	//fmt.Printf("# handleTopen: req.Path=%v\n", req.Path())
	upath := req.Path()
	uf := ufs.resolve(upath)
	info, err := os.Stat(uf.apath)
	if err != nil {
		req.Rerror("O1 %v", err)
		return
	}
	if info.IsDir() {
		d := ufs.directory(upath)
		req.Ropen(d, nil)
	} else {
		apath := uf.apath
		flag := req.Flag
		if flag&(os.O_RDWR|os.O_WRONLY) != 0 {
			// copy-on-write, except it's copy-on-open-for-writing
			err = ufs.copyToTop(upath, uf.depth)
			if err != nil {
				req.Rerror("O2 %v", err)
				return
			}
			apath = ufs.apathAtDepth(upath, 0)
		}
		f, err := os.OpenFile(apath, flag, 0)
		if err != nil {
			req.Rerror("O3 %v", err)
			return
		}
		req.Ropen(f, nil)
	}
}

// FIXME should not create the parents if no perms etc
func (ufs UFS) handleTcreate(s *styx.Session, req styx.Tcreate) {
	//fmt.Printf("# handleTcreate: req=%v\n", req)
	//fmt.Printf("# handleTcreate: newPath=%v\n", req.NewPath())
	//fmt.Printf("# handleTcreate: flag=%v\n", req.Flag)
	//fmt.Printf("# handleTcreate: mode=%v\n", req.Mode)
	upath := req.NewPath()
	uf := ufs.resolve(upath)
	err := ufs.createParents(upath)
	if err != nil {
		req.Rerror("X4 %v", err)
		return
	}
	if req.Mode.IsDir() {
		if err := os.Mkdir(uf.apath, req.Mode); err != nil {
			req.Rerror("X2 %v", err)
			return
		}
		d := ufs.directory(req.Path())
		req.Rcreate(d, nil)
	} else {
		flag := req.Flag
		f, err := os.OpenFile(uf.apath, flag|os.O_CREATE, req.Mode)
		if err != nil {
			fmt.Printf("X3 %v\n", err)
			req.Rerror("X3 %v", err)
			return
		}
		req.Rcreate(f, nil)
	}
}

// FIXME always the case that the parent dir already exist in 1 layer,
// minimise number of calls to resolve()
func (ufs UFS) createParents(upath string) error {
	//fmt.Printf("# createParentsOnTop(%s)\n", p)
	assertAbsolute(upath)
	uparent := filepath.Dir(upath)
	if uparent == "/" {
		return nil
	}
	uf := ufs.resolve(uparent)
	if uf.depth < 0 {
		panic(fmt.Sprintf("%s: depth < 0", uparent))
	} else if uf.depth > 0 {
		return ufs.copyToTop(uparent, uf.depth)
	}
	return nil
}

func (ufs UFS) copyToTop(upath string, srcDepth int) error {
	//fmt.Printf("# copyToTop(%s, %d)\n", upath, srcDepth)
	assertAbsolute(upath)
	names := strings.Split(upath, "/")[1:]
	for n := range names {
		upartial := "/" + strings.Join(names[:n+1], "/")
		asrc := ufs.layers[srcDepth] + upartial
		atgt := ufs.layers[0] + upartial
		if err := ufs.duplicate(asrc, atgt); err != nil {
			fmt.Printf("# copyToTop duplicate error=%v\n", err)
			return err
		}
	}
	// FIXME here another loop for setting the times
	return nil
}

func (ufs UFS) duplicate(asrc, atgt string) (err error) {
	info, e := os.Stat(asrc)
	if e != nil {
		err = e
		return
	}
	_, e = os.Stat(atgt)
	if e == nil {
		// target already present
		err = nil
		return
	}
	if info.IsDir() {
		if err = os.Mkdir(atgt, info.Mode().Perm()); err != nil {
			return
		}
		err = os.Chtimes(atgt, info.ModTime(), info.ModTime())
		return
	} else {
		var in, out *os.File
		in, err = os.Open(asrc)
		if err != nil {
			return
		}
		defer in.Close()
		out, err = os.OpenFile(atgt, os.O_CREATE|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return
		}
		defer func() {
			if e := out.Close(); e != nil {
				err = e
			}
		}()
		_, err = io.Copy(out, in)
		if err != nil {
			return
		}
		// FIXME get atime on 9?
		err = os.Chtimes(atgt, info.ModTime(), info.ModTime())
		return
	}
}

func (ufs UFS) handleTremove(s *styx.Session, req styx.Tremove) {
	whiteout := func(upath string) error {
		uparent := filepath.Dir(upath)
		uname := filepath.Base(upath)
		err := ufs.createParents(upath)
		if err != nil {
			return err
		}
		wh := fmt.Sprintf("%s%s/.wh.%s", ufs.layers[0], uparent, uname)
		_, err = os.OpenFile(wh, os.O_CREATE, 0600)
		return err
	}

	upath := req.Path()
	uf := ufs.resolve(upath)
	if uf.depth == 0 {
		info, err := os.Stat(uf.apath)
		if err != nil {
			req.Rremove(err)
			return
		}
		if info.IsDir() {
			err = os.RemoveAll(uf.apath)
			if err != nil {
				req.Rremove(err)
				return
			}
			err = whiteout(upath)
		} else {
			err = os.Remove(uf.apath)
		}
		req.Rremove(err)
	} else {
		err := whiteout(upath)
		req.Rremove(err)
	}
}

func (ufs UFS) handleTutimes(s *styx.Session, req styx.Tutimes) {
	upath := req.Path()
	uf := ufs.resolve(upath)
	err := os.Chtimes(uf.apath, req.Atime, req.Mtime)
	req.Rutimes(err)
}

func (ufs UFS) handleTchmod(s *styx.Session, req styx.Tchmod) {
	upath := req.Path()
	uf := ufs.resolve(upath)
	err := os.Chmod(uf.apath, req.Mode)
	req.Rchmod(err)
}

func (ufs UFS) handleTchown(s *styx.Session, req styx.Tchown) {
	upath := req.Path()
	uf := ufs.resolve(upath)
	err := os.Chown(uf.apath, -1, -1)
	req.Rchown(err)
}

func (ufs UFS) handleTtruncate(s *styx.Session, req styx.Ttruncate) {
	upath := req.Path()
	uf := ufs.resolve(upath)
	err := os.Truncate(uf.apath, req.Size)
	req.Rtruncate(err)
}

func (ufs UFS) handleTrename(s *styx.Session, req styx.Trename) {
	fmt.Printf("-- handleTrename: path=%s newPath=%s\n",
		req.Path(), req.NewPath)
	upath := req.Path()
	uf := ufs.resolve(upath)
	oldAPath := uf.apath
	fmt.Printf("-- handleTrename: path=%s newPath=%s oldAPath=%s\n",
		req.Path(), req.NewPath, oldAPath)
	//newAPath := ufs.apathAtDepth(req.NewPath, 0)
	newAPath := filepath.Dir(oldAPath) + "/" + req.NewPath
	err := os.Rename(oldAPath, newAPath)
	fmt.Printf("-- handleTrename: err=%v\n", err)
	req.Rrename(err)
}

func (ufs UFS) handleTsync(s *styx.Session, req styx.Tsync) {
	req.Rsync(errors.New("FIXME"))
}
