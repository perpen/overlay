package main

import (
	"flag"
	"log"
	"os"
)

func main() {
	log.SetPrefix("")
	log.SetFlags(0)

	addr := flag.String("a", ":8888", "Port to listen on")
	debug := flag.Bool("D", false, "trace 9P messages")
	verbose := flag.Bool("v", false, "print extra info")
	flag.Parse()
	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(2)
	}
	layers := flag.Args()

	ufs := UFS{
		id:      "overlay",
		layers:  layers,
		addr:    *addr,
		port:    8888,
		mnt:     "/n/overlay",
		verbose: *verbose,
		debug:   *debug,
	}
	ufs.serve()
}
