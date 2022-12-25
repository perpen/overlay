package main

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"testing"
	"time"
)

var tester UFSTester

func TestMain(m *testing.M) {
	testDir := "/usr/henri/src/overlay/tmp/layers"
	tester = newUFSTester(testDir)
	tester.reset()
	os.Exit(m.Run())
}

func check(e error) {
	if e != nil {
		log.Fatal(e)
	}
}

func sleep(s string, ms time.Duration) {
	fmt.Printf("--- sleep %s %dms\n", s, ms)
	time.Sleep(ms * time.Millisecond)
}

type UFSTester struct {
	testDir     string
	nextLayerId int
	ufs         UFS
}

func newUFSTester(testDir string) UFSTester {
	log.SetPrefix("")
	log.SetFlags(0)
	ufsID := "overlayTest"
	debug := false
	tester := UFSTester{
		testDir:     testDir,
		nextLayerId: 0,
		ufs: UFS{
			id:      ufsID,
			layers:  []string{},
			addr:    ":8889",
			port:    8889,
			mnt:     fmt.Sprintf("/n/%s", ufsID),
			verbose: debug,
			debug:   debug,
		},
	}
	return tester
}

func (tester *UFSTester) reset() {
	os.RemoveAll(tester.testDir)
	check(os.MkdirAll(tester.testDir, 0750))
	tester.nextLayerId = 0
}

func (tester *UFSTester) addLayer() {
	if len(tester.ufs.layers) > 0 {
		tester.unmount()
		sleep("addLayer after unmount", 700)
	}
	layerId := tester.nextLayerId
	tester.nextLayerId++
	layer := tester.testDir + "/" + strconv.Itoa(layerId)
	fmt.Printf("addLayer: layer=%v\n", layer)
	check(os.MkdirAll(layer, 0750))
	tester.ufs.layers = append([]string{layer}, tester.ufs.layers...)
	go tester.ufs.serve()
	sleep("addLayer after ufs.serve", 1000)
}

func (tester *UFSTester) unmount() {
	fmt.Printf("unmount:  unmounting %v\n", tester.ufs.mnt)
	out, err := exec.Command("unmount", tester.ufs.mnt).Output()
	if err != nil {
		log.Fatalf("unmount: error: %v\n%s", err, string(out))
	}
}

func (tester *UFSTester) mkdir(path string, perm fs.FileMode) {
	fullPath := tester.ufs.mnt + path
	check(os.MkdirAll(fullPath, 0))
	check(os.Chmod(fullPath, perm))
}

func (tester *UFSTester) mkfile(path string, perm fs.FileMode, t time.Time) {
	content := fmt.Sprintf("<%s>", path)
	fullPath := tester.ufs.mnt + path
	check(os.WriteFile(fullPath, []byte(content), 0))
	check(os.Chmod(fullPath, perm))
	check(os.Chtimes(fullPath, t, t))
}

func (tester *UFSTester) chtimes(path string, t time.Time) {
	fullPath := tester.ufs.mnt + path
	check(os.Chtimes(fullPath, t, t))
}

func (tester *UFSTester) rm(path string) {
	fullPath := tester.ufs.mnt + path
	check(os.RemoveAll(fullPath))
}

func (tester *UFSTester) assertStat(t *testing.T,
	path string, expectedPerm fs.FileMode, expectedTime time.Time) {
	fullPath := tester.ufs.mnt + path
	info, err := os.Stat(fullPath)
	check(err)
	actualTime := info.ModTime()
	if actualTime != expectedTime {
		t.Errorf("%s: expected mod time %v, got %v",
			path, expectedTime, actualTime)
	}
	actualPerm := info.Mode()
	if actualPerm != expectedPerm {
		t.Errorf("%s: expected perm %v, got %v",
			path, expectedPerm, actualPerm)
	}
}

func (tester *UFSTester) assertDirEntries(t *testing.T,
	path string, expected []string) {

	fullPath := tester.ufs.mnt + path
	info, err := os.Stat(fullPath)
	if err != nil {
		t.Errorf("not found: %s", fullPath)
		return
	}
	if !info.IsDir() {
		t.Errorf("not a directory: %s", fullPath)
		return
	}
	entries, _ := os.ReadDir(fullPath)
	//FIXME check(err)
	names := make([]string, len(entries))
	for n, entry := range entries {
		names[n] = entry.Name()
	}
	if !reflect.DeepEqual(names, expected) {
		t.Errorf("%s: expected dir entries %v, got %v",
			fullPath, expected, names)
	}
}

func TestUFS(t *testing.T) {
	t0 := time.Date(2000, time.February, 0, 0, 0, 0, 0, time.Local)
	t1 := time.Date(2001, time.February, 0, 0, 0, 0, 0, time.Local)
	/*
		t2 := time.Date(2002, time.February, 0, 0, 0, 0, 0, time.Local)
		t3 := time.Date(2003, time.February, 0, 0, 0, 0, 0, time.Local)
	*/
	perm0 := fs.FileMode(0770)
	perm1 := fs.FileMode(0771)
	/*
		p2 := 0770
		p3 := 0770
	*/

	tester.addLayer()
	tester.mkdir("/A", perm0)
	tester.mkfile("/A/a", perm0, t0)
	tester.chtimes("/A", t0)
	tester.assertStat(t, "/A", perm0|fs.ModeDir, t0)
	tester.assertStat(t, "/A/a", perm0, t0)
	tester.assertDirEntries(t, "/A", []string{"a"})

	check(os.Remove("/srv/overlayTest")) //FIXME
	tester.addLayer()
	tester.assertStat(t, "/A", perm0|fs.ModeDir, t0)
	tester.assertStat(t, "/A/a", perm0, t0)
	tester.assertDirEntries(t, "/A", []string{"a"})
	existingDir := tester.ufs.mnt + "/A"
	if err := os.Mkdir(existingDir, 0666); err == nil {
		panic(fmt.Sprintf("mkdir on existing path %s: %v", existingDir, err))
	}
	tester.mkdir("/B", perm1)
	tester.mkfile("/B/b", perm1, t1)
	tester.chtimes("/B", t1)
	tester.assertStat(t, "/B", perm1|fs.ModeDir, t1)
	tester.assertStat(t, "/B/b", perm1, t1)
	tester.assertDirEntries(t, "/B", []string{"b"})
	tester.mkdir("/B", perm1)
	tester.mkfile("/B/b", perm1, t1)
	tester.chtimes("/B", t1)
	tester.assertStat(t, "/B", perm1|fs.ModeDir, t1)
	tester.assertStat(t, "/B/b", perm1, t1)
	tester.assertDirEntries(t, "/B", []string{"b"})

	tester.mkfile("/A/b", perm1, t1)
	tester.assertStat(t, "/A/b", perm1, t1)
	tester.assertDirEntries(t, "/A", []string{"a", "b"})

	tester.assertDirEntries(t, "/", []string{"A", "B"})

	check(os.Remove("/srv/overlayTest")) //FIXME
	tester.addLayer()
	tester.rm("/A/a")
	tester.assertDirEntries(t, "/A", []string{"b"})
	tester.rm("/A/b")
	tester.assertDirEntries(t, "/A", []string{})
	tester.assertDirEntries(t, "/", []string{"A", "B"})
	tester.rm("/A")
	tester.assertDirEntries(t, "/", []string{"B"})

	tester.unmount()
}
