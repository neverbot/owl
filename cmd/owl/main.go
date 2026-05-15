package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/neverbot/owl/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.String())
		os.Exit(0)
	}

	fmt.Fprintln(os.Stderr, "owl: no subcommand selected; run with --version")
	os.Exit(2)
}
