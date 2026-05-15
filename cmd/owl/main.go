package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	rtcollect "github.com/neverbot/owl/internal/collect/runtime"
	"github.com/neverbot/owl/internal/config"
	"github.com/neverbot/owl/internal/dashboards"
	"github.com/neverbot/owl/internal/query"
	"github.com/neverbot/owl/internal/scrape"
	"github.com/neverbot/owl/internal/storage"
	"github.com/neverbot/owl/internal/version"
	"github.com/neverbot/owl/internal/web"
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
		fail(err)
	}
	config.ApplyEnv(&cfg)
	if err := config.Validate(&cfg); err != nil {
		fail(err)
	}
	if checkOnly {
		fmt.Fprintf(os.Stderr, "owl: config %s OK\n", configPath)
		return
	}

	if err := run(cfg); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "owl: %v\n", err)
	os.Exit(2)
}

func run(cfg config.Config) error {
	if err := ensureDir(cfg.Storage.Path); err != nil {
		return fmt.Errorf("storage dir: %w", err)
	}
	store, err := storage.Open(cfg.Storage.Path)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	retention := &storage.Worker{
		Store:    store,
		Time:     cfg.Storage.Retention.Time,
		Size:     cfg.Storage.Retention.Size,
		Interval: 5 * time.Minute,
	}
	go retention.Run(ctx)

	collector := rtcollect.New(store, cfg.Scrape.DefaultInterval)
	go collector.Run(ctx)

	// HTTP scrape manager.
	scrapeMgr := scrape.NewManager(store)
	scrapeMgr.Set(buildTargets(cfg))
	go scrapeMgr.Run(ctx)

	// Query engine.
	engine := query.NewEngine(store)

	// Dashboard loader.
	dashLoader := dashboards.NewLoader(cfg.Dashboards.Dir, engine)
	if err := dashLoader.Reload(); err != nil {
		return fmt.Errorf("dashboards: %w", err)
	}

	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: web.NewServer(web.Options{
			Store:  store,
			Engine: engine,
			Loader: dashLoader,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	httpErr := make(chan error, 1)
	go func() {
		fmt.Fprintf(os.Stderr, "owl: listening on http://%s\n", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "owl: shutting down")
	case err := <-httpErr:
		cancel()
		return err
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	return srv.Shutdown(shutdownCtx)
}

func buildTargets(cfg config.Config) []scrape.Target {
	defInterval := cfg.Scrape.DefaultInterval
	defTimeout := cfg.Scrape.DefaultTimeout
	out := make([]scrape.Target, 0, len(cfg.Targets))
	for _, t := range cfg.Targets {
		interval := t.Interval
		if interval <= 0 {
			interval = defInterval
		}
		timeout := t.Timeout
		if timeout <= 0 {
			timeout = defTimeout
		}
		labels := map[string]string{}
		for k, v := range t.Labels {
			labels[k] = v
		}
		out = append(out, scrape.Target{
			Name:     t.Name,
			URL:      t.URL,
			Interval: interval,
			Timeout:  timeout,
			Labels:   labels,
		})
	}
	return out
}

func ensureDir(path string) error {
	dir := path
	for i := len(dir) - 1; i >= 0; i-- {
		if dir[i] == '/' {
			dir = dir[:i]
			break
		}
	}
	if dir == "" || dir == path {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}
