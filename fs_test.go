package main

import (
	"fmt"
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
		sleep("addLayer after unmount", 1000)
	}
	layerId := tester.nextLayerId
	tester.nextLayerId++
	layer := tester.testDir + "/" + strconv.Itoa(layerId)
	fmt.Printf("addLayer: layer=%v\n", layer)
	check(os.MkdirAll(layer, 0750))
	tester.ufs.layers = append([]string{layer}, tester.ufs.layers...)
	go tester.ufs.serve()
	sleep("addLayer after ufs.serve", 1500)
}

func (tester *UFSTester) unmount() {
	fmt.Printf("unmount:  unmounting %v\n", tester.ufs.mnt)
	out, err := exec.Command("unmount", tester.ufs.mnt).Output()
	if err != nil {
		log.Fatalf("unmount: error: %v\n%s", err, string(out))
	}
}

func (tester *UFSTester) mkdir(path string, perm int) {
	fullPath := tester.ufs.mnt + "/" + path
	check(os.MkdirAll(fullPath, os.FileMode(perm)))
}

func (tester *UFSTester) mkfile(path string, perm int, t time.Time) {
	content := fmt.Sprintf("<%s>", path)
	fullPath := tester.ufs.mnt + "/" + path
	check(os.WriteFile(fullPath, []byte(content), os.FileMode(perm)))
	check(os.Chtimes(fullPath, t, t))
}

func (tester *UFSTester) chtimes(path string, t time.Time) {
	fullPath := tester.ufs.mnt + "/" + path
	check(os.Chtimes(fullPath, t, t))
}

func (tester *UFSTester) rm(path string) {
	fullPath := tester.ufs.mnt + "/" + path
	check(os.RemoveAll(fullPath))
}

func (tester *UFSTester) assertDirEntries(t *testing.T,
	path string, expected []string) {

	fullPath := tester.ufs.mnt + "/" + path
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
	/*
		t1 := time.Date(2001, time.February, 0, 0, 0, 0, 0, time.Local)
		t2 := time.Date(2002, time.February, 0, 0, 0, 0, 0, time.Local)
		t3 := time.Date(2003, time.February, 0, 0, 0, 0, 0, time.Local)
	*/
	perm0 := 0770
	/*
		p1 := 0771
		p2 := 0770
		p3 := 0770
	*/

	tester.addLayer()
	tester.mkdir("A", perm0)
	tester.mkfile("A/a", perm0, t0)
	tester.chtimes("A", t0)
	tester.assertDirEntries(t, "A", []string{"a"})

	check(os.Remove("/srv/overlayTest"))
	sleep("before addLayer", 1000)
	tester.addLayer()
	tester.assertDirEntries(t, "A", []string{"a"})
	tester.unmount()

	/*
		tester.addLayer()
		tester.assertDirEntries(t, "A", []string{"a"})
		tester.rm("A")
		tester.assertDirEntries(t, "A", []string{})

		tester.unmount()

		tester.addLayer()
		tester.rm("B")

		useLayers(1, 2, 3)
		mkdir("A", 0666, 0)
		mkfile("A/b", 0666, t1)
		mkfile("A/c", 0666, t1)
		chtimes("A", t1)
		mkfile("e", 0444, t1)

		useLayers(0, 1, 2, 3)
		rm("e")
		mkfile("f", 0400, t0)
	*/
}

/*
func (tester *UFSTester) pathTime(path string) time.Time {
	r := int(path[0]) - '0'
	if r < 0 || r >= len(layers) {
		panic(fmt.Sprintf("unexpected first char: %s", path))
	}
	return time.Date(2000+r, time.February, 0, 0, 0, 0, 0, time.Local)
}

func TestUFS(t *testing.T) {
	// Setup the roots
	resetRoots()
	mkdirs("1/D", "3/D")
	mkfiles("0/a", "0/.wh.b", "1/D/f", "1/D/g", "1/b", "2/.wh.D", "3/D/c")

	// Change tree via the UFS
	check(os.WriteFile(ufs.mnt+"/p", []byte("<p>"), 0644))
	check(os.WriteFile(ufs.mnt+"/D/q", []byte("<D/q>"), 0644))
	check(os.Mkdir(ufs.mnt+"/D/E", 0660))
	check(os.WriteFile(ufs.mnt+"/D/g", []byte("<D/g>"), 0644))
	//TODO check Chtimes, Chmod

	chtimesAll()

	tests := []struct {
		path    string
		year    int // -1 if path should be absent
		isDir   bool
		perms   fs.FileMode
		entries []string // checked if isDir
		content string   // checked if !isDir
	}{
		{"/", 2000, true, 0750, []string{"D", "a", "b", "p"}, ""},
		{"/D", 2000, true, 0750, []string{"E", "f", "g", "q"}, ""},
		{"/b", -1, false, 0640, nil, ""},
		{"/a", 2000, false, 0640, nil, "<0/a>"},
		{"/D/f", 2001, false, 0640, nil, "<1/D/f>"},
		{"/D/g", 2000, false, 0640, nil, "<D/g>"},
		{"/D/c", -1, false, 0640, nil, ""},
		{"/D/q", 2000, false, 0640, nil, "<D/q>"},
		{"/p", 2000, false, 0640, nil, "<p>"},
	}
	for _, tst := range tests {
		abs := ufs.mnt + tst.path
		info, err := os.Stat(abs)
		if tst.year < 0 {
			if err == nil {
				t.Errorf("%s: path exists", tst.path)
				continue
			}
		} else {
			if tst.isDir {
				// check dir entries
				if !info.IsDir() {
					t.Errorf("%s: expected a directory, got a file", tst.path)
					continue
				}
				entries, _ := os.ReadDir(abs)
				//FIXME check(err)
				names := make([]string, len(entries))
				for n, entry := range entries {
					names[n] = entry.Name()
				}
				if !reflect.DeepEqual(names, tst.entries) {
					t.Errorf("%s: expected dir entries %v, got %v",
						tst.path, tst.entries, names)
					continue
				}
			} else {
				// check file content
				if info.IsDir() {
					t.Errorf("%s: expected a file, got a directory", tst.path)
					continue
				}
				b, err := os.ReadFile(abs)
				check(err)
				content := string(b)
				if content != tst.content {
					t.Errorf("%s: expected content '%s', got '%s'",
						tst.path, tst.content, content)
					continue
				}
			}
			// check stat
			year := info.ModTime().Year()
			if year != tst.year {
				t.Errorf("%s: expected year %d, got %d",
					tst.path, tst.year, year)
				continue
			}
			perms := info.Mode().Perm()
			if perms != tst.perms {
				t.Errorf("%s: expected perms %o, got %o",
					tst.path, tst.perms, perms)
				continue
			}
		}
	}
}
*/
