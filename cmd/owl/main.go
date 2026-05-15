package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/neverbot/owl/internal/config"
	"github.com/neverbot/owl/internal/version"
)

func main() {
	var (
		configPath  string
		showVersion bool
		checkOnly   bool
	)
	flag.StringVar(&configPath, "config", "/etc/owl/config.yml", "path to the config file")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.BoolVar(&checkOnly, "check-config", false, "validate the config and exit")
	flag.Parse()

	if showVersion {
		fmt.Println(version.String())
		return
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "owl: %v\n", err)
		os.Exit(2)
	}
	config.ApplyEnv(&cfg)
	if err := config.Validate(&cfg); err != nil {
		fmt.Fprintf(os.Stderr, "owl: %v\n", err)
		os.Exit(2)
	}

	if checkOnly {
		fmt.Fprintf(os.Stderr, "owl: config %s OK\n", configPath)
		return
	}

	mgr := config.NewManager(cfg)
	_ = mgr // wired into subsequent subsystems in later plans

	fmt.Fprintln(os.Stderr, "owl: foundation only; no runtime subsystems wired yet")
	os.Exit(0)
}
